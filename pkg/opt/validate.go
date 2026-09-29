package opt

import (
	"fmt"

	"github.com/guoyyit/bytecode-optimizer-go/pkg/ir"
)

// Validate checks well-formedness: known ops/functions, defined labels,
// consistent stack depths at join points and valid call arities. It is used
// both before and after optimization, guaranteeing that emitted bytecode has
// the same stack discipline as the input.
func Validate(p *ir.Program) error {
	if p == nil {
		return fmt.Errorf("invalid program: nil")
	}
	if len(p.Funcs) == 0 {
		return fmt.Errorf("invalid program: no functions")
	}
	seen := map[string]bool{}
	for _, f := range p.Funcs {
		if f == nil || f.Name == "" {
			return fmt.Errorf("invalid program: anonymous function")
		}
		if seen[f.Name] {
			return fmt.Errorf("invalid program: duplicate function %q", f.Name)
		}
		seen[f.Name] = true
		if dup := duplicateParam(f.Params); dup != "" {
			return fmt.Errorf("invalid program: function %q has duplicate parameter %q", f.Name, dup)
		}
		if err := validateFunc(p, f); err != nil {
			return err
		}
	}
	return nil
}

func duplicateParam(params []string) string {
	seen := map[string]bool{}
	for _, v := range params {
		if seen[v] {
			return v
		}
		seen[v] = true
	}
	return ""
}

func validateFunc(p *ir.Program, f *ir.Func) error {
	n := len(f.Code)
	if n == 0 {
		return fmt.Errorf("invalid bytecode in %s: empty function body", f.Name)
	}
	labels := map[string]int{}
	for i, ins := range f.Code {
		switch ins.Op {
		case ir.OpInvalid:
			return fmt.Errorf("invalid bytecode in %s at line %d: unknown opcode", f.Name, ins.SrcLine)
		case ir.OpLabel:
			if ins.Text == "" {
				return fmt.Errorf("invalid bytecode in %s at line %d: empty label", f.Name, ins.SrcLine)
			}
			if _, dup := labels[ins.Text]; dup {
				return fmt.Errorf("invalid bytecode in %s: duplicate label %q", f.Name, ins.Text)
			}
			labels[ins.Text] = i
		case ir.OpCall:
			callee := p.Func(ins.Text)
			if callee == nil {
				return fmt.Errorf("invalid bytecode in %s at line %d: call to undefined function %q", f.Name, ins.SrcLine, ins.Text)
			}
			if int64(len(callee.Params)) != ins.Int {
				return fmt.Errorf("invalid bytecode in %s at line %d: call to %s expects %d args, got %d",
					f.Name, ins.SrcLine, ins.Text, len(callee.Params), ins.Int)
			}
		}
	}

	for _, ins := range f.Code {
		if ins.Op == ir.OpJmp || ins.Op == ir.OpJz || ins.Op == ir.OpJnz {
			if _, ok := labels[ins.Text]; !ok {
				return fmt.Errorf("invalid bytecode in %s at line %d: jump to undefined label %q", f.Name, ins.SrcLine, ins.Text)
			}
		}
	}

	// Stack-depth analysis over the control-flow graph. Every reachable PC
	// must observe a single consistent operand-stack depth and the depth may
	// never become negative.
	depthAt := make([]int, n)
	known := make([]bool, n)
	known[0] = true
	type edge struct{ from, to int }
	stack := []edge{{-1, 0}}
	for len(stack) > 0 {
		e := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		d := 0
		if e.from >= 0 {
			d = depthAt[e.from]
		}
		ins := f.Code[e.to]
		if known[e.to] {
			if depthAt[e.to] != d {
				return fmt.Errorf("invalid bytecode in %s: inconsistent stack depth %d/%d at line %d",
					f.Name, depthAt[e.to], d, ins.SrcLine)
			}
			continue
		}
		known[e.to] = true
		depthAt[e.to] = d

		if ins.Op == ir.OpLabel {
			if e.to+1 < n {
				stack = append(stack, edge{e.to, e.to + 1})
			}
			continue
		}

		delta := ins.Op.StackDelta(ins.Int)
		if ins.Op == ir.OpRet {
			delta = 0
		}
		nd := d + delta
		if nd < 0 {
			return fmt.Errorf("invalid bytecode in %s at line %d: operand stack underflow", f.Name, ins.SrcLine)
		}

		branch := -1
		fall := true
		switch ins.Op {
		case ir.OpJmp:
			branch = labels[ins.Text]
			fall = false
		case ir.OpJz, ir.OpJnz:
			branch = labels[ins.Text]
		case ir.OpRet:
			fall = false
		}
		if fall && e.to+1 < n {
			depthAt[e.to] = nd
			stack = append(stack, edge{e.to, e.to + 1})
		}
		if branch >= 0 {
			depthAt[e.to] = nd
			stack = append(stack, edge{e.to, branch})
		}
	}
	return nil
}
