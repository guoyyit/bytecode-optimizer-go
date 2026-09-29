package parse

import (
	"strings"
	"testing"

	"github.com/guoyyit/bytecode-optimizer-go/pkg/ir"
	"github.com/guoyyit/bytecode-optimizer-go/pkg/opt"
)

func TestParseRoundTrip(t *testing.T) {
	src := `func add(a, b)
  load a
  load b
  add
  ret
end

func main
  push 3
  push 4
  call add 2
  print
  ret
end
`
	p, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(p.Funcs) != 2 {
		t.Fatalf("want 2 funcs, got %d", len(p.Funcs))
	}
	if got := p.Func("add"); got == nil || len(got.Params) != 2 {
		t.Fatalf("function add not parsed correctly: %+v", got)
	}
	if err := opt.Validate(p); err != nil {
		t.Fatalf("validate: %v", err)
	}
	p2, err := Parse(p.Format())
	if err != nil {
		t.Fatalf("reparse formatted: %v", err)
	}
	if p2.Format() != p.Format() {
		t.Fatalf("format not stable:\n%s\n---\n%s", p.Format(), p2.Format())
	}
}

func TestParseErrors(t *testing.T) {
	bad := []string{
		"",
		"funcmain\nend\n",
		"func main\nbogus\nend\n",
		"func main\npush abc\nend\n",
		"func main\njmp nowhere\nend\n",
		"func main\nl:\nl:\nend\n",
		"func main\nend\nfunc main\nend\n",
		"func main()\nend\n",
		"func f(1x)\nend\n",
		"func main\ncall ghost 0\nend\n",
		"func main\n",
	}
	for i, src := range bad {
		p, err := Parse(src)
		if err == nil {
			if verr := opt.Validate(p); verr == nil {
				t.Fatalf("case %d: expected parse or validate error for %q", i, src)
			}
		}
		if err != nil && !strings.Contains(err.Error(), "line") && !strings.Contains(err.Error(), "no functions") &&
			!strings.Contains(err.Error(), "missing end") {
			t.Fatalf("case %d: error should mention line, got %v", i, err)
		}
	}
}

func TestParsePreservesSourceLines(t *testing.T) {
	src := "func main\n  push 1\nloop:\n  jmp loop\nend\n"
	p, _ := Parse(src)
	f := p.Func("main")
	want := []struct {
		op      ir.Op
		srcLine int
		text    string
	}{
		{ir.OpPush, 2, ""},
		{ir.OpLabel, 3, "loop"},
		{ir.OpJmp, 4, "loop"},
	}
	if len(f.Code) != len(want) {
		t.Fatalf("got %d instrs", len(f.Code))
	}
	for i, w := range want {
		got := f.Code[i]
		if got.Op != w.op || got.SrcLine != w.srcLine || got.Text != w.text {
			t.Fatalf("instr %d = %+v, want op=%v line=%d text=%q", i, got, w.op, w.srcLine, w.text)
		}
	}
}
