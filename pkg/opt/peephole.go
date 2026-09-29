package opt

import "github.com/guoyyit/bytecode-optimizer-go/pkg/ir"

// passPeephole merges provably redundant adjacent instructions. Every rule is
// net-stack-neutral (the delta of the removed sequence is 0), never crosses a
// label boundary (a jump may land between the pair), never removes a possible
// runtime fault and keeps source lines attached to surviving instructions:
//
//   - LOAD x; STORE x -> (removed)
//     pushes locals[x] and writes it straight back, leaving the stack and
//     locals exactly as they were (including the default-zero case).
//   - PUSH c; POP -> (removed)
//     a constant that is immediately discarded has no observable effect.
//   - NEG; NEG -> identity
//     holds even for the minimum int64 because two runtime NEGs wrap exactly
//     as doing nothing would (the value is restored, not computed as -(-v)).
//
// Algebraic identities such as PUSH 1; MUL or PUSH 0; ADD are intentionally
// NOT applied: deleting the pair changes the stack depth by 0 while the
// original two instructions consume the left operand, which would require a
// DUP opcode this ISA does not provide. Constant folding of such expressions
// happens in passConstantFold only when both operands are literals.
func passPeephole(f *ir.Func) Stats {
	st := Stats{Name: PassPeephole}
	out := make([]ir.Instr, 0, len(f.Code))
	i := 0
	for i < len(f.Code) {
		ins := f.Code[i]

		if ins.Op == ir.OpLabel || i+1 >= len(f.Code) || f.Code[i+1].Op == ir.OpLabel {
			out = append(out, ins)
			i++
			continue
		}
		next := f.Code[i+1]

		switch {
		case ins.Op == ir.OpLoad && next.Op == ir.OpStore && ins.Text == next.Text:
			st.Removed += 2
			st.changed = true
		case ins.Op == ir.OpPush && next.Op == ir.OpPop:
			st.Removed += 2
			st.changed = true
		case ins.Op == ir.OpNeg && next.Op == ir.OpNeg:
			st.Removed += 2
			st.changed = true
		default:
			out = append(out, ins)
			i++
			continue
		}
		i += 2
	}
	f.Code = out
	return st
}
