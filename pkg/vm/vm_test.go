package vm_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/guoyyit/bytecode-optimizer-go/pkg/parser"
	"github.com/guoyyit/bytecode-optimizer-go/pkg/vm"
)

func run(t *testing.T, src string) (string, error) {
	t.Helper()
	p, err := parser.Parse(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	err = vm.New(p, &buf).Run()
	return buf.String(), err
}

func TestRecursiveFactorial(t *testing.T) {
	src := `
func main 0 0
  push 5
  call fact 1
  print
  halt
end
func fact 1 1
  load 0 n
  push 1
  le
  jz rec
  push 1
  ret
label rec
  load 0 n
  load 0 n
  push 1
  sub
  call fact 1
  mul
  ret
end`
	out, err := run(t, src)
	if err != nil {
		t.Fatal(err)
	}
	if out != "120\n" {
		t.Fatalf("5! = 120, got %q", out)
	}
}

func TestHaltStopsWholeProgram(t *testing.T) {
	// halt 出现在被调函数中时必须终结整个程序，而不是仅返回到调用者继续执行。
	src := `
func main 0 0
  call stop 0
  push 7
  print
  halt
end
func stop 0 0
  push 99
  print
  halt
end`
	out, err := run(t, src)
	if err != nil {
		t.Fatal(err)
	}
	if out != "99\n" {
		t.Fatalf("halt must stop the whole program, got %q", out)
	}
}

func TestRuntimeErrorIsLocated(t *testing.T) {
	src := `
func main 0 0
  push 1
  push 0
  div
  print
  halt
end`
	_, err := run(t, src)
	if err == nil {
		t.Fatalf("expected error")
	}
	ve, ok := err.(*vm.Error)
	if !ok {
		t.Fatalf("want *vm.Error, got %T", err)
	}
	if ve.FuncName != "main" || !strings.Contains(ve.Reason, "divide by zero") {
		t.Fatalf("bad error location/reason: %+v", ve)
	}
}
