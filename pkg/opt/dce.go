package opt

import "github.com/guoyyit/bytecode-optimizer-go/pkg/ir"

// passDCE removes instructions that can never execute: code following an
// unconditional terminator (RET or JMP) up to the next label marker. Labels
// themselves are never deleted here because they are the anchors used to keep
// every jump target relocatable; a separate cleanup prunes unreferenced
// labels once all passes are done.
func passDCE(f *ir.Func) Stats {
	st := Stats{Name: PassDCE}
	out := make([]ir.Instr, 0, len(f.Code))
	dead := false
	for _, ins := range f.Code {
		if ins.Op == ir.OpLabel {
			dead = false
			out = append(out, ins)
			continue
		}
		if dead {
			st.Removed++
			st.changed = true
			continue
		}
		if ins.Op == ir.OpRet || ins.Op == ir.OpJmp {
			dead = true
		}
		out = append(out, ins)
	}
	f.Code = out
	return st
}
