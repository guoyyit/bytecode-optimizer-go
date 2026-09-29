package opt

import "github.com/guoyyit/bytecode-optimizer-go/pkg/bytecode"

// PassStats 记录单遍优化的可量化收益。
type PassStats struct {
	Name       string
	Removed    int // 删除（合并）的指令数
	Folded     int // 折叠的常量运算数
	CostBefore int // 优化前静态成本
	CostAfter  int // 优化后静态成本
}

// Delta 返回成本下降量。
func (s PassStats) Delta() int { return s.CostBefore - s.CostAfter }

// Changed 报告本遍是否产生了任何变化。
func (s PassStats) Changed() bool { return s.Removed > 0 || s.Folded > 0 }

// ConstantFold 常量折叠遍。
func ConstantFold(p *bytecode.Program) (*bytecode.Program, PassStats, error) {
	out := p.Clone()
	st := PassStats{Name: "constfold", CostBefore: p.StaticCost()}
	for _, f := range out.Funcs {
		if err := foldFunc(f, &st); err != nil {
			return nil, PassStats{Name: "constfold"}, err
		}
	}
	st.CostAfter = out.StaticCost()
	return out, st, nil
}

func foldFunc(f *bytecode.Function, st *PassStats) error {
	code := f.Code
	res := make([]*bytecode.Instr, 0, len(code))

	emit := func(in *bytecode.Instr) { res = append(res, in) }

	for i := 0; i < len(code); i++ {
		in := code[i]

		// push a; (push b;)? 二元运算/比较
		if in.Op == bytecode.OpPush && i+2 < len(code) &&
			code[i+1].Op == bytecode.OpPush &&
			isBinOrCmp(code[i+2].Op) && noLabelBetween(code, i, i+2) {
			a, b := in.Arg, code[i+1].Arg
			op := code[i+2].Op
			if r, ok := tryFoldBin(op, a, b); ok {
				out := *code[i+2]
				out.Op = bytecode.OpPush
				out.Arg = r
				out.Target = ""
				emit(&out)
				st.Folded++
				st.Removed += 2
				i += 2
				continue
			}
		}

		// push a; neg
		if in.Op == bytecode.OpPush && i+1 < len(code) && code[i+1].Op == bytecode.OpNeg &&
			noLabelBetween(code, i, i+1) {
			if r, err := bytecode.NegInt(in.Arg); err == nil {
				out := *code[i+1]
				out.Op = bytecode.OpPush
				out.Arg = r
				out.Target = ""
				emit(&out)
				st.Folded++
				st.Removed++
				i++
				continue
			}
		}

		// push c; jz/jnz L —— 常量条件分支折叠为直接跳转或删除
		if in.Op == bytecode.OpPush && i+1 < len(code) &&
			(code[i+1].Op == bytecode.OpJz || code[i+1].Op == bytecode.OpJnz) &&
			noLabelBetween(code, i, i+1) {
			br := code[i+1]
			taken := in.Arg == 0
			if br.Op == bytecode.OpJnz {
				taken = !taken
			}
			st.Folded++
			st.Removed++ // push 被消化
			i++
			if taken {
				j := *br
				j.Op = bytecode.OpJmp
				emit(&j)
			}
			// 不跳转：分支指令整体删除，顺序流入下一条
			continue
		}

		emit(in)
	}
	f.Code = res
	return nil
}

func isBinOrCmp(op bytecode.Opcode) bool {
	switch op {
	case bytecode.OpAdd, bytecode.OpSub, bytecode.OpMul, bytecode.OpDiv, bytecode.OpMod,
		bytecode.OpEq, bytecode.OpNe, bytecode.OpLt, bytecode.OpLe, bytecode.OpGt, bytecode.OpGe:
		return true
	}
	return false
}

// tryFoldBin 仅在不发生运行时错误（除零、溢出）时折叠，保证异常语义不变。
func tryFoldBin(op bytecode.Opcode, a, b int64) (int64, bool) {
	switch op {
	case bytecode.OpAdd:
		r, err := bytecode.AddInt(a, b)
		return r, err == nil
	case bytecode.OpSub:
		r, err := bytecode.SubInt(a, b)
		return r, err == nil
	case bytecode.OpMul:
		r, err := bytecode.MulInt(a, b)
		return r, err == nil
	case bytecode.OpDiv:
		r, err := bytecode.DivInt(a, b)
		return r, err == nil
	case bytecode.OpMod:
		r, err := bytecode.ModInt(a, b)
		return r, err == nil
	case bytecode.OpEq:
		return b2i(a == b), true
	case bytecode.OpNe:
		return b2i(a != b), true
	case bytecode.OpLt:
		return b2i(a < b), true
	case bytecode.OpLe:
		return b2i(a <= b), true
	case bytecode.OpGt:
		return b2i(a > b), true
	case bytecode.OpGe:
		return b2i(a >= b), true
	default:
		return 0, false
	}
}

func b2i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// noLabelBetween 报告 code[l:r+1] 内是否存在标号。
// 任何跨标号的多指令模式都不能折叠，否则跳转可从模式中间进入。
func noLabelBetween(code []*bytecode.Instr, l, r int) bool {
	for k := l; k <= r; k++ {
		if code[k].Op == bytecode.OpLabel {
			return false
		}
	}
	return true
}

// DeadCodeElim 死代码消除遍。
func DeadCodeElim(p *bytecode.Program) (*bytecode.Program, PassStats, error) {
	out := p.Clone()
	st := PassStats{Name: "dce", CostBefore: p.StaticCost()}
	for _, f := range out.Funcs {
		dceFunc(f, &st)
	}
	st.CostAfter = out.StaticCost()
	return out, st, nil
}

func dceFunc(f *bytecode.Function, st *PassStats) {
	code := f.Code

	labels := f.LabelTargets()

	// CFG 工作列表可达性：同时考虑顺序后继与跳转目标。
	reachable := make([]bool, len(code))
	var wl []int
	wl = append(wl, 0)
	for len(wl) > 0 {
		pc := wl[len(wl)-1]
		wl = wl[:len(wl)-1]
		if pc < 0 || pc >= len(code) || reachable[pc] {
			continue
		}
		reachable[pc] = true
		in := code[pc]
		if in.Op == bytecode.OpJmp {
			wl = append(wl, labels[in.Target])
			continue
		}
		if in.Op.IsTerminator() { // ret / halt
			continue
		}
		if in.Op == bytecode.OpJz || in.Op == bytecode.OpJnz {
			wl = append(wl, labels[in.Target])
		}
		wl = append(wl, pc+1)
	}

	// 只有仍被可达分支引用的标号才存活。
	referenced := make(map[string]bool)
	for i, in := range code {
		if reachable[i] && in.Op.IsBranch() {
			referenced[in.Target] = true
		}
	}

	res := make([]*bytecode.Instr, 0, len(code))
	for i, in := range code {
		if in.Op == bytecode.OpLabel {
			if referenced[in.Target] {
				res = append(res, in)
			} else {
				st.Removed++
			}
			continue
		}
		if reachable[i] {
			res = append(res, in)
		} else {
			st.Removed++
		}
	}
	f.Code = res
}

// JumpThreading 跳转链折叠遍。
func JumpThreading(p *bytecode.Program) (*bytecode.Program, PassStats, error) {
	out := p.Clone()
	st := PassStats{Name: "jumpthread", CostBefore: p.StaticCost()}
	for _, f := range out.Funcs {
		threadFunc(f, &st)
	}
	st.CostAfter = out.StaticCost()
	return out, st, nil
}

// resolve 跟随无条件跳转链，返回最终落点的指令下标。
func resolve(labels map[string]int, code []*bytecode.Instr, start string) (string, int) {
	name := start
	seen := map[string]bool{start: true}
	for {
		pc, ok := labels[name]
		if !ok {
			return name, -1
		}
		// 跳过标号，寻找该标号处第一条非标号指令。
		i := pc
		for i < len(code) && code[i].Op == bytecode.OpLabel {
			i++
		}
		if i < len(code) && code[i].Op == bytecode.OpJmp {
			next := code[i].Target
			if seen[next] {
				return name, pc // 回环，保持现状
			}
			seen[next] = true
			name = next
			continue
		}
		return name, pc
	}
}

func threadFunc(f *bytecode.Function, st *PassStats) {
	code := f.Code
	labels := f.LabelTargets()

	// 1) 重定向所有分支到链的最终目标。
	for _, in := range code {
		if !in.Op.IsBranch() {
			continue
		}
		final, _ := resolve(labels, code, in.Target)
		if final != in.Target {
			in.Target = final
			st.Folded++
		}
	}

	// 2) 消除模式:
	//    jmp L          label L:
	//    label L:  ->   <真实代码>
	//    <真实代码>
	// 即无条件 jmp 到紧邻的下一条非标号指令所在标号。
	res := make([]*bytecode.Instr, 0, len(code))
	for i := 0; i < len(code); i++ {
		in := code[i]
		if in.Op == bytecode.OpJmp && i+1 < len(code) {
			// 找 jmp 之后连续标号块中是否存在其目标。
			j := i + 1
			for j < len(code) && code[j].Op == bytecode.OpLabel {
				if code[j].Target == in.Target {
					st.Removed++
					in = nil
					break
				}
				j++
			}
		}
		if in != nil {
			res = append(res, in)
		}
	}
	f.Code = res
}

// RedundantMerge 冗余指令合并遍（如 PUSH;POP、STORE;LOAD 同槽）。
func RedundantMerge(p *bytecode.Program) (*bytecode.Program, PassStats, error) {
	out := p.Clone()
	st := PassStats{Name: "peephole", CostBefore: p.StaticCost()}
	for _, f := range out.Funcs {
		peepholeFunc(f, &st)
	}
	st.CostAfter = out.StaticCost()
	return out, st, nil
}

func isPureProducer(op bytecode.Opcode) bool {
	return op == bytecode.OpPush || op == bytecode.OpLoad
}

func peepholeFunc(f *bytecode.Function, st *PassStats) {
	code := f.Code
	res := make([]*bytecode.Instr, 0, len(code))

	for i := 0; i < len(code); i++ {
		// dup; store s; pop  ->  store s
		if i+2 < len(code) &&
			code[i].Op == bytecode.OpDup &&
			code[i+1].Op == bytecode.OpStore &&
			code[i+2].Op == bytecode.OpPop && noLabelBetween(code, i, i+2) {
			res = append(res, code[i+1])
			st.Removed += 2
			i += 2
			continue
		}

		// <pure producer>; pop -> 空   以及  dup; pop -> 空
		if i+1 < len(code) && code[i+1].Op == bytecode.OpPop &&
			(isPureProducer(code[i].Op) || code[i].Op == bytecode.OpDup) &&
			noLabelBetween(code, i, i+1) {
			st.Removed += 2
			i++
			continue
		}

		res = append(res, code[i])
	}
	f.Code = res
}
