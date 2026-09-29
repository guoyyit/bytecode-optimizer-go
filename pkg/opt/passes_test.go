package opt

import (
	"strings"
	"testing"

	"github.com/guoyyit/bytecode-optimizer-go/pkg/interp"
	"github.com/guoyyit/bytecode-optimizer-go/pkg/ir"
)

func countOps(p *ir.Program, op ir.Op) int {
	n := 0
	for _, f := range p.Funcs {
		for _, ins := range f.Code {
			if ins.Op == op {
				n++
			}
		}
	}
	return n
}

func TestJumpChainRelocationAfterDeletion(t *testing.T) {
	// a -> b -> c -> ret; after const-fold + dce delete instructions before
	// labels, the JMPs must all resolve to the surviving target and behavior
	// must be unchanged (jumps stay label-based, no numeric patching).
	src := `func main
  push 2
  push 3
  add
  print
  jmp a
  push 11
  print
a:
  push 0
  jz b
  push 22
  print
b:
  jmp c
  push 33
  print
c:
  push 44
  ret
end
`
	p := mustProg(t, src)
	op, rep := Optimize(p, DefaultConfig())
	if rep.Failed {
		t.Fatal(rep.Reason)
	}
	v1, o1, e1 := interp.Run(p, "main")
	v2, o2, e2 := interp.Run(op, "main")
	if !sameResult(v1, o1, e1, v2, o2, e2) {
		t.Fatalf("before v=%d out=%q err=%v\nafter v=%d out=%q err=%v", v1, o1, e1, v2, o2, e2)
	}
	if strings.Contains(o2, "11") || strings.Contains(o2, "33") {
		t.Fatalf("dead prints must be unreachable, got %q", o2)
	}
	// No dangling jumps: post-optimization validation already ran inside
	// Optimize; assert labels referenced by jumps still exist explicitly.
	for _, f := range op.Funcs {
		labels := map[string]bool{}
		for _, ins := range f.Code {
			if ins.Op == ir.OpLabel {
				labels[ins.Text] = true
			}
		}
		for _, ins := range f.Code {
			if ins.Op == ir.OpJmp || ins.Op == ir.OpJz || ins.Op == ir.OpJnz {
				if !labels[ins.Text] {
					t.Fatalf("jump %s has no target after relocation", ins.Text)
				}
			}
		}
	}
}

func TestConstantFoldUnit(t *testing.T) {
	src := `func main
  push 1
  push 2
  eq
  push 6
  push 7
  mul
  print
  print
  ret
end
`
	p := mustProg(t, src)
	op, rep := Optimize(p, cfgOnly(PassConstantFold))
	if rep.Failed {
		t.Fatal(rep.Reason)
	}
	if countOps(op, ir.OpMul) != 0 || countOps(op, ir.OpEq) != 0 {
		t.Fatal("constant operators should be folded away")
	}
	_, out, err := interp.Run(op, "main")
	if err != nil || out != "42\n0\n" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestDCEUnit(t *testing.T) {
	src := `func main
  push 1
  ret
  push 2
  print
end
`
	p := mustProg(t, src)
	op, rep := Optimize(p, cfgOnly(PassDCE))
	if rep.Failed {
		t.Fatal(rep.Reason)
	}
	if rep.TotalRemoved != 2 {
		t.Fatalf("removed = %d, want 2 (PUSH, PRINT)", rep.TotalRemoved)
	}
	v, _, err := interp.Run(op, "main")
	if err != nil || v != 1 {
		t.Fatalf("v=%d err=%v", v, err)
	}
}

func TestJumpPassRemovesRedundantJmp(t *testing.T) {
	src := `func main
  push 1
  jmp next
next:
  ret
end
`
	p := mustProg(t, src)
	op, rep := Optimize(p, cfgOnly(PassJump))
	if rep.Failed {
		t.Fatal(rep.Reason)
	}
	if countOps(op, ir.OpJmp) != 0 {
		t.Fatalf("redundant JMP not removed:\n%s", op.Format())
	}
	v, _, err := interp.Run(op, "main")
	if err != nil || v != 1 {
		t.Fatalf("v=%d err=%v", v, err)
	}
}

func TestPeepholeUnit(t *testing.T) {
	src := `func main
  push 9
  pop
  push 8
  store x
  load x
  store x
  push 7
  neg
  neg
  print
  ret
end
	`
	p := mustProg(t, src)
	op, rep := Optimize(p, cfgOnly(PassPeephole))
	if rep.Failed {
		t.Fatal(rep.Reason)
	}
	_, out, err := interp.Run(op, "main")
	if err != nil || out != "7\n" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if rep.TotalRemoved != 6 {
		t.Fatalf("removed = %d, expected 6 (push/pop=2, load/store=2, neg/neg=2)", rep.TotalRemoved)
	}
}

func TestPassesDoNotCrossLabels(t *testing.T) {
	// enter jumps into the middle with a preset stack: PUSH 5; JZ mid is not
	// taken (5 != 0); then JMP end; mid expects one value and returns it.
	src := `func main
  push 5
  jz mid
  push 7
  jmp end
mid:
  push 3
end:
  ret
end
`
	p := mustProg(t, src)
	for _, name := range AllPasses {
		op, rep := Optimize(p, cfgOnly(name))
		if rep.Failed {
			t.Fatalf("%s: %s", name, rep.Reason)
		}
		v1, _, e1 := interp.Run(p, "main")
		v2, _, e2 := interp.Run(op, "main")
		if v1 != v2 || (e1 == nil) != (e2 == nil) {
			t.Fatalf("pass %s changed behavior: %d/%v vs %d/%v", name, v1, e1, v2, e2)
		}
	}
}

func TestOptimalProgramRepeatedIsStableSingleIteration(t *testing.T) {
	// Already minimal: no constants adjacent to operators, no jumps, no
	// peephole pairs, no unreachable code.
	src := `func id(x)
  load x
  ret
end

func main
  push 41
  call id 1
  print
  ret
end
`
	p := mustProg(t, src)
	op, rep := Optimize(p, DefaultConfig())
	if rep.Failed || rep.Iterations != 1 || rep.TotalRemoved != 0 {
		t.Fatalf("iters=%d removed=%d failed=%v", rep.Iterations, rep.TotalRemoved, rep.Failed)
	}
	if op.Format() != p.Format() {
		t.Fatalf("minimal program changed:\n%s", op.Format())
	}
}
