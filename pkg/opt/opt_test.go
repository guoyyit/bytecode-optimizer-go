package opt

import (
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
