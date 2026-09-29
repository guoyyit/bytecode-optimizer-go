package parser_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/guoyyit/bytecode-optimizer-go/pkg/bytecode"
	"github.com/guoyyit/bytecode-optimizer-go/pkg/parser"
)

func TestParseAndRoundTrip(t *testing.T) {
	src := `func main 0 2
  push 10
  store 0 x # line=5
  load 0 x
  print # line=6
  halt
end
`
	p, err := parser.Parse(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	main := p.Func("main")
	if main == nil || main.Locals != 2 {
		t.Fatalf("bad function header: %+v", main)
	}
	if main.Code[3].Line != 6 {
		t.Fatalf("line debug info not parsed: %d", main.Code[3].Line)
	}

	var b bytes.Buffer
	if err := parser.Print(&b, p); err != nil {
		t.Fatal(err)
	}
	p2, err := parser.Parse(&b)
	if err != nil {
		t.Fatalf("roundtrip parse: %v\n%s", err, b.String())
	}
	for i := range main.Code {
		a, c := main.Code[i], p2.Func("main").Code[i]
		if a.Op != c.Op || a.Arg != c.Arg || a.Target != c.Target ||
			a.Name != c.Name || a.Line != c.Line || a.OrigPC != c.OrigPC {
			t.Fatalf("instr %d changed across round trip", i)
		}
	}
}

func TestOpcodeCoverage(t *testing.T) {
	// 每个非伪指令都必须有可解析的助记符且非 INVALID。
	for op := bytecode.OpPush; op <= bytecode.OpHalt; op++ {
		if op.String() == "" || op.String() == "INVALID" {
			t.Fatalf("opcode %d missing mnemonic", op)
		}
	}
}
