package opt

import "github.com/guoyyit/bytecode-optimizer-go/pkg/ir"

// passJumpFold performs two semantics-preserving rewrites:
//
//  1. Chain folding: "JMP A" where A is immediately "JMP B" (labels between
//     them allowed) is rewritten to "JMP B". Conditional jumps follow the
//     same chain. Chains form a functional graph (every label has at most one
//     outgoing chain edge), so each label's final target is resolved exactly
//     once and memoized: a whole pass is linear in the function size, no
//     matter how deep the chains are. A label on a reference cycle resolves
//     to itself and a label leading into a cycle resolves to the cycle
//     entry, so cyclic programs converge instead of spinning.
//  2. Redundant JMP removal: "JMP L" whose target is exactly where control
//     would continue anyway is deleted.
//
// Labels are never removed by this pass: they are the symbolic anchors that
// keep every jump target correct after instructions are deleted or reordered.
func passJumpFold(f *ir.Func) Stats {
	st := Stats{Name: PassJump}
	code := f.Code

	// nextInChain maps a label to the target of the JMP that immediately
	// follows it (labels in between allowed). One sweep: every label run
	// remembers the JMP that terminates it.
	nextInChain := map[string]string{}
	var pending []string
	for _, ins := range code {
		if ins.Op == ir.OpLabel {
			pending = append(pending, ins.Text)
			continue
		}
		if ins.Op == ir.OpJmp {
			for _, name := range pending {
				nextInChain[name] = ins.Text
			}
		}
		pending = pending[:0]
	}

	// resolve returns the final target of the chain starting at a label,
	// memoized per label. Resolution is iterative and detects cycles with an
	// on-path index, so even self-referential chains terminate.
	memo := make(map[string]string, len(nextInChain))
	resolve := func(start string) string {
		if dst, ok := memo[start]; ok {
			return dst
		}
		var path []string
		onPath := map[string]int{}
		cur := start
		for {
			if dst, ok := memo[cur]; ok {
				for _, name := range path {
					memo[name] = dst
				}
				return dst
			}
			next, ok := nextInChain[cur]
			if !ok {
				memo[cur] = cur
				for _, name := range path {
					memo[name] = cur
				}
				return cur
			}
			if j, cyc := onPath[cur]; cyc {
				// Cycle: members resolve to themselves, the labels leading
				// into it resolve to the cycle entry.
				memo[cur] = cur
				for _, name := range path[j:] {
					memo[name] = name
				}
				for _, name := range path[:j] {
					memo[name] = cur
				}
				return cur
			}
			onPath[cur] = len(path)
			path = append(path, cur)
			cur = next
		}
	}

	out := make([]ir.Instr, 0, len(code))
	for i := 0; i < len(code); i++ {
		ins := code[i]
		switch ins.Op {
		case ir.OpJmp, ir.OpJz, ir.OpJnz:
			dst := resolve(ins.Text)
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
