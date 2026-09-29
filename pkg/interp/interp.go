package interp

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/guoyyit/bytecode-optimizer-go/pkg/ir"
)

// Options controls interpreter execution.
type Options struct {
	Trace io.Writer
}

// RuntimeError describes an exceptional execution with its original source line.
type RuntimeError struct {
	SrcLine int
	Message string
}

func (e *RuntimeError) Error() string {
	return fmt.Sprintf("runtime error at source line %d: %s", e.SrcLine, e.Message)
}

type frame struct {
	f      *ir.Func
	pc     int
	stack  []int64
	locals map[string]int64
}

// Run executes entry with args on the reference interpreter, returning the
// entry's result, all PRINT output and a possible runtime error.
func Run(p *ir.Program, entry string, args ...int64) (int64, string, error) {
	return RunWith(p, entry, args, Options{})
}

// RunWith is Run with interpreter options.
func RunWith(p *ir.Program, entry string, args []int64, opts Options) (int64, string, error) {
	fn := p.Func(entry)
	if fn == nil {
		return 0, "", fmt.Errorf("runtime error: unknown entry function %q", entry)
	}
	if len(args) != len(fn.Params) {
		return 0, "", fmt.Errorf("runtime error: %s expects %d args, got %d", entry, len(fn.Params), len(args))
	}

	var out strings.Builder
	callStack := []*frame{newFrame(fn, args)}

	push := func(v int64) { fr := callStack[len(callStack)-1]; fr.stack = append(fr.stack, v) }
	pop := func() (int64, bool) {
		fr := callStack[len(callStack)-1]
		if len(fr.stack) == 0 {
			return 0, false
		}
		v := fr.stack[len(fr.stack)-1]
		fr.stack = fr.stack[:len(fr.stack)-1]
		return v, true
	}

	for len(callStack) > 0 {
		fr := callStack[len(callStack)-1]
		if fr.pc >= len(fr.f.Code) {
			// Falling off the end is equivalent to RET with an empty stack.
			if len(callStack) == 1 {
				return 0, out.String(), nil
			}
			callStack = callStack[:len(callStack)-1]
			push(0)
			continue
		}
		ins := fr.f.Code[fr.pc]

		if opts.Trace != nil {
			fmt.Fprintf(opts.Trace, "%s pc=%d line=%d %s\n", fr.f.Name, fr.pc, ins.SrcLine, display(p, ins))
		}

		next := fr.pc + 1

		switch ins.Op {
		case ir.OpLabel:
			// no-op
		case ir.OpPush:
			push(ins.Int)
		case ir.OpLoad:
			push(fr.locals[ins.Text])
		case ir.OpStore:
			v, ok := pop()
			if !ok {
				return 0, out.String(), rtErr(ins.SrcLine, "STORE on empty stack")
			}
			fr.locals[ins.Text] = v
		case ir.OpPop:
			if _, ok := pop(); !ok {
				return 0, out.String(), rtErr(ins.SrcLine, "POP on empty stack")
			}
		case ir.OpAdd, ir.OpSub, ir.OpMul, ir.OpDiv, ir.OpMod, ir.OpEq, ir.OpLt, ir.OpGt:
			b, ok1 := pop()
			a, ok2 := pop()
			if !ok1 || !ok2 {
				return 0, out.String(), rtErr(ins.SrcLine, "stack underflow")
			}
			v, ok := ir.EvalBinary(ins.Op, a, b)
			if !ok {
				return 0, out.String(), rtErr(ins.SrcLine, "integer divide by zero")
			}
			push(v)
		case ir.OpNeg:
			v, ok := pop()
			if !ok {
				return 0, out.String(), rtErr(ins.SrcLine, "NEG on empty stack")
			}
			push(-v)
		case ir.OpPrint:
			v, ok := pop()
			if !ok {
				return 0, out.String(), rtErr(ins.SrcLine, "PRINT on empty stack")
			}
			fmt.Fprintln(&out, v)
		case ir.OpJmp:
			target, err := labelPC(fr.f, ins.Text, ins.SrcLine)
			if err != nil {
				return 0, out.String(), err
			}
			next = target
		case ir.OpJz, ir.OpJnz:
			v, ok := pop()
			if !ok {
				return 0, out.String(), rtErr(ins.SrcLine, "conditional jump on empty stack")
			}
			take := (v == 0) == (ins.Op == ir.OpJz)
			if take {
				target, err := labelPC(fr.f, ins.Text, ins.SrcLine)
				if err != nil {
					return 0, out.String(), err
				}
				next = target
			}
		case ir.OpCall:
			callee := p.Func(ins.Text)
			if callee == nil {
				return 0, out.String(), rtErr(ins.SrcLine, "call to undefined function "+ins.Text)
			}
			if int64(len(callee.Params)) != ins.Int {
				return 0, out.String(), rtErr(ins.SrcLine, fmt.Sprintf(
					"call to %s expects %d args, got %d", ins.Text, len(callee.Params), ins.Int))
			}
			cargs := make([]int64, ins.Int)
			for i := ins.Int - 1; i >= 0; i-- {
				v, ok := pop()
				if !ok {
					return 0, out.String(), rtErr(ins.SrcLine, "call stack underflow")
				}
				cargs[i] = v
			}
			callStack = append(callStack, newFrame(callee, cargs))
			fr.pc++
			continue
		case ir.OpRet:
			v, has := pop()
			if !has {
				v = 0
			}
			callStack = callStack[:len(callStack)-1]
			if len(callStack) == 0 {
				return v, out.String(), nil
			}
			push(v)
		default:
			return 0, out.String(), rtErr(ins.SrcLine, "unknown opcode")
		}
		fr.pc = next
	}
	return 0, out.String(), nil
}

func newFrame(fn *ir.Func, args []int64) *frame {
	fr := &frame{f: fn, locals: map[string]int64{}}
	for i, name := range fn.Params {
		fr.locals[name] = args[i]
	}
	return fr
}

func labelPC(f *ir.Func, name string, srcLine int) (int, error) {
	for i, ins := range f.Code {
		if ins.Op == ir.OpLabel && ins.Text == name {
			return i, nil
		}
	}
	return 0, rtErr(srcLine, "jump to undefined label "+name)
}

func rtErr(line int, msg string) error {
	return &RuntimeError{SrcLine: line, Message: msg}
}

func display(p *ir.Program, ins ir.Instr) string {
	s := ins.Op.String()
	if ins.Op == ir.OpPush {
		s += " " + strconv.FormatInt(ins.Int, 10)
	} else if ins.Op.HasTextArg() {
		s += " " + ins.Text
		if ins.Op == ir.OpCall {
			s += " " + strconv.FormatInt(ins.Int, 10)
		}
	}
	return s
}
