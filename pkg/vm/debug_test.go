package vm_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/guoyyit/bytecode-optimizer-go/pkg/bytecode"
	"github.com/guoyyit/bytecode-optimizer-go/pkg/opt"
	"github.com/guoyyit/bytecode-optimizer-go/pkg/parser"
	"github.com/guoyyit/bytecode-optimizer-go/pkg/vm"
)

const debugSrc = `
func main 0 1
  push 3
  print
  push 0
  store 0 i
label head
  load 0 i
  push 4
  ge
  jnz done
  load 0 i
  push 1
  add
  store 0 i
  jmp head
label done
  halt
end
`

func steps(t *testing.T, src string) []vm.StepEvent {
	t.Helper()
	p, err := parser.Parse(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	var got []vm.StepEvent
	v := vm.New(p, &bytes.Buffer{})
	v.OnStep(func(e vm.StepEvent) { got = append(got, e) })
	if err := v.Run(); err != nil {
		t.Fatal(err)
	}
	return got
}

// TestStepSequenceIsSubsequence: 优化后单步到的每个 OrigPC 都必须是
// 原始程序单步序列中的合法位置，且顺序一致（允许跳过被删除指令）。
func TestStepOrigPCConsistency(t *testing.T) {
	origSteps := steps(t, debugSrc)

	p, err := parser.Parse(strings.NewReader(debugSrc))
	if err != nil {
		t.Fatal(err)
	}
	op, rep, err := opt.Optimize(p, opt.Config{
		ConstantFold:   true,
		DeadCodeElim:   true,
		JumpThreading:  true,
		RedundantMerge: true,
	})
	if err != nil || !rep.Stable {
		t.Fatalf("optimize: %v stable=%v", err, rep.Stable)
	}

	var optSteps []vm.StepEvent
	v := vm.New(op, &bytes.Buffer{})
	v.OnStep(func(e vm.StepEvent) { optSteps = append(optSteps, e) })
	if err := v.Run(); err != nil {
		t.Fatal(err)
	}

	origPCSet := make(map[int]bool)
	for _, e := range origSteps {
		origPCSet[e.OrigPC] = true
	}

	// 优化后的每个停点必须映射回原始程序中真实存在的指令。
	for _, e := range optSteps {
		if !origPCSet[e.OrigPC] {
			t.Fatalf("optimized step maps to unknown original pc %d", e.OrigPC)
		}
	}

	// 优化后序列必须是原始序列的子序列（保持单步顺序）。
	j := 0
	for _, e := range optSteps {
		for j < len(origSteps) && origSteps[j].OrigPC != e.OrigPC {
			j++
		}
		if j >= len(origSteps) {
			t.Fatalf("optimized step order diverges at origPC %d", e.OrigPC)
		}
		j++
	}
}

// TestBreakpointHitsOriginalLocation: 在原始程序与优化后程序的同一
// “优化前 PC” 处下断，命中的源码位置必须一致。
func TestBreakpointMapsToOriginalLocation(t *testing.T) {
	breakPC := 10 // label 后 load 0 i（循环条件加载），在源程序中的索引

	parse := func() *bytecode.Program {
		p, err := parser.Parse(strings.NewReader(debugSrc))
		if err != nil {
			t.Fatal(err)
		}
		return p
	}

	countHits := func(p *bytecode.Program) int {
		hits := 0
		v := vm.New(p, &bytes.Buffer{})
		v.SetBreakpoint("main", breakPC)
		v.OnStep(func(e vm.StepEvent) {
			if e.Break {
				hits++
			}
		})
		if err := v.Run(); err != nil {
			t.Fatal(err)
		}
		return hits
	}

	if h := countHits(parse()); h != 4 {
		t.Fatalf("original loop condition evaluated 4 times, got %d", h)
	}

	op, _, err := opt.Optimize(parse(), opt.Config{
		ConstantFold: true, DeadCodeElim: true,
		JumpThreading: true, RedundantMerge: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if h := countHits(op); h != 4 {
		t.Fatalf("breakpoint hit count changed after optimization: %d", h)
	}
}

// TestDebugRoundTrip: Print -> Parse 必须保留 OrigPC 映射。
func TestDebugRoundTrip(t *testing.T) {
	p, err := parser.Parse(strings.NewReader(debugSrc))
	if err != nil {
		t.Fatal(err)
	}
	op, _, err := opt.Optimize(p, opt.Config{ConstantFold: true, RedundantMerge: true})
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := parser.Print(&b, op); err != nil {
		t.Fatal(err)
	}
	back, err := parser.Parse(&b)
	if err != nil {
		t.Fatalf("reparse optimized bytecode: %v\n%s", err, b.String())
	}

	for _, fn := range []string{"main"} {
		a := op.Func(fn)
		c := back.Func(fn)
		if len(a.Code) != len(c.Code) {
			t.Fatalf("%s: instruction count changed in round trip %d vs %d",
				fn, len(a.Code), len(c.Code))
		}
		for i := range a.Code {
			if a.Code[i].OrigPC != c.Code[i].OrigPC ||
				a.Code[i].Line != c.Code[i].Line {
				t.Fatalf("%s pc=%d debug mapping lost: (%d,%d) vs (%d,%d)",
					fn, i, a.Code[i].OrigPC, a.Code[i].Line,
					c.Code[i].OrigPC, c.Code[i].Line)
			}
		}
	}
}
