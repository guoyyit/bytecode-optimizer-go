package parser

import (
	"fmt"
	"io"
	"strings"

	"github.com/guoyyit/bytecode-optimizer-go/pkg/bytecode"
)

// Print 以 Parse 可读回的文本格式输出程序（优化结果可继续执行/调试）。
func Print(w io.Writer, p *bytecode.Program) error {
	var b strings.Builder
	for _, f := range p.Funcs {
		fmt.Fprintf(&b, "func %s %d %d\n", f.Name, f.Params, f.Locals)
		for pc, in := range f.Code {
			b.WriteString("  ")
			b.WriteString(formatInstr(in))
			if in.Line > 0 || in.OrigPC != pc {
				b.WriteString(" #")
				if in.Line > 0 {
					fmt.Fprintf(&b, " line=%d", in.Line)
				}
				if in.OrigPC != pc {
					fmt.Fprintf(&b, " pc=%d", in.OrigPC)
				}
			}
			b.WriteByte('\n')
		}
		b.WriteString("end\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func formatInstr(in *bytecode.Instr) string {
	switch in.Op {
	case bytecode.OpPush:
		return fmt.Sprintf("push %d", in.Arg)
	case bytecode.OpLoad, bytecode.OpStore:
		if in.Name != "" {
			return fmt.Sprintf("%s %d %s", in.Op, in.Arg, in.Name)
		}
		return fmt.Sprintf("%s %d", in.Op, in.Arg)
	case bytecode.OpLabel:
		return fmt.Sprintf("label %s", in.Target)
	case bytecode.OpJmp, bytecode.OpJz, bytecode.OpJnz:
		return fmt.Sprintf("%s %s", in.Op, in.Target)
	case bytecode.OpCall:
		return fmt.Sprintf("call %s %d", in.Name, in.Arg)
	default:
		return in.Op.String()
	}
}
