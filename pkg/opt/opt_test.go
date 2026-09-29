package opt_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/guoyyit/bytecode-optimizer-go/pkg/bytecode"
	"github.com/guoyyit/bytecode-optimizer-go/pkg/opt"
	"github.com/guoyyit/bytecode-optimizer-go/pkg/parser"
	"github.com/guoyyit/bytecode-optimizer-go/pkg/vm"
)

func mustParse(t *testing.T, src string) *bytecode.Program {
	t.Helper()
	p, err := parser.Parse(strings.NewReader(src))
	if err != nil {
		t.Fatalf("parse: %v\nsrc:\n%s", err, src)
	}
	return p
}

func runProg(p *bytecode.Program) (string, error) {
	var buf bytes.Buffer
	err := vm.New(p, &buf).Run()
	return buf.String(), err
}

func allOn() opt.Config {
	return opt.Config{
		ConstantFold:   true,
		DeadCodeElim:   true,
		JumpThreading:  true,
		RedundantMerge: true,
	}
}

// diffRun 在参考解释器上分别执行优化前后程序，比较可观测输出与错误类别。
func diffRun(t *testing.T, p *bytecode.Program, cfg opt.Config) (*bytecode.Program, *opt.Report) {
	t.Helper()
	outBefore, errBefore := runProg(p)

	op, rep, err := opt.Optimize(p, cfg)
	if err != nil {
		t.Fatalf("optimize: %v", err)
	}
	if !rep.Stable {
		t.Fatalf("expected stable fixpoint")
	}

	outAfter, errAfter := runProg(op)
	if outBefore != outAfter {
		t.Fatalf("output mismatch:\nbefore=%q\nafter =%q", outBefore, outAfter)
	}
	if (errBefore == nil) != (errAfter == nil) {
		t.Fatalf("error presence mismatch: before=%v after=%v", errBefore, errAfter)
	}
	if errBefore != nil {
		rb, rok := errBefore.(*vm.Error)
		ra, aok := errAfter.(*vm.Error)
		if !rok || !aok {
			t.Fatalf("errors must be *vm.Error: before=%v after=%v", errBefore, errAfter)
		}
		if rb.Reason != ra.Reason {
			t.Fatalf("error reason mismatch: before=%q after=%q", rb.Reason, ra.Reason)
		}
	}
	return op, rep
}

func TestDifferentialArithmetic(t *testing.T) {
	src := `
func main 0 1
  push 7
  push 6
  mul
  push 5
  add
  push 3
  div
  print
  push 10
  push 3
  mod
  print
  halt
end`
	op, _ := diffRun(t, mustParse(t, src), allOn())
	out, _ := runProg(op)
	if out != "15\n1\n" {
		t.Fatalf("got %q", out)
	}
}

func TestDifferentialBranchLoopCall(t *testing.T) {
	src := `
func main 0 2
  push 0
  store 1 s
  push 1
  store 0 i
label loop
  load 0 i
  push 6
  ge
  jnz done
  load 0 i
  call isEven 1
  jnz odd
  load 1 s
  load 0 i
  add
  store 1 s
label odd
  load 0 i
  push 1
  add
  store 0 i
  jmp loop
label done
  load 1 s
  print
  halt
end
func isEven 1 1
  load 0 n
  push 2
  mod
  ret
end`
	// sum of even numbers in [1,5] = 2+4 = 6
	op, _ := diffRun(t, mustParse(t, src), allOn())
	out, _ := runProg(op)
	if out != "6\n" {
		t.Fatalf("got %q", out)
	}
}

func TestDifferentialDivisionByZero(t *testing.T) {
	src := `
func main 0 0
  push 10
  push 5
  push 5
  sub
  div
  print
  halt
end`
	// 5-5=0 的减法可安全折叠；随后的 10/0 不得被折叠，除零异常须保留。
	op, rep, err := opt.Optimize(mustParse(t, src), allOn())
	if err != nil {
		t.Fatal(err)
	}
	if folded := totalFolded(rep); folded != 1 {
		t.Fatalf("only the subtraction may fold, div must remain; folded=%d", folded)
	}
	// 优化后必须仍存在 div 指令。
	if !hasOp(op, "main", bytecode.OpDiv) {
		t.Fatalf("div instruction must be preserved")
	}
	before, e1 := runProg(mustParse(t, src))
	after, e2 := runProg(op)
	if before != after {
		t.Fatalf("output %q vs %q", before, after)
	}
	if e1 == nil || e2 == nil {
		t.Fatalf("expected divide-by-zero error both sides: %v %v", e1, e2)
	}
	if !strings.Contains(e2.Error(), "divide by zero") {
		t.Fatalf("expected div by zero, got %v", e2)
	}
}

func TestDifferentialOverflow(t *testing.T) {
	src := `
func main 0 0
  push 9223372036854775807
  push 1
  add
  print
  halt
end`
	before, e1 := runProg(mustParse(t, src))
	op, _, err := opt.Optimize(mustParse(t, src), allOn())
	if err != nil {
		t.Fatal(err)
	}
	after, e2 := runProg(op)
	if before != after || e1 == nil || e2 == nil {
		t.Fatalf("overflow must be preserved: %q/%v vs %q/%v", before, e1, after, e2)
	}
	if !strings.Contains(e2.Error(), "overflow") {
		t.Fatalf("expected overflow, got %v", e2)
	}
}

func totalFolded(rep *opt.Report) int {
	n := 0
	for _, r := range rep.Rounds {
		for _, s := range r.Passes {
			n += s.Folded
		}
	}
	return n
}

func hasOp(p *bytecode.Program, fn string, op bytecode.Opcode) bool {
	for _, in := range p.Func(fn).Code {
		if in.Op == op {
			return true
		}
	}
	return false
}
