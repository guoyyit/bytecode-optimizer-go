package parse

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/guoyyit/bytecode-optimizer-go/pkg/ir"
)

// Parse reads textual bytecode into a Program.
func Parse(src string) (*ir.Program, error) {
	lines := strings.Split(src, "\n")
	p := &ir.Program{Lines: lines}

	var cur *ir.Func
	labels := map[string]bool{}
	funcs := map[string]bool{}

	for i, raw := range lines {
		lineNo := i + 1
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if cur == nil {
			fn, perr := parseFuncHeader(line)
			if perr != "" {
				return nil, fmt.Errorf("parse error at line %d: %s", lineNo, perr)
			}
			if funcs[fn.Name] {
				return nil, fmt.Errorf("parse error at line %d: duplicate function %q", lineNo, fn.Name)
			}
			funcs[fn.Name] = true
			cur = fn
			labels = map[string]bool{}
			p.Funcs = append(p.Funcs, cur)
			continue
		}

		if line == "end" {
			cur = nil
			continue
		}

		if strings.HasSuffix(line, ":") {
			name := strings.TrimSpace(strings.TrimSuffix(line, ":"))
			if !isIdent(name) {
				return nil, fmt.Errorf("parse error at line %d: invalid label %q", lineNo, name)
			}
			if labels[name] {
				return nil, fmt.Errorf("parse error at line %d: duplicate label %q", lineNo, name)
			}
			labels[name] = true
			cur.Code = append(cur.Code, ir.Instr{Op: ir.OpLabel, Text: name, SrcLine: lineNo})
			continue
		}

		ins, perr := parseInstr(line, lineNo)
		if perr != "" {
			return nil, fmt.Errorf("parse error at line %d: %s", lineNo, perr)
		}
		cur.Code = append(cur.Code, ins)
	}

	if cur != nil {
		return nil, fmt.Errorf("parse error: function %q missing end", cur.Name)
	}
	if len(p.Funcs) == 0 {
		return nil, fmt.Errorf("parse error: no functions defined")
	}
	return p, nil
}

func parseFuncHeader(line string) (*ir.Func, string) {
	tokens := tokenize(line)
	if len(tokens) == 0 || !strings.EqualFold(tokens[0], "func") {
		return nil, "expected 'func <name>(params)'"
	}
	if len(tokens) < 2 {
		return nil, "expected function name"
	}
	name := tokens[1]
	if !isIdent(name) {
		return nil, fmt.Sprintf("invalid function name %q", name)
	}
	f := &ir.Func{Name: name}
	if len(tokens) == 2 {
		return f, ""
	}
	rest := strings.TrimSpace(line[len(tokens[0]):])
	rest = strings.TrimSpace(rest[len(tokens[1]):])
	if !strings.HasPrefix(rest, "(") || !strings.HasSuffix(rest, ")") {
		return nil, "malformed parameter list"
	}
	params := strings.TrimSpace(rest[1 : len(rest)-1])
	if params != "" {
		for _, part := range strings.Split(params, ",") {
			v := strings.TrimSpace(part)
			if !isIdent(v) {
				return nil, fmt.Sprintf("invalid parameter name %q", v)
			}
			f.Params = append(f.Params, v)
		}
	}
	return f, ""
}

func parseInstr(line string, lineNo int) (ir.Instr, string) {
	tokens := tokenize(line)
	op, ok := opByName(tokens[0])
	if !ok {
		return ir.Instr{}, fmt.Sprintf("unknown opcode %q", tokens[0])
	}
	ins := ir.Instr{Op: op, SrcLine: lineNo}

	switch op {
	case ir.OpPush:
		if len(tokens) != 2 {
			return ins, "PUSH expects one integer operand"
		}
		v, err := strconv.ParseInt(tokens[1], 10, 64)
		if err != nil {
			return ins, fmt.Sprintf("invalid integer %q", tokens[1])
		}
		ins.Int = v
	case ir.OpLoad, ir.OpStore, ir.OpJmp, ir.OpJz, ir.OpJnz:
		if len(tokens) != 2 || !isIdent(tokens[1]) {
			return ins, op.String() + " expects one identifier operand"
		}
		ins.Text = tokens[1]
	case ir.OpCall:
		if len(tokens) != 3 || !isIdent(tokens[1]) {
			return ins, "CALL expects <function> <argc>"
		}
		argc, err := strconv.ParseInt(tokens[2], 10, 64)
		if err != nil || argc < 0 {
			return ins, "CALL argc must be a non-negative integer"
		}
		ins.Text = tokens[1]
		ins.Int = argc
	default:
		if len(tokens) != 1 {
			return ins, op.String() + " takes no operands"
		}
	}
	return ins, ""
}

func tokenize(line string) []string {
	return strings.FieldsFunc(line, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '(' || r == ')' || r == ','
	})
}

func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func opByName(s string) (ir.Op, bool) {
	for op := ir.OpPush; op <= ir.OpPrint; op++ {
		if strings.EqualFold(op.String(), s) {
			return op, true
		}
	}
	return ir.OpInvalid, false
}
