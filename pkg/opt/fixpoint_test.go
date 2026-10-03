package opt

import (
	"fmt"
	"strings"
	"testing"

	"github.com/guoyyit/bytecode-optimizer-go/pkg/interp"
	"github.com/guoyyit/bytecode-optimizer-go/pkg/ir"
)

// labelStructurePrograms exercises unreferenced labels and every label
// topology the pipeline must converge on: labels no jump references, labels
// hiding dead code, jump chains through labels, interleaved label clusters,
// reachable and unreachable reference cycles, self loops and degenerate
// label-only bodies.
var labelStructurePrograms = []struct {
	name string
	src  string
}{
	{"unreferenced label blocks peephole", `func main
  push 1
dead:
  pop
  ret
end
`},
	{"unreferenced label hides dead code", `func main
  ret
dead:
  push 1
  print
end
`},
	{"jump chain through labels", `func main
  jmp a
a:
  jmp b
b:
  jmp c
c:
  push 7
  print
  ret
end
`},
	{"interleaved label cluster blocks constfold", `func main
  push 2
u1:
u2:
  push 3
u3:
  add
  print
  ret
end
`},
	{"mixed referenced and unreferenced labels", `func main
  push 2
u1:
u2:
  push 3
u3:
  add
  print
  jmp done
u4:
  push 9
  print
done:
u5:
  ret
end
`},
	{"reachable label cycle", `func main
  push 1
  jnz skip
spin:
  jmp loop2
loop2:
  jmp spin
skip:
  push 5
  print
  ret
end
`},
	{"unreachable label cycle", `func main
  jmp go
spin:
  jmp loop2
loop2:
  jmp spin
go:
  push 5
  print
  ret
end
`},
	{"unreachable self loop", `func main
  jmp start
self:
  jmp self
start:
  push 3
  ret
end
`},
	{"conditional chain folding", `func main
  push 0
  jz a
  push 1
  print
a:
  jmp b
b:
  push 2
  print
  ret
end
`},
	{"label only body", `func main
only:
end
`},
	{"two unreferenced labels only", `func main
u1:
u2:
end
`},
}

func instrCount(p *ir.Program) int {
	n := 0
	for _, f := range p.Funcs {
		n += len(f.Code)
	}
	return n
}

// assertNoOpRerun optimizes an already-converged program and requires the
// run to be a pure no-op: byte-identical output, one iteration, every
// statistic at zero.
func assertNoOpRerun(t *testing.T, p *ir.Program, cfg Config) {
	t.Helper()
	op, rep := Optimize(p, cfg)
	if rep.Failed {
		t.Fatalf("rerun failed: %s", rep.Reason)
	}
	if !rep.Stable {
		t.Fatal("rerun of converged program must report stable")
	}
	if op.Format() != p.Format() {
		t.Fatalf("converged program changed:\n%s\n---\n%s", p.Format(), op.Format())
	}
	if rep.TotalRemoved != 0 || rep.Iterations != 1 {
		t.Fatalf("rerun should be a no-op, removed=%d iters=%d", rep.TotalRemoved, rep.Iterations)
	}
	if rep.CostBefore != rep.CostAfter {
		t.Fatalf("cost moved on converged input: %d -> %d", rep.CostBefore, rep.CostAfter)
	}
	for _, s := range rep.Passes {
		if s.Removed != 0 || s.CostBefore != s.CostAfter {
			t.Fatalf("pass %s did work on converged input: %+v", s.Name, s)
		}
	}
}

// assertStatsConsistent checks the reporting contract: every deletion is
// counted (TotalRemoved equals the instruction-count delta) and the stable
// declaration matches what actually happened.
func assertStatsConsistent(t *testing.T, before, after *ir.Program, rep *Report) {
	t.Helper()
	delta := instrCount(before) - instrCount(after)
	if rep.TotalRemoved != delta {
		t.Fatalf("TotalRemoved=%d but instruction delta is %d", rep.TotalRemoved, delta)
	}
	sum := 0
	for _, s := range rep.Passes {
		sum += s.Removed
	}
	if sum != rep.TotalRemoved {
		t.Fatalf("per-pass removals sum to %d, report says %d", sum, rep.TotalRemoved)
	}
	changed := before.Format() != after.Format()
	if changed && rep.Iterations < 2 {
		t.Fatalf("content changed but report claims stability after %d iteration(s)", rep.Iterations)
	}
	if changed && rep.TotalRemoved == 0 {
		t.Fatal("content changed but no transformation was counted")
	}
	if !changed && (rep.TotalRemoved != 0 || rep.Iterations != 1) {
		t.Fatalf("no-op run reported removed=%d iters=%d", rep.TotalRemoved, rep.Iterations)
	}
}

func TestLabelStructuresConvergeToTrueFixedPoint(t *testing.T) {
	for _, tc := range labelStructurePrograms {
		t.Run(tc.name, func(t *testing.T) {
			p := mustProg(t, tc.src)
			v1, o1, e1 := interp.Run(p, "main")

			op1, r1 := Optimize(p, DefaultConfig())
			if r1.Failed {
				t.Fatalf("first run failed: %s", r1.Reason)
			}
			if !r1.Stable {
				t.Fatal("first run must end stable")
			}
			assertStatsConsistent(t, p, op1, r1)

			v2, o2, e2 := interp.Run(op1, "main")
			if !sameResult(v1, o1, e1, v2, o2, e2) {
				t.Fatalf("behavior changed: before v=%d out=%q err=%v, after v=%d out=%q err=%v",
					v1, o1, e1, v2, o2, e2)
			}

			// Re-optimizing any number of times must be byte-identical.
			prev := op1
			for i := 0; i < 3; i++ {
				assertNoOpRerun(t, prev, DefaultConfig())
				next, _ := Optimize(prev, DefaultConfig())
				prev = next
			}
		})
	}
}

func TestSinglePassIdempotentOnLabelStructures(t *testing.T) {
	for _, tc := range labelStructurePrograms {
		for _, name := range AllPasses {
			cfg := cfgOnly(name)
			p := mustProg(t, tc.src)
			op1, r1 := Optimize(p, cfg)
			if r1.Failed {
				t.Fatalf("%s pass on %q failed: %s", name, tc.name, r1.Reason)
			}
			assertStatsConsistent(t, p, op1, r1)
			assertNoOpRerun(t, op1, cfg)
		}
	}
}

func allPassSubsets() []Config {
	var out []Config
	for mask := 1; mask < 1<<len(AllPasses); mask++ {
		cfg := Config{Passes: map[string]bool{}}
		for i, name := range AllPasses {
			if mask&(1<<i) != 0 {
				cfg.Passes[name] = true
			}
		}
		out = append(out, cfg)
	}
	return out
}

func TestPassSubsetIdempotentOnLabelStructures(t *testing.T) {
	for _, tc := range labelStructurePrograms {
		for _, cfg := range allPassSubsets() {
			p := mustProg(t, tc.src)
			op1, r1 := Optimize(p, cfg)
			if r1.Failed {
				t.Fatalf("%v on %q failed: %s", enabled(cfg), tc.name, r1.Reason)
			}
			assertStatsConsistent(t, p, op1, r1)
			assertNoOpRerun(t, op1, cfg)
		}
	}
}

func TestConvergedProgramImmuneToAnyPassSubset(t *testing.T) {
	// A program that reached the full-pipeline fixed point is a fixed point
	// of every individual pass, so no subset configuration may change it.
	for _, tc := range labelStructurePrograms {
		p := mustProg(t, tc.src)
		op1, r1 := Optimize(p, DefaultConfig())
		if r1.Failed {
			t.Fatalf("%q: %s", tc.name, r1.Reason)
		}
		for _, cfg := range allPassSubsets() {
			assertNoOpRerun(t, op1, cfg)
		}
	}
}

// genLargeLabelProgram builds a program with a deep jump chain of `chains`
// links, `junk` unreferenced label pairs interleaved with removable
// instruction pairs, and an unreachable dead chain of `chains` links.
func genLargeLabelProgram(chains, junk int) string {
	var b strings.Builder
	b.WriteString("func main\n  push 0\n  store acc\n  jmp c0\n")
	for i := 0; i < chains; i++ {
		fmt.Fprintf(&b, "c%d:\n  jmp c%d\n", i, i+1)
	}
	fmt.Fprintf(&b, "c%d:\n", chains)
	for i := 0; i < junk; i++ {
		fmt.Fprintf(&b, "u%d:\n  push 1\nv%d:\n  pop\n", i, i)
	}
	b.WriteString("  push 40\n  push 2\n  add\n  store acc\n  jmp done\n")
	for i := 0; i < chains; i++ {
		fmt.Fprintf(&b, "d%d:\n  jmp d%d\n", i, i+1)
	}
	fmt.Fprintf(&b, "d%d:\n  push 9\n  print\n", chains)
	b.WriteString("done:\n  load acc\n  print\n  ret\nend\n")
	return b.String()
}

func TestLargeScaleLabelProgramsConvergeInBoundedIterations(t *testing.T) {
	// The iteration budget is size-independent: convergence rounds must not
	// grow with the number of labels or the chain depth.
	const maxExpectedIterations = 6
	var iterations []int
	for _, size := range [][2]int{{2000, 1500}, {4000, 3000}} {
		src := genLargeLabelProgram(size[0], size[1])
		p := mustProg(t, src)
		v1, o1, e1 := interp.Run(p, "main")

		op1, r1 := Optimize(p, DefaultConfig())
		if r1.Failed {
			t.Fatalf("size %v: failed: %s", size, r1.Reason)
		}
		if !r1.Stable {
			t.Fatalf("size %v: did not converge", size)
		}
		if r1.Iterations > maxExpectedIterations {
			t.Fatalf("size %v: took %d iterations, want <= %d", size, r1.Iterations, maxExpectedIterations)
		}
		iterations = append(iterations, r1.Iterations)
		assertStatsConsistent(t, p, op1, r1)

		v2, o2, e2 := interp.Run(op1, "main")
		if !sameResult(v1, o1, e1, v2, o2, e2) {
			t.Fatalf("size %v: behavior changed: %d/%q/%v vs %d/%q/%v", size, v1, o1, e1, v2, o2, e2)
		}
		assertNoOpRerun(t, op1, DefaultConfig())
	}
	if iterations[1] != iterations[0] {
		t.Fatalf("iteration count grew with program size: %v", iterations)
	}
}
