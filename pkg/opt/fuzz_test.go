package opt_test

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"

	"github.com/guoyyit/bytecode-optimizer-go/pkg/bytecode"
	"github.com/guoyyit/bytecode-optimizer-go/pkg/opt"
	"github.com/guoyyit/bytecode-optimizer-go/pkg/vm"
)

// genDAGProgram 生成一个无环（只含向前跳转）、保证通过 Verify 且必然终止的程序。
// 方法：在两个汇合点（所有路径栈深相同）之间填充直线代码与“向前跳过一小段”的分支。
func genDAGProgram(r *rand.Rand) *bytecode.Program {
	const locals = 2
	var code []*bytecode.Instr
	emit := func(op bytecode.Opcode, arg int64, target string) {
		code = append(code, &bytecode.Instr{Op: op, Arg: arg, Target: target, OrigPC: len(code)})
	}

	depth := 0
	blocks := 2 + r.Intn(4)
	for b := 0; b < blocks; b++ {
		join := fmt.Sprintf("J%d", b)

		// 块入口：所有路径栈深统一为 depth。先压条件并可向前跳过一条 push。
		emit(bytecode.OpPush, r.Int63n(3)-1, "")
		depth++
		emit(bytecode.OpPush, r.Int63n(4), "")
		depth++

		// 可选：插入一个“被跳过的纯 push;pop”死代码候选。
		if r.Intn(2) == 0 {
			emit(bytecode.OpJz, 0, join)
			depth -= 2 // 弹掉条件与多余值，使 taken 路径到达 join 时为 depth-2
			emit(bytecode.OpPush, r.Int63n(100), "")
			emit(bytecode.OpPop, 0, "")
			// 顺序路径弹回相同深度，保证汇合点栈深一致。
			emit(bytecode.OpLabel, 0, join)
			depth += 0
		} else {
			emit(bytecode.OpPop, 0, "")
			depth--
			emit(bytecode.OpPop, 0, "")
			depth--
			emit(bytecode.OpLabel, 0, join)
		}

		// 汇合后：做一次可折叠/不可折叠运算或 print，始终保持栈平衡。
		emit(bytecode.OpPush, r.Int63n(6)-3, "")
		depth++
		switch r.Intn(4) {
		case 0:
			emit(bytecode.OpPush, r.Int63n(6)-3, "")
			depth++
			ops := []bytecode.Opcode{
				bytecode.OpAdd, bytecode.OpSub, bytecode.OpMul,
				bytecode.OpEq, bytecode.OpLt, bytecode.OpGt,
			}
			emit(ops[r.Intn(len(ops))], 0, "")
			depth--
		case 1:
			emit(bytecode.OpDup, 0, "")
			depth++
			emit(bytecode.OpPop, 0, "")
			depth--
		case 2:
			if depth >= 2 {
				emit(bytecode.OpPrint, 0, "")
				depth--
				emit(bytecode.OpPush, r.Int63n(10), "")
				depth++
			}
		}
	}

	for depth > 0 {
		emit(bytecode.OpPop, 0, "")
		depth--
	}
	emit(bytecode.OpHalt, 0, "")

	return &bytecode.Program{Funcs: []*bytecode.Function{{
		Name: "main", Params: 0, Locals: locals, Code: code,
	}}}
}

func execute(p *bytecode.Program) (out, reason string) {
	var buf bytes.Buffer
	err := vm.New(p, &buf).Run()
	if err != nil {
		if ve, ok := err.(*vm.Error); ok {
			reason = ve.Reason
		} else {
			reason = err.Error()
		}
	}
	return buf.String(), reason
}

func TestFuzzDifferential(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	r := rand.New(rand.NewSource(1))
	cfg := opt.Config{
		ConstantFold: true, DeadCodeElim: true,
		JumpThreading: true, RedundantMerge: true,
	}
	for iter := 0; iter < 500; iter++ {
		orig := genDAGProgram(r)
		out0, err0 := execute(orig)

		op, rep, oerr := opt.Optimize(orig, cfg)
		if oerr != nil {
			t.Fatalf("iter %d: optimizer rejected a valid program: %v", iter, oerr)
		}
		if !rep.Stable {
			t.Fatalf("iter %d: not stable", iter)
		}
		out1, err1 := execute(op)
		if out0 != out1 || err0 != err1 {
			var b bytes.Buffer
			for _, in := range orig.Funcs[0].Code {
				fmt.Fprintf(&b, "  %s %d %s\n", in.Op, in.Arg, in.Target)
			}
			t.Fatalf("iter %d mismatch\nbefore=(%q,%q)\nafter =(%q,%q)\n%s",
				iter, out0, err0, out1, err1, b.String())
		}
	}
}
