package opt

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/guoyyit/bytecode-optimizer-go/pkg/parse"
)

// genRandomLabelProgram builds a program with arbitrary labels, jumps and
// stack-neutral instruction blocks. Every block keeps the operand stack at
// depth 0 and every label is defined exactly once, so the result is almost
// always a valid program. Termination is not guaranteed, so the generated
// programs are never executed — the fuzz checks convergence, idempotency
// and statistics, not behavior.
func genRandomLabelProgram(r *rand.Rand) string {
	neutral := [][]string{
		{"push 1", "pop"},
		{"push 2", "push 3", "add", "pop"},
		{"load x", "store x"},
		{"push 5", "neg", "neg", "pop"},
		{"push 1", "push 2", "sub", "pop"},
		{"push 4", "push 2", "mul", "pop"},
	}
	nLabels := 1 + r.Intn(30)
	nBlocks := 3 + r.Intn(30)

	// Assign every label to a random block boundary; several labels may land
	// on the same boundary, producing interleaved clusters.
	labelAt := map[int][]int{}
	for l := 0; l < nLabels; l++ {
		at := r.Intn(nBlocks + 1)
		labelAt[at] = append(labelAt[at], l)
	}

	var b strings.Builder
	b.WriteString("func main\n")
	emitLabels := func(at int) {
		for _, l := range labelAt[at] {
			fmt.Fprintf(&b, "L%d:\n", l)
		}
	}
	for i := 0; i < nBlocks; i++ {
		emitLabels(i)
		switch r.Intn(6) {
		case 0:
			fmt.Fprintf(&b, "  jmp L%d\n", r.Intn(nLabels))
		case 1:
			fmt.Fprintf(&b, "  push %d\n  jz L%d\n", r.Intn(2), r.Intn(nLabels))
		case 2:
			fmt.Fprintf(&b, "  push %d\n  jnz L%d\n", r.Intn(2), r.Intn(nLabels))
		case 3:
			b.WriteString("  ret\n")
		default:
			for _, ins := range neutral[r.Intn(len(neutral))] {
				b.WriteString("  " + ins + "\n")
			}
		}
	}
	emitLabels(nBlocks)
	b.WriteString("  ret\nend\n")
	return b.String()
}

// TestRandomLabelStructureFixedPoint fuzzes the pipeline with random label
// topologies: unreferenced labels, chains, clusters and cycles. Every valid
// program must converge (never roll back), reach a byte-stable fixed point
// with zero statistics on re-run, and have every deletion accounted for.
func TestRandomLabelStructureFixedPoint(t *testing.T) {
	r := rand.New(rand.NewSource(99))
	tried := 0
	for i := 0; i < 3000; i++ {
		src := genRandomLabelProgram(r)
		p, err := parse.Parse(src)
		if err != nil {
			continue
		}
		if err := Validate(p); err != nil {
			continue
		}
		tried++

		op1, r1 := Optimize(p, DefaultConfig())
		if r1.Failed {
			t.Fatalf("iter %d: valid program rolled back (%s): %s\n%s", i, r1.Kind, r1.Reason, src)
		}
		if !r1.Stable {
			t.Fatalf("iter %d: not stable\n%s", i, src)
		}
		assertStatsConsistent(t, p, op1, r1)
		assertNoOpRerun(t, op1, DefaultConfig())

		// The fixed point must be immune to every individual pass as well.
		for _, name := range AllPasses {
			assertNoOpRerun(t, op1, cfgOnly(name))
		}
	}
	if tried < 1000 {
		t.Fatalf("generator produced too few valid programs: %d", tried)
	}
}
