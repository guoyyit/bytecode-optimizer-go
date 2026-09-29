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

	chainTarget := func(name string) string {
		seen := map[string]bool{}
		for {
			if seen[name] {
				return name // defensive: chain cycles are rejected by Validate
			}
			seen[name] = true
			pos := findLabel(f.Code, name)
			if pos < 0 {
				return name
			}
			next := skipLabels(f.Code, pos+1)
			if next >= len(f.Code) || f.Code[next].Op != ir.OpJmp {
				return name
			}
			name = f.Code[next].Text
		}
	}

	out := make([]ir.Instr, 0, len(f.Code))
	for i := 0; i < len(f.Code); i++ {
		ins := f.Code[i]
		switch ins.Op {
		case ir.OpJmp, ir.OpJz, ir.OpJnz:
			dst := chainTarget(ins.Text)
			if dst != ins.Text {
				ins.Text = dst
				st.changed = true
			}
			if ins.Op == ir.OpJmp && redundantFallthrough(out, f.Code[i+1:], dst) {
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

func findLabel(code []ir.Instr, name string) int {
	for i, ins := range code {
		if ins.Op == ir.OpLabel && ins.Text == name {
			return i
		}
	}
	return -1
}

func skipLabels(code []ir.Instr, from int) int {
	for from < len(code) && code[from].Op == ir.OpLabel {
		from++
	}
	return from
}
