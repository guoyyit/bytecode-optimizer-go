package opt

import (
	"fmt"
	"strings"
	"testing"

	"github.com/guoyyit/bytecode-optimizer-go/pkg/interp"
	"github.com/guoyyit/bytecode-optimizer-go/pkg/ir"
)

// lineTrace runs the program collecting one "func:op@line" event per executed
// instruction (labels excluded), mirroring what a single-stepping debugger
// would show.
type traceWriter struct{ b strings.Builder }

func (w *traceWriter) Write(p []byte) (int, error) { w.b.Write(p); return len(p), nil }

func executedLines(p *ir.Program) ([]string, string, error) {
	w := &traceWriter{}
	_, out, err := interp.RunWith(p, "main", nil, interp.Options{Trace: w})
	var events []string
	for _, line := range strings.Split(strings.TrimRight(w.b.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		// format: main pc=N line=L OP...
		parts := strings.SplitN(line, " ", 4)
		if len(parts) < 4 {
			continue
		}
		op := parts[3]
		if op == "LABEL" {
			continue
		}
		var srcLine int
		fmt.Sscanf(parts[2], "line=%d", &srcLine)
		events = append(events, op+"@"+fmt.Sprint(srcLine))
	}
	return events, out, err
}

func TestDebugLinesRemainMappedToOriginal(t *testing.T) {
	for i, src := range corpus {
		p := mustProg(t, src)
		validLines := map[int]bool{}
		for _, f := range p.Funcs {
			for _, ins := range f.Code {
				validLines[ins.SrcLine] = true
			}
		}
		op, rep := Optimize(p, DefaultConfig())
		if rep.Failed {
			t.Fatalf("program %d: %s", i, rep.Reason)
		}
		for _, f := range op.Funcs {
			for _, ins := range f.Code {
				if !validLines[ins.SrcLine] {
					t.Fatalf("program %d: instruction %s maps to unknown source line %d",
						i, ins.Op, ins.SrcLine)
				}
			}
		}
		// Local variable names must survive unchanged.
		for j, of := range op.Funcs {
			pf := p.Funcs[j]
			if strings.Join(of.Params, ",") != strings.Join(pf.Params, ",") {
				t.Fatalf("program %d: parameter names changed", i)
			}
			for _, ins := range of.Code {
				if ins.Op == ir.OpLoad || ins.Op == ir.OpStore {
					if !containsName(pf, ins.Text) {
						t.Fatalf("program %d: unknown variable %q after optimization", i, ins.Text)
					}
				}
			}
		}
	}
}

func containsName(f *ir.Func, name string) bool {
	for _, v := range f.Params {
		if v == name {
			return true
		}
	}
	for _, ins := range f.Code {
		if (ins.Op == ir.OpLoad || ins.Op == ir.OpStore) && ins.Text == name {
			return true
		}
	}
	return false
}

func TestStepTraceIsSubsequenceWithSameBreakpointLines(t *testing.T) {
	src := `func main
  push 2
  push 3
  push 4
  add
  mul
  print
  push 0
  jz skip
  push 1
  print
skip:
  push 7
  ret
end
`
	p := mustProg(t, src)
	before, out1, err1 := executedLines(p)
	op, rep := Optimize(p, DefaultConfig())
	if rep.Failed {
		t.Fatal(rep.Reason)
	}
	after, out2, err2 := executedLines(op)
	if out1 != out2 || fmt.Sprint(err1) != fmt.Sprint(err2) {
		t.Fatalf("behavior differs: %q/%v vs %q/%v", out1, err1, out2, err2)
	}
	// The breakpoint lines actually reached must be the same set in order.
	var bLines, aLines []string
	for _, e := range before {
		bLines = append(bLines, eventLine(e))
	}
	for _, e := range after {
		aLines = append(aLines, eventLine(e))
	}
	if !isSubsequence(aLines, bLines) {
		t.Fatalf("optimized source-line trace must be a subsequence:\nbefore=%v\nafter =%v",
			bLines, aLines)
	}
	// Every optimized breakpoint position must also have been hit in the
	// original run, so no new location is introduced.
	hit := map[string]bool{}
	for _, l := range bLines {
		hit[l] = true
	}
	for _, l := range aLines {
		if !hit[l] {
			t.Fatalf("optimized program breaks at line %s, never a breakpoint before", l)
		}
	}
	if len(aLines) >= len(bLines) {
		t.Fatalf("expected strictly fewer steps, before=%d after=%d", len(bLines), len(aLines))
	}
}

func isSubsequence(short, long []string) bool {
	i := 0
	for _, e := range long {
		if i < len(short) && short[i] == e {
			i++
		}
	}
	return i == len(short)
}

func eventLine(e string) string {
	if i := strings.Index(e, "@"); i >= 0 {
		return e[i+1:]
	}
	return e
}
