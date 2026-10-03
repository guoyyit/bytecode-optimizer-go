package opt

import "github.com/guoyyit/bytecode-optimizer-go/pkg/ir"

// passJumpFold performs two semantics-preserving rewrites:

//  1. Chain folding: "JMP A" where A is immediately "JMP B" (labels between
//     them allowed) is rewritten to "JMP B". Conditional jumps follow the
//     same chain.
//  2. Redundant JMP removal: "JMP L" whose target is exactly where control
//     would continue anyway is deleted.
//
// Labels are never removed by this pass: they are the symbolic anchors that
// keep every jump target correct after instructions are deleted or reordered.
func passJumpFold(f *ir.Func) Stats {
	st := Stats{Name: PassJump}
	code := f.Code
	n := len(code)

	// Label index: name -> position, built once so chain following is O(1)
	// per step instead of a linear scan over every instruction.
	labelPos := make(map[string]int, n)
	for i, ins := range code {
		if ins.Op == ir.OpLabel {
			labelPos[ins.Text] = i
		}
	}

	// chainTarget returns the final label of the jump chain starting at
	// name. It memoizes results so each label is followed at most once:
	// following l0->l1->...->ln populates the answer for every link.
	memo := make(map[string]string, len(labelPos))
	var chainTarget func(string) string
	chainTarget = func(name string) string {
		if end, ok := memo[name]; ok {
			return end
		}
		path := make([]string, 0, 8)
		cur := name
		for {
			if end, ok := memo[cur]; ok {
				for _, p := range path {
					memo[p] = end
				}
				return end
			}
			for _, p := range path {
				if p == cur {
					for _, q := range path {
						memo[q] = cur
					}
					return cur
				}
			}
			path = append(path, cur)
			pos, ok := labelPos[cur]
			if !ok {
				for _, p := range path {
					memo[p] = cur
				}
				return cur
			}
			next := skipLabels(code, pos+1)
			if next >= n || code[next].Op != ir.OpJmp {
				for _, p := range path {
					memo[p] = cur
				}
				return cur
			}
			cur = code[next].Text
		}
	}

	out := make([]ir.Instr, 0, n)
	for i := 0; i < n; i++ {
		ins := code[i]
		switch ins.Op {
		case ir.OpJmp, ir.OpJz, ir.OpJnz:
			dst := chainTarget(ins.Text)
			if dst != ins.Text {
				ins.Text = dst
				st.changed = true
			}
			if ins.Op == ir.OpJmp && redundantFallthrough(out, code[i+1:], dst) {
				st.Removed++
				st.changed = true
				continue
			}
		}
		out = append(out, ins)
	}
	f.Code = out
	return st
}

// redundantFallthrough reports whether deleting JMP dst leaves control
// flowing straight into dst: the remaining tail must consist of zero or more
// label markers, its first label (closest to the jump) must be dst, and the
// instruction already emitted before the jump must allow fall-through.
func redundantFallthrough(out []ir.Instr, tail []ir.Instr, dst string) bool {
	k := 0
	for k < len(tail) && tail[k].Op == ir.OpLabel {
		k++
	}
	if k == 0 || tail[0].Text != dst {
		return false
	}
	if len(out) > 0 {
		last := out[len(out)-1]
		if last.Op == ir.OpRet || last.Op == ir.OpJmp {
			return false
		}
	}
	return true
}

func skipLabels(code []ir.Instr, from int) int {
	for from < len(code) && code[from].Op == ir.OpLabel {
		from++
	}
	return from
}
