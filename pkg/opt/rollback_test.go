package opt

import (
	"strings"
	"testing"

	"github.com/guoyyit/bytecode-optimizer-go/pkg/parse"
)

func TestInvalidInputRollsBack(t *testing.T) {
	// Valid parse, invalid semantics: jump to an undefined label. Parse
	// accepts it (labels and jumps are both just names lexically) and
	// Validate must reject it before any mutation.
	src := `func main
  push 0
  jz ghost
  ret
end
`
	p, err := parse.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	before := p.Format()
	op, rep := Optimize(p, DefaultConfig())
	if !rep.Failed {
		t.Fatal("expected failure")
	}
	if rep.Kind != FailureInvalidInput {
		t.Fatalf("kind = %s, want invalid_input", rep.Kind)
	}
	if !strings.Contains(rep.Reason, "ghost") {
		t.Fatalf("reason should name missing label, got %q", rep.Reason)
	}
	if op != p {
		t.Fatal("must return the original program pointer on rollback")
	}
	if p.Format() != before {
		t.Fatal("original program was mutated on failure")
	}
}

func TestInvalidCallArityRollsBack(t *testing.T) {
	src := `func add(a, b)
  load a
  load b
  add
  ret
end

func main
  push 1
  call add 1
  ret
end
`
	p, err := parse.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	_, rep := Optimize(p, DefaultConfig())
	if !rep.Failed || rep.Kind != FailureInvalidInput {
		t.Fatalf("want invalid input rollback, got failed=%v kind=%s reason=%s",
			rep.Failed, rep.Kind, rep.Reason)
	}
	if !strings.Contains(rep.Reason, "expects 2 args") {
		t.Fatalf("reason should describe arity, got %q", rep.Reason)
	}
}

func TestUnknownPassNameRejected(t *testing.T) {
	src := `func main
  push 1
  ret
end
`
	p, _ := parse.Parse(src)
	_, rep := Optimize(p, Config{Passes: map[string]bool{"bogus": true}})
	if !rep.Failed || rep.Kind != FailureInvalidInput {
		t.Fatalf("want invalid input for unknown pass, got %+v", rep)
	}
}

func TestFailureKindsAreDistinguishable(t *testing.T) {
	if FailureInvalidInput.String() == FailureInternal.String() {
		t.Fatal("failure kinds must render distinctly")
	}
	if FailureNone.String() != "none" {
		t.Fatalf("none kind = %q", FailureNone.String())
	}
}
