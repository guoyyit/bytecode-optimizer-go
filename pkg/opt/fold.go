package opt

import "github.com/guoyyit/bytecode-optimizer-go/pkg/ir"

// passConstantFold replaces PUSH a; PUSH b; <binop> with PUSH (a op b).
// Division/modulo by a literal zero is never folded because the interpreter
// raises a runtime error there, so the observable behavior must be preserved.
// The surviving PUSH keeps the binary op's source line: a runtime error can
// never originate from a folded instruction, and breakpoints map to the
// original expression's location.
func passConstantFold(f *ir.Func) Stats {
	st := Stats{Name: PassConstantFold}
	out := make([]ir.Instr, 0, len(f.Code))
	i := 0
	for i < len(f.Code) {
		if i+2 < len(f.Code) &&
			f.Code[i].Op == ir.OpPush &&
			f.Code[i+1].Op == ir.OpPush &&
			isFoldableBinary(f.Code[i+2].Op) {
			a := f.Code[i].Int
			b := f.Code[i+1].Int
			op := f.Code[i+2].Op
			if !((op == ir.OpDiv || op == ir.OpMod) && b == 0) {
				v, _ := ir.EvalBinary(op, a, b)
				out = append(out, ir.Instr{Op: ir.OpPush, Int: v, SrcLine: f.Code[i+2].SrcLine})
				st.Removed += 2
				i += 3
				st.changed = true
				continue
			}
		}
		out = append(out, f.Code[i])
		i++
	}
	f.Code = out
	return st
}

func isFoldableBinary(op ir.Op) bool {
	switch op {
	case ir.OpAdd, ir.OpSub, ir.OpMul, ir.OpDiv, ir.OpMod, ir.OpEq, ir.OpLt, ir.OpGt:
		return true
	default:
		return false
	}
}
