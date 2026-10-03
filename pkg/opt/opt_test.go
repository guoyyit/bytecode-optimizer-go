package opt

import (
	"fmt"
	"strings"
	"testing"

	"github.com/guoyyit/bytecode-optimizer-go/pkg/interp"
	"github.com/guoyyit/bytecode-optimizer-go/pkg/ir"
	"github.com/guoyyit/bytecode-optimizer-go/pkg/parse"
)

func mustProg(t *testing.T, src string) *ir.Program {
	t.Helper()
	p, err := parse.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := Validate(p); err != nil {
		t.Fatalf("validate: %v", err)
	}
	return p
}

func cfgOnly(name string) Config {
	return Config{Passes: map[string]bool{name: true}}
}

// corpus holds programs exercising arithmetic, branches, loops and calls.
var corpus = []string{
	`func main
  push 3
  push 4
  add
  push 2
  mul
  print
  ret
end
`,
	`func isEven(n)
  load n
  push 2
  mod
  push 0
  eq
  ret
end

func main
  push 7
  call isEven 1
  print
  push 8
  call isEven 1
  print
  ret
end
`,
	`func loop(n)
  push 0
  store acc
top:
  load n
  jz done
  load acc
  load n
  add
  store acc
  load n
  push 1
  sub
  store n
  jmp top
done:
  load acc
  ret
end

func main
  push 10
  call loop 1
  print
  ret
end
`,
	`func fact(n)
  load n
  push 1
  gt
  jz base
  load n
  load n
  push 1
  sub
  call fact 1
  mul
  ret
base:
  push 1
  ret
end

func main
  push 6
  call fact 1
  ret
end
`,
	`func main
  push 5
  neg
  neg
  push 1
  mul
  add
  push 0
  add
  print
  jmp tail
  push 9
  print
tail:
  push 1
  ret
end
`,
}

func TestDifferentialAllPasses(t *testing.T) {
	configs := []Config{
		DefaultConfig(),
		cfgOnly(PassConstantFold),
		cfgOnly(PassDCE),
		cfgOnly(PassJump),
		cfgOnly(PassPeephole),
	}
	for i, src := range corpus {
		p := mustProg(t, src)
		for _, cfg := range configs {
			op, rep := Optimize(p, cfg)
			if rep.Failed {
				t.Fatalf("program %d passes %v: optimization failed: %s", i, enabled(cfg), rep.Reason)
			}
			v1, o1, e1 := interp.Run(p, "main")
			v2, o2, e2 := interp.Run(op, "main")
			if !sameResult(v1, o1, e1, v2, o2, e2) {
				t.Fatalf("program %d passes %v:\n before v=%d out=%q err=%v\n after  v=%d out=%q err=%v",
					i, enabled(cfg), v1, o1, e1, v2, o2, e2)
			}
		}
	}
}

func enabled(cfg Config) []string {
	var out []string
	for _, name := range AllPasses {
		if cfg.Passes[name] {
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		return append([]string(nil), AllPasses...)
	}
	return out
}

func sameResult(v1 int64, o1 string, e1 error, v2 int64, o2 string, e2 error) bool {
	if v1 != v2 || o1 != o2 {
		return false
	}
	return errKey(e1) == errKey(e2)
}

func errKey(e error) string {
	if e == nil {
		return ""
	}
	return e.Error()
}

func TestConstantFoldDoesNotSwallowDivByZero(t *testing.T) {
	src := `func main
  push 10
  push 0
  div
  print
  ret
end
`
	p := mustProg(t, src)
	op, rep := Optimize(p, cfgOnly(PassConstantFold))
	if rep.Failed {
		t.Fatal(rep.Reason)
	}
	if rep.TotalRemoved != 0 {
		t.Fatalf("div by zero must not be folded, removed=%d", rep.TotalRemoved)
	}
	_, _, err := interp.Run(op, "main")
	if err == nil || !strings.Contains(err.Error(), "divide by zero") {
		t.Fatalf("runtime error must survive: %v", err)
	}
}

func TestFixedPointIdempotent(t *testing.T) {
	for i, src := range corpus {
		p := mustProg(t, src)
		op1, r1 := Optimize(p, DefaultConfig())
		if r1.Failed || !r1.Stable {
			t.Fatalf("program %d: first run failed/stable=%v", i, r1.Stable)
		}
		op2, r2 := Optimize(op1, DefaultConfig())
		if r2.Failed {
			t.Fatalf("program %d: second run failed: %s", i, r2.Reason)
		}
		if op2.Format() != op1.Format() {
			t.Fatalf("program %d: already-optimal program changed:\n%s\n---\n%s",
				i, op1.Format(), op2.Format())
		}
		if r2.TotalRemoved != 0 || r2.Iterations != 1 {
			t.Fatalf("program %d: second run should be a no-op, removed=%d iters=%d",
				i, r2.TotalRemoved, r2.Iterations)
		}
	}
}

func TestStatsReportCostAccounting(t *testing.T) {
	p := mustProg(t, corpus[0])
	before := programCost(p)
	op, rep := Optimize(p, DefaultConfig())
	if rep.Failed {
		t.Fatal(rep.Reason)
	}
	if rep.CostAfter > before || rep.CostAfter != programCost(op) {
		t.Fatalf("cost accounting wrong: before=%d actual=%d report=%d", before, programCost(op), rep.CostAfter)
	}
	if rep.CostAfter >= rep.CostBefore {
		t.Fatalf("expected positive savings, %d -> %d", rep.CostBefore, rep.CostAfter)
	}
	for _, s := range rep.Passes {
		if s.CostAfter > s.CostBefore {
			t.Fatalf("pass %s increased cost %d -> %d", s.Name, s.CostBefore, s.CostAfter)
		}
	}
}

// countAllInstrs returns the total number of instructions (labels included)
// across every function.
func countAllInstrs(p *ir.Program) int {
	n := 0
	for _, f := range p.Funcs {
		n += len(f.Code)
	}
	return n
}

// TestUnreferencedLabelFixedPoint verifies the core bug fix: a program with
// an unreferenced label must converge on the first run, and the output must
// be byte-identical when re-optimized with all stats zero.
func TestUnreferencedLabelFixedPoint(t *testing.T) {
	src := `func main
  push 1
  ret
dead:
  push 2
  print
end
`
	p := mustProg(t, src)
	op1, r1 := Optimize(p, DefaultConfig())
	if r1.Failed {
		t.Fatalf("first run failed: %s", r1.Reason)
	}
	if !r1.Stable {
		t.Fatalf("first run must be stable, got stable=%v", r1.Stable)
	}
	if r1.TotalRemoved == 0 {
		t.Fatalf("first run must remove the unreferenced label and dead code, got removed=0")
	}
	// Stats must account for every instruction the output dropped.
	if diff := countAllInstrs(p) - countAllInstrs(op1); diff != r1.TotalRemoved {
		t.Fatalf("total_removed=%d but instruction diff=%d", r1.TotalRemoved, diff)
	}
	op2, r2 := Optimize(op1, DefaultConfig())
	if r2.Failed {
		t.Fatalf("second run failed: %s", r2.Reason)
	}
	if op2.Format() != op1.Format() {
		t.Fatalf("output changed on re-optimization:\n%s\n---\n%s", op1.Format(), op2.Format())
	}
	if r2.TotalRemoved != 0 || r2.Iterations != 1 || !r2.Stable {
		t.Fatalf("second run must be a no-op, removed=%d iters=%d stable=%v",
			r2.TotalRemoved, r2.Iterations, r2.Stable)
	}
}

// TestLabelStructuresFixedPoint covers label-to-label jumps, interleaved
// labels and jump cycles. Every shape must converge to a true fixed point.
func TestLabelStructuresFixedPoint(t *testing.T) {
	cases := []string{
		// label-to-label chain with unreferenced labels interleaved
		`func main
  push 1
  ret
unref:
  push 2
  print
a:
  jmp b
b:
  jmp c
c:
  push 3
  ret
end
`,
		// interleaved referenced and unreferenced labels in live code
		`func main
  push 1
live1:
  push 2
  jmp live2
dead1:
  push 9
  print
live2:
  push 3
  ret
end
`,
		// jump cycle (infinite loop) with unreferenced labels around it
		`func main
  push 0
  jmp loop
loop:
  push 1
  pop
  jmp loop
dead:
  push 2
  print
end
`,
		// unreferenced label at function entry
		`func main
entry:
  push 1
  ret
end
`,
		// only an unreferenced label after a terminator
		`func main
  push 1
  ret
dead:
end
`,
	}
	for i, src := range cases {
		p := mustProg(t, src)
		op1, r1 := Optimize(p, DefaultConfig())
		if r1.Failed {
			t.Fatalf("case %d: first run failed: %s", i, r1.Reason)
		}
		if !r1.Stable {
			t.Fatalf("case %d: first run not stable", i)
		}
		if diff := countAllInstrs(p) - countAllInstrs(op1); diff != r1.TotalRemoved {
			t.Fatalf("case %d: total_removed=%d but diff=%d", i, r1.TotalRemoved, diff)
		}
		op2, r2 := Optimize(op1, DefaultConfig())
		if r2.Failed {
			t.Fatalf("case %d: second run failed: %s", i, r2.Reason)
		}
		if op2.Format() != op1.Format() {
			t.Fatalf("case %d: output changed on re-optimization:\n%s\n---\n%s", i, op1.Format(), op2.Format())
		}
		if r2.TotalRemoved != 0 || r2.Iterations != 1 {
			t.Fatalf("case %d: second run must be a no-op, removed=%d iters=%d", i, r2.TotalRemoved, r2.Iterations)
		}
	}
}

// TestSinglePassIdempotent asserts each pass is idempotent on its own output:
// running the same pass again on an already-optimized program must not change
// anything.
func TestSinglePassIdempotent(t *testing.T) {
	src := `func main
  push 1
  push 2
  add
  print
  jmp tail
  push 9
  print
tail:
  push 3
  pop
  push 4
  neg
  neg
  store x
  load x
  store x
  ret
end
`
	p := mustProg(t, src)
	for _, name := range AllPasses {
		cfg := cfgOnly(name)
		op1, r1 := Optimize(p, cfg)
		if r1.Failed {
			t.Fatalf("pass %s: first run failed: %s", name, r1.Reason)
		}
		op2, r2 := Optimize(op1, cfg)
		if r2.Failed {
			t.Fatalf("pass %s: second run failed: %s", name, r2.Reason)
		}
		if op2.Format() != op1.Format() {
			t.Fatalf("pass %s: not idempotent:\n%s\n---\n%s", name, op1.Format(), op2.Format())
		}
		if r2.TotalRemoved != 0 || r2.Iterations != 1 {
			t.Fatalf("pass %s: second run must be a no-op, removed=%d iters=%d",
				name, r2.TotalRemoved, r2.Iterations)
		}
	}
}

// TestCombinationIdempotent asserts arbitrary pass combinations are
// idempotent: the output of any combination, re-optimized with the same
// combination, must be byte-identical with zero stats.
func TestCombinationIdempotent(t *testing.T) {
	src := `func main
  push 1
  push 2
  mul
  print
  jmp a
  push 9
  print
a:
  jmp b
b:
  push 3
  pop
  ret
end
`
	p := mustProg(t, src)
	combos := []Config{
		{Passes: map[string]bool{PassConstantFold: true, PassJump: true}},
		{Passes: map[string]bool{PassPeephole: true, PassDCE: true}},
		{Passes: map[string]bool{PassConstantFold: true, PassPeephole: true, PassDCE: true}},
		{Passes: map[string]bool{PassJump: true, PassDCE: true}},
		DefaultConfig(),
	}
	for i, cfg := range combos {
		op1, r1 := Optimize(p, cfg)
		if r1.Failed {
			t.Fatalf("combo %d: first run failed: %s", i, r1.Reason)
		}
		op2, r2 := Optimize(op1, cfg)
		if r2.Failed {
			t.Fatalf("combo %d: second run failed: %s", i, r2.Reason)
		}
		if op2.Format() != op1.Format() {
			t.Fatalf("combo %d: not idempotent:\n%s\n---\n%s", i, op1.Format(), op2.Format())
		}
		if r2.TotalRemoved != 0 || r2.Iterations != 1 {
			t.Fatalf("combo %d: second run must be a no-op, removed=%d iters=%d",
				i, r2.TotalRemoved, r2.Iterations)
		}
	}
}

// genDeepChain builds a program with a deep jump chain l0->l1->...->lN,
// with an unreferenced label and dead code interleaved between every link.
func genDeepChain(n int) string {
	var b strings.Builder
	b.WriteString("func main\n")
	b.WriteString("  jmp l0\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "dead%d:\n", i)
		fmt.Fprintf(&b, "  push %d\n", i)
		b.WriteString("  print\n")
		fmt.Fprintf(&b, "l%d:\n", i)
		if i < n-1 {
			fmt.Fprintf(&b, "  jmp l%d\n", i+1)
		}
	}
	fmt.Fprintf(&b, "l%d:\n", n)
	b.WriteString("  push 42\n")
	b.WriteString("  ret\n")
	b.WriteString("end\n")
	return b.String()
}

// TestLargeProgramConvergence asserts that programs with thousands of labels
// and deep jump chains converge in a bounded number of iterations that does
// not grow with program size, and that the output is a true fixed point.
func TestLargeProgramConvergence(t *testing.T) {
	sizes := []int{100, 1000, 5000}
	var prevIters int
	for si, n := range sizes {
		src := genDeepChain(n)
		p := mustProg(t, src)
		op1, r1 := Optimize(p, DefaultConfig())
		if r1.Failed {
			t.Fatalf("size %d: failed: %s", n, r1.Reason)
		}
		if !r1.Stable {
			t.Fatalf("size %d: not stable after %d iterations", n, r1.Iterations)
		}
		// Convergence rounds must be bounded: they must not grow with n.
		if r1.Iterations > 4 {
			t.Fatalf("size %d: converged in %d iterations, want <= 4", n, r1.Iterations)
		}
		if si > 0 && r1.Iterations > prevIters {
			t.Fatalf("size %d: iterations grew %d -> %d with program size", n, prevIters, r1.Iterations)
		}
		prevIters = r1.Iterations
		if diff := countAllInstrs(p) - countAllInstrs(op1); diff != r1.TotalRemoved {
			t.Fatalf("size %d: total_removed=%d but diff=%d", n, r1.TotalRemoved, diff)
		}
		op2, r2 := Optimize(op1, DefaultConfig())
		if r2.Failed {
			t.Fatalf("size %d: second run failed: %s", n, r2.Reason)
		}
		if op2.Format() != op1.Format() {
			t.Fatalf("size %d: output changed on re-optimization", n)
		}
		if r2.TotalRemoved != 0 || r2.Iterations != 1 {
			t.Fatalf("size %d: second run must be a no-op, removed=%d iters=%d",
				n, r2.TotalRemoved, r2.Iterations)
		}
		// Behavior must be preserved: the chain always reaches push 42.
		v1, _, e1 := interp.Run(p, "main")
		v2, _, e2 := interp.Run(op1, "main")
		if v1 != v2 || (e1 == nil) != (e2 == nil) {
			t.Fatalf("size %d: behavior changed: %d/%v vs %d/%v", n, v1, e1, v2, e2)
		}
	}
}

// TestLargeLoopConvergence covers a program with a backward jump (loop) and
// many unreferenced labels, ensuring cycles converge too.
func TestLargeLoopConvergence(t *testing.T) {
	var b strings.Builder
	b.WriteString("func main\n")
	b.WriteString("  push 0\n")
	b.WriteString("  store acc\n")
	b.WriteString("  push 1000\n")
	b.WriteString("  store i\n")
	b.WriteString("loop:\n")
	b.WriteString("  load i\n")
	b.WriteString("  jz done\n")
	for i := 0; i < 500; i++ {
		fmt.Fprintf(&b, "dead%d:\n", i)
		fmt.Fprintf(&b, "  push %d\n", i%7)
		b.WriteString("  pop\n")
	}
	b.WriteString("  load acc\n")
	b.WriteString("  load i\n")
	b.WriteString("  add\n")
	b.WriteString("  store acc\n")
	b.WriteString("  load i\n")
	b.WriteString("  push 1\n")
	b.WriteString("  sub\n")
	b.WriteString("  store i\n")
	b.WriteString("  jmp loop\n")
	b.WriteString("done:\n")
	b.WriteString("  load acc\n")
	b.WriteString("  print\n")
	b.WriteString("  ret\n")
	b.WriteString("end\n")
	src := b.String()
	p := mustProg(t, src)
	op1, r1 := Optimize(p, DefaultConfig())
	if r1.Failed {
		t.Fatalf("failed: %s", r1.Reason)
	}
	if !r1.Stable || r1.Iterations > 4 {
		t.Fatalf("converged in %d iterations stable=%v, want <= 4 and stable", r1.Iterations, r1.Stable)
	}
	op2, r2 := Optimize(op1, DefaultConfig())
	if r2.Failed || op2.Format() != op1.Format() || r2.TotalRemoved != 0 || r2.Iterations != 1 {
		t.Fatalf("second run must be a no-op: failed=%v changed=%v removed=%d iters=%d",
			r2.Failed, op2.Format() != op1.Format(), r2.TotalRemoved, r2.Iterations)
	}
	v1, _, e1 := interp.Run(p, "main")
	v2, _, e2 := interp.Run(op1, "main")
	if v1 != v2 || (e1 == nil) != (e2 == nil) {
		t.Fatalf("behavior changed: %d/%v vs %d/%v", v1, e1, v2, e2)
	}
}
