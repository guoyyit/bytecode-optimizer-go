package interp

import (
	"strings"
	"testing"

	"github.com/guoyyit/bytecode-optimizer-go/pkg/ir"
	"github.com/guoyyit/bytecode-optimizer-go/pkg/parse"
)

func mustParse(t *testing.T, src string) *ir.Program {
	t.Helper()
	p, err := parse.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return p
}

func TestArithmeticAndComparisons(t *testing.T) {
	src := `func main
  push 20
  push 3
  add
  push 2
  mul
  push 7
  sub
  push 10
  div
  push 10
  mod
  print
  push 3
  push 3
  eq
  print
  push 2
  push 5
  lt
  print
  push 5
  push 2
  gt
  print
  push 8
  neg
  print
  push 8
  neg
  ret
end
`
	p := mustParse(t, src)
	v, out, err := Run(p, "main")
	if err != nil {
		t.Fatal(err)
	}
	wantOut := "3\n1\n1\n1\n-8\n"
	if out != wantOut {
		t.Fatalf("out = %q, want %q", out, wantOut)
	}
	if v != -8 {
		t.Fatalf("result = %d, want -8", v)
	}
}

func TestBranchingAndLoop(t *testing.T) {
	// sum 1..5 via a loop = 15
	src := `func main
  push 0
  store sum
  push 1
  store i
loop:
  load i
  push 5
  gt
  jnz done
  load sum
  load i
  add
  store sum
  load i
  push 1
  add
  store i
  jmp loop
done:
  load sum
  ret
end
`
	p := mustParse(t, src)
	v, _, err := Run(p, "main")
	if err != nil {
		t.Fatal(err)
	}
	if v != 15 {
		t.Fatalf("sum = %d, want 15", v)
	}
}

func TestCallsAndRecursion(t *testing.T) {
	src := `func fib(n)
  load n
  push 2
  lt
  jnz base
  load n
  push 1
  sub
  call fib 1
  load n
  push 2
  sub
  call fib 1
  add
  ret
base:
  load n
  ret
end

func main
  push 10
  call fib 1
  ret
end
`
	p := mustParse(t, src)
	v, _, err := Run(p, "main")
	if err != nil {
		t.Fatal(err)
	}
	if v != 55 {
		t.Fatalf("fib(10) = %d, want 55", v)
	}
}

func TestEntryArgs(t *testing.T) {
	src := `func square(x)
  load x
  load x
  mul
  ret
end
`
	p := mustParse(t, src)
	v, _, err := Run(p, "square", 9)
	if err != nil {
		t.Fatal(err)
	}
	if v != 81 {
		t.Fatalf("square(9) = %d, want 81", v)
	}
}

func TestDivisionByZeroReportsSourceLine(t *testing.T) {
	src := "func main\n  push 1\n  push 0\n  div\n  ret\nend\n"
	p := mustParse(t, src)
	_, _, err := Run(p, "main")
	if err == nil {
		t.Fatal("expected runtime error")
	}
	rerr, ok := err.(*RuntimeError)
	if !ok {
		t.Fatalf("want *RuntimeError, got %T", err)
	}
	if rerr.SrcLine != 4 {
		t.Fatalf("error line = %d, want 4", rerr.SrcLine)
	}
	if !strings.Contains(rerr.Message, "divide by zero") {
		t.Fatalf("message = %q", rerr.Message)
	}
}
