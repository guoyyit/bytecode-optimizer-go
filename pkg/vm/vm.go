package vm

import (
	"fmt"
	"io"

	"github.com/guoyyit/bytecode-optimizer-go/pkg/bytecode"
)

// Error 携带运行时错误及其在字节码中的位置。
type Error struct {
	FuncName string
	PC       int
	Op       bytecode.Opcode
	Reason   string
}

func (e *Error) Error() string {
	return fmt.Sprintf("runtime error in %s at pc=%d (%s): %s",
		e.FuncName, e.PC, e.Op, e.Reason)
}

// StepEvent 是单步调试回调看到的事件，位置基于优化前 PC。
type StepEvent struct {
	FuncName string
	OrigPC   int
	Line     int
	Op       bytecode.Opcode
	Break    bool
}

var errHalt = &haltErr{}

type haltErr struct{}

func (haltErr) Error() string { return "halt" }

// VM 是参考解释器，优化前后程序在其上的可观测行为必须一致。
type VM struct {
	prog *bytecode.Program
	out  io.Writer

	breakpoints map[string]map[int]bool
	stepFn      func(StepEvent)
	maxDepth    int
}

// New 构造参考解释器。
func New(prog *bytecode.Program, out io.Writer) *VM {
	return &VM{
		prog:        prog,
		out:         out,
		breakpoints: make(map[string]map[int]bool),
		maxDepth:    256,
	}
}

// Run 从入口函数开始执行。
func (v *VM) Run() error {
	entry := v.prog.Funcs[0]
	_, err := v.call(entry, nil, 1)
	if err == errHalt {
		return nil
	}
	return err
}

// SetBreakpoint 在指定函数的优化前 PC 处设置断点。
func (v *VM) SetBreakpoint(funcName string, origPC int) {
	set := v.breakpoints[funcName]
	if set == nil {
		set = make(map[int]bool)
		v.breakpoints[funcName] = set
	}
	set[origPC] = true
}

// OnStep 注册单步回调（用于单步调试）。
func (v *VM) OnStep(fn func(StepEvent)) { v.stepFn = fn }

// ClearBreakpoint 移除一个断点。
func (v *VM) ClearBreakpoint(funcName string, origPC int) {
	delete(v.breakpoints[funcName], origPC)
}

type frame struct {
	fn     *bytecode.Function
	locals []int64
	stack  []int64
	pc     int
	labels map[string]int
}

func (f *frame) pop() int64 {
	n := len(f.stack)
	v := f.stack[n-1]
	f.stack = f.stack[:n-1]
	return v
}

func (f *frame) push(x int64) { f.stack = append(f.stack, x) }

func (v *VM) call(fn *bytecode.Function, args []int64, depth int) (int64, error) {
	if depth > v.maxDepth {
		return 0, &Error{FuncName: fn.Name, Reason: "call stack overflow"}
	}
	if len(args) != fn.Params {
		return 0, &Error{FuncName: fn.Name, Reason: fmt.Sprintf("arity mismatch: %d args to %s(params=%d)", len(args), fn.Name, fn.Params)}
	}
	fr := &frame{
		fn:     fn,
		locals: make([]int64, fn.Locals),
		labels: fn.LabelTargets(),
	}
	copy(fr.locals, args)

	for {
		if fr.pc < 0 || fr.pc >= len(fn.Code) {
			return 0, &Error{FuncName: fn.Name, PC: fr.pc, Reason: "pc out of range"}
		}
		in := fn.Code[fr.pc]

		if in.Op != bytecode.OpLabel && v.stepFn != nil {
			v.stepFn(StepEvent{
				FuncName: fn.Name,
				OrigPC:   in.OrigPC,
				Line:     in.Line,
				Op:       in.Op,
				Break:    v.breakpoints[fn.Name][in.OrigPC],
			})
		}

		next := fr.pc + 1
		switch in.Op {
		case bytecode.OpLabel:
			// 伪指令，顺序流过。
		case bytecode.OpPush:
			fr.push(in.Arg)
		case bytecode.OpPop:
			if len(fr.stack) < 1 {
				return 0, v.stackErr(fn, fr.pc, "pop underflow")
			}
			fr.pop()
		case bytecode.OpDup:
			if len(fr.stack) < 1 {
				return 0, v.stackErr(fn, fr.pc, "dup underflow")
			}
			fr.push(fr.stack[len(fr.stack)-1])
		case bytecode.OpLoad:
			fr.push(fr.locals[in.Arg])
		case bytecode.OpStore:
			if len(fr.stack) < 1 {
				return 0, v.stackErr(fn, fr.pc, "store underflow")
			}
			fr.locals[in.Arg] = fr.pop()
		case bytecode.OpAdd, bytecode.OpSub, bytecode.OpMul,
			bytecode.OpDiv, bytecode.OpMod:
			if len(fr.stack) < 2 {
				return 0, v.stackErr(fn, fr.pc, "binary op underflow")
			}
			b := fr.pop()
			a := fr.pop()
			r, e := applyBinop(in.Op, a, b)
			if e != nil {
				return 0, &Error{FuncName: fn.Name, PC: fr.pc, Op: in.Op, Reason: e.Error()}
			}
			fr.push(r)
		case bytecode.OpNeg:
			if len(fr.stack) < 1 {
				return 0, v.stackErr(fn, fr.pc, "neg underflow")
			}
			r, e := bytecode.NegInt(fr.pop())
			if e != nil {
				return 0, &Error{FuncName: fn.Name, PC: fr.pc, Op: in.Op, Reason: e.Error()}
			}
			fr.push(r)
		case bytecode.OpEq, bytecode.OpNe, bytecode.OpLt,
			bytecode.OpLe, bytecode.OpGt, bytecode.OpGe:
			if len(fr.stack) < 2 {
				return 0, v.stackErr(fn, fr.pc, "compare underflow")
			}
			b := fr.pop()
			a := fr.pop()
			fr.push(applyCmp(in.Op, a, b))
		case bytecode.OpJmp:
			target, ok := fr.labels[in.Target]
			if !ok {
				return 0, v.stackErr(fn, fr.pc, "jmp to missing label "+in.Target)
			}
			next = target
		case bytecode.OpJz, bytecode.OpJnz:
			if len(fr.stack) < 1 {
				return 0, v.stackErr(fn, fr.pc, "branch underflow")
			}
			cond := fr.pop()
			taken := (cond == 0)
			if in.Op == bytecode.OpJnz {
				taken = !taken
			}
			target, ok := fr.labels[in.Target]
			if !ok {
				return 0, v.stackErr(fn, fr.pc, "branch to missing label "+in.Target)
			}
			if taken {
				next = target
			}
		case bytecode.OpCall:
			callee := v.prog.Func(in.Name)
			if callee == nil {
				return 0, v.stackErr(fn, fr.pc, "call to missing function "+in.Name)
			}
			argc := int(in.Arg)
			if len(fr.stack) < argc {
				return 0, v.stackErr(fn, fr.pc, "call arguments underflow")
			}
			cargs := make([]int64, argc)
			for i := argc - 1; i >= 0; i-- {
				cargs[i] = fr.pop()
			}
			r, e := v.call(callee, cargs, depth+1)
			if e != nil {
				return 0, e
			}
			fr.push(r)
		case bytecode.OpRet:
			if len(fr.stack) < 1 {
				return 0, v.stackErr(fn, fr.pc, "ret underflow")
			}
			return fr.pop(), nil
		case bytecode.OpPrint:
			if len(fr.stack) < 1 {
				return 0, v.stackErr(fn, fr.pc, "print underflow")
			}
			fmt.Fprintf(v.out, "%d\n", fr.pop())
		case bytecode.OpHalt:
			return 0, errHalt
		default:
			return 0, &Error{FuncName: fn.Name, PC: fr.pc, Op: in.Op, Reason: "invalid opcode"}
		}
		fr.pc = next
	}
}

func (v *VM) stackErr(fn *bytecode.Function, pc int, reason string) error {
	op := bytecode.OpInvalid
	if pc >= 0 && pc < len(fn.Code) {
		op = fn.Code[pc].Op
	}
	return &Error{FuncName: fn.Name, PC: pc, Op: op, Reason: reason}
}

func applyBinop(op bytecode.Opcode, a, b int64) (int64, error) {
	switch op {
	case bytecode.OpAdd:
		return bytecode.AddInt(a, b)
	case bytecode.OpSub:
		return bytecode.SubInt(a, b)
	case bytecode.OpMul:
		return bytecode.MulInt(a, b)
	case bytecode.OpDiv:
		return bytecode.DivInt(a, b)
	case bytecode.OpMod:
		return bytecode.ModInt(a, b)
	default:
		return 0, fmt.Errorf("not a binary op: %s", op)
	}
}

func applyCmp(op bytecode.Opcode, a, b int64) int64 {
	var c bool
	switch op {
	case bytecode.OpEq:
		c = a == b
	case bytecode.OpNe:
		c = a != b
	case bytecode.OpLt:
		c = a < b
	case bytecode.OpLe:
		c = a <= b
	case bytecode.OpGt:
		c = a > b
	case bytecode.OpGe:
		c = a >= b
	}
	if c {
		return 1
	}
	return 0
}
