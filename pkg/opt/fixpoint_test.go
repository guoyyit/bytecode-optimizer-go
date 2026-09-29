package opt_test

import (
	"strings"
	"testing"

	"github.com/guoyyit/bytecode-optimizer-go/pkg/bytecode"
	"github.com/guoyyit/bytecode-optimizer-go/pkg/opt"
	"github.com/guoyyit/bytecode-optimizer-go/pkg/parser"
)

// canonicalText 用 printer 规范化输出，便于比较两个程序是否逐指令相同。
func canonicalText(t *testing.T, p *bytecode.Program) string {
	t.Helper()
	var b strings.Builder
	if err := parser.Print(&b, p); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestFixpointIdempotent(t *testing.T) {
	src := `
func main 0 1
  push 3
  print
  push 0
  store 0 i
label head
  load 0 i
  push 5
  ge
  jnz end
  load 0 i
  push 1
  add
  store 0 i
  jmp head
label end
  halt
end`
	p := mustParse(t, src)

	first, rep1, err := opt.Optimize(p, allOn())
	if err != nil {
		t.Fatal(err)
	}
	if !rep1.Stable {
		t.Fatalf("first run must converge")
	}

	second, rep2, err := opt.Optimize(first, allOn())
	if err != nil {
		t.Fatal(err)
	}
	if !rep2.Stable {
		t.Fatalf("second run must converge immediately")
	}
	if canonicalText(t, first) != canonicalText(t, second) {
		t.Fatalf("re-optimizing an optimal program changed its contents")
	}

	// 第二轮管线里所有遍都必须报告零变化。
	if len(rep2.Rounds) != 1 {
		t.Fatalf("expected a single confirming round, got %d", len(rep2.Rounds))
	}
	for _, st := range rep2.Rounds[0].Passes {
		if st.Changed() {
			t.Fatalf("pass %s changed an already optimal program", st.Name)
		}
	}
}

func TestEachPassIndependentlyConverges(t *testing.T) {
	src := `
func main 0 0
  push 4
  push 3
  add
  push 9
  pop
  print
  jmp skip
  push 1
  print
label skip
  halt
end`
	p := mustParse(t, src)

	passes := []struct {
		name string
		cfg  opt.Config
	}{
		{"constfold", opt.Config{ConstantFold: true}},
		{"dce", opt.Config{DeadCodeElim: true}},
		{"jumpthread", opt.Config{JumpThreading: true}},
		{"peephole", opt.Config{RedundantMerge: true}},
	}
	for _, pc := range passes {
		op, rep, err := opt.Optimize(p, pc.cfg)
		if err != nil {
			t.Fatalf("%s: %v", pc.name, err)
		}
		if !rep.Stable {
			t.Fatalf("%s: did not converge", pc.name)
		}
		op2, _, err := opt.Optimize(op, pc.cfg)
		if err != nil {
			t.Fatalf("%s re-run: %v", pc.name, err)
		}
		if canonicalText(t, op) != canonicalText(t, op2) {
			t.Fatalf("%s: not idempotent", pc.name)
		}
		// 语义保持。
		diffRun(t, p, pc.cfg)
	}
}

func TestCostDoesNotIncrease(t *testing.T) {
	src := `
func main 0 1
  push 2
  push 3
  mul
  push 8
  pop
  print
  halt
end`
	p := mustParse(t, src)
	_, rep, err := opt.Optimize(p, allOn())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rep.Rounds {
		for _, st := range r.Passes {
			if st.CostAfter > st.CostBefore {
				t.Fatalf("pass %s increased cost %d -> %d",
					st.Name, st.CostBefore, st.CostAfter)
			}
		}
	}
}
