package opt

import "github.com/guoyyit/bytecode-optimizer-go/pkg/ir"

// passDCE removes every instruction that can never execute, including label
// markers no reachable jump targets. It computes the reachable set by a
// traversal of the control-flow graph starting at the function entry:
//
//   - fall-through edges from every non-terminating instruction (everything
//     except JMP and RET) to the next instruction;
//   - branch edges from JMP/JZ/JNZ to their target label.
//
// An instruction is kept exactly when it is reachable. This subsumes the
// classic "code after RET/JMP up to the next label" rule and also removes
// unreferenced labels together with code that only an unreferenced label
// would fall through to. The whole cascade (unreferenced label -> exposed
// dead code -> jumps only dead code referenced) is resolved in one pass, so
// the pipeline converges in a bounded number of iterations regardless of
// label count or jump-chain depth. Labels are symbolic anchors, so removing
// unreachable ones never relocates a reachable jump: every reachable jump
// targets a reachable label, which survives.
func passDCE(f *ir.Func) Stats {
	st := Stats{Name: PassDCE}
	code := f.Code
	n := len(code)
	if n == 0 {
		return st
	}

	labelPos := make(map[string]int)
	for i, ins := range code {
		if ins.Op == ir.OpLabel {
			labelPos[ins.Text] = i
		}
	}

	reachable := make([]bool, n)
	stack := []int{0}
	reachable[0] = true
	for len(stack) > 0 {
		pc := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		ins := code[pc]

		if ins.Op == ir.OpJmp || ins.Op == ir.OpJz || ins.Op == ir.OpJnz {
			if t, ok := labelPos[ins.Text]; ok && !reachable[t] {
				reachable[t] = true
				stack = append(stack, t)
			}
		}
		if ins.Op != ir.OpJmp && ins.Op != ir.OpRet && pc+1 < n && !reachable[pc+1] {
			reachable[pc+1] = true
			stack = append(stack, pc+1)
		}
	}

	out := make([]ir.Instr, 0, n)
	for i, ins := range code {
		if !reachable[i] {
			st.Removed++
			st.changed = true
			continue
		}
		out = append(out, ins)
	}
	f.Code = out
	return st
}
