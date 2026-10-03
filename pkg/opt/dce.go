package opt

import "github.com/guoyyit/bytecode-optimizer-go/pkg/ir"

// passDCE removes instructions that can never execute, plus label markers no
// jump references. It computes the set of reachable PCs with a control-flow
// walk from the function entry (labels are transparent, RET/JMP terminate
// fall-through, JZ/JNZ both fall through and branch) and then keeps exactly
// the reachable instructions, dropping label markers that no reachable jump
// names. Because both properties are decided from the same fixed reachability
// facts, one sweep is fully convergent: running the pass on its own output
// removes nothing. Keeping label pruning inside the pass (instead of a
// cleanup after the fixed point) is what makes the whole pipeline idempotent:
// deleting a label can expose new peephole/fold/jump opportunities, so the
// iteration loop must observe it as an ordinary transformation.
//
// A function body is never emptied: if every remaining instruction would be
// dropped (possible only when the body is nothing but unreferenced labels),
// the entry instruction is kept so the result stays a valid program.
func passDCE(f *ir.Func) Stats {
	st := Stats{Name: PassDCE}
	code := f.Code
	n := len(code)
	if n == 0 {
		return st
	}

	labelPos := make(map[string]int, n)
	for i, ins := range code {
		if ins.Op == ir.OpLabel {
			labelPos[ins.Text] = i
		}
	}

	reachable := make([]bool, n)
	worklist := []int{0}
	for len(worklist) > 0 {
		i := worklist[len(worklist)-1]
		worklist = worklist[:len(worklist)-1]
		if i < 0 || i >= n || reachable[i] {
			continue
		}
		reachable[i] = true
		ins := code[i]
		switch ins.Op {
		case ir.OpJmp:
			worklist = append(worklist, jumpTarget(labelPos, ins.Text))
		case ir.OpJz, ir.OpJnz:
			worklist = append(worklist, i+1, jumpTarget(labelPos, ins.Text))
		case ir.OpRet:
			// No successors: control leaves the function.
		default:
			// Labels and plain instructions fall through.
			worklist = append(worklist, i+1)
		}
	}

	used := map[string]bool{}
	for i, ins := range code {
		if !reachable[i] {
			continue
		}
		if ins.Op == ir.OpJmp || ins.Op == ir.OpJz || ins.Op == ir.OpJnz {
			used[ins.Text] = true
		}
	}

	keep := make([]bool, n)
	removed := 0
	for i, ins := range code {
		if reachable[i] && (ins.Op != ir.OpLabel || used[ins.Text]) {
			keep[i] = true
			continue
		}
		removed++
	}
	if removed == n {
		// Never empty the body; the entry instruction always stays.
		keep[0] = true
		removed--
	}
	if removed == 0 {
		return st
	}

	out := make([]ir.Instr, 0, n-removed)
	for i, ins := range code {
		if keep[i] {
			out = append(out, ins)
		}
	}
	f.Code = out
	st.Removed = removed
	st.changed = true
	return st
}

// jumpTarget resolves a label name to its instruction index, or -1 when the
// label is undefined (Validate rejects such programs before any pass runs,
// so this is only defensive).
func jumpTarget(labelPos map[string]int, name string) int {
	pos, ok := labelPos[name]
	if !ok {
		return -1
	}
	return pos
}
