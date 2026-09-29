package ir

import "strings"

// Format renders the program back into the canonical textual bytecode format.
func (p *Program) Format() string {
	var b strings.Builder
	for fi, f := range p.Funcs {
		if fi > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("func ")
		b.WriteString(f.Name)
		if len(f.Params) > 0 {
			b.WriteByte('(')
			for i, v := range f.Params {
				if i > 0 {
					b.WriteString(", ")
				}
				b.WriteString(v)
			}
			b.WriteByte(')')
		}
		b.WriteByte('\n')
		for _, ins := range f.Code {
			if ins.Op == OpLabel {
				b.WriteString(ins.Text)
				b.WriteString(":\n")
				continue
			}
			b.WriteString("    ")
			b.WriteString(ins.Op.String())
			if ins.Op.HasTextArg() {
				b.WriteByte(' ')
				b.WriteString(ins.Text)
			}
			if ins.Op == OpPush || ins.Op == OpCall {
				b.WriteByte(' ')
				b.WriteString(itoa(ins.Int))
			}
			b.WriteByte('\n')
		}
		b.WriteString("end\n")
	}
	return b.String()
}

// HasTextArg reports whether the op carries its Text field (label or name).
func (op Op) HasTextArg() bool {
	switch op {
	case OpLoad, OpStore, OpJmp, OpJz, OpJnz, OpCall:
		return true
	default:
		return false
	}
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var digits [20]byte
	i := len(digits)
	for v > 0 {
		i--
		digits[i] = byte('0' + v%10)
		v /= 10
	}
	s := string(digits[i:])
	if neg {
		s = "-" + s
	}
	return s
}
