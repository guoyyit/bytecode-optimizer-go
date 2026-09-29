package ir

// Op identifies a single bytecode opcode.
type Op int

const (
	OpInvalid Op = iota
	OpLabel      // pseudo-op marking a jump target, Text holds the label name
	OpPush       // Int holds the constant
	OpLoad       // Text holds the variable name
	OpStore      // Text holds the variable name
	OpPop
	OpAdd
	OpSub
	OpMul
	OpDiv
	OpMod
	OpNeg
	OpEq
	OpLt
	OpGt
	OpJmp  // Text holds target label
	OpJz   // pop; jump when zero; Text holds target label
	OpJnz  // pop; jump when non-zero; Text holds target label
	OpCall // Text holds function name, Int holds argument count
	OpRet  // pop top of stack as return value (0 when stack empty)
	OpPrint
)

// Instr is one instruction or label marker. SrcLine is the 1-based line in
// the original source used for debug info and runtime error mapping.
type Instr struct {
	Op      Op
	Int     int64
	Text    string
	SrcLine int
}

// Func is one independently compiled function body.
type Func struct {
	Name   string
	Params []string
	Code   []Instr
}

// Program is an ordered collection of functions plus the raw source lines
// referenced by SrcLine for debugger display.
type Program struct {
	Funcs []*Func
	Lines []string
}

func (op Op) String() string {
	switch op {
	case OpLabel:
		return "LABEL"
	case OpPush:
		return "PUSH"
	case OpLoad:
		return "LOAD"
	case OpStore:
		return "STORE"
	case OpPop:
		return "POP"
	case OpAdd:
		return "ADD"
	case OpSub:
		return "SUB"
	case OpMul:
		return "MUL"
	case OpDiv:
		return "DIV"
	case OpMod:
		return "MOD"
	case OpNeg:
		return "NEG"
	case OpEq:
		return "EQ"
	case OpLt:
		return "LT"
	case OpGt:
		return "GT"
	case OpJmp:
		return "JMP"
	case OpJz:
		return "JZ"
	case OpJnz:
		return "JNZ"
	case OpCall:
		return "CALL"
	case OpRet:
		return "RET"
	case OpPrint:
		return "PRINT"
	default:
		return "INVALID"
	}
}

// StackDelta is the net operand-stack effect of the op.
func (op Op) StackDelta(argc int64) int {
	switch op {
	case OpPush, OpLoad:
		return 1
	case OpStore, OpPop, OpJz, OpJnz, OpRet, OpPrint:
		return -1
	case OpAdd, OpSub, OpMul, OpDiv, OpMod:
		return -1
	case OpCall:
		return 1 - int(argc)
	default:
		return 0
	}
}

// Cost is the weighted execution cost used by optimization statistics.
func (op Op) Cost() int64 {
	switch op {
	case OpDiv, OpMod:
		return 8
	case OpPrint:
		return 10
	case OpMul:
		return 3
	case OpAdd, OpSub, OpEq, OpLt, OpGt:
		return 2
	case OpJz, OpJnz, OpRet:
		return 2
	case OpCall:
		return 5
	default:
		return 1
	}
}

// EvalBinary evaluates a binary op with exactly the interpreter semantics
// (wrapping int64 arithmetic). ok is false for division by zero.
func EvalBinary(op Op, a, b int64) (v int64, ok bool) {
	switch op {
	case OpAdd:
		return a + b, true
	case OpSub:
		return a - b, true
	case OpMul:
		return a * b, true
	case OpDiv:
		if b == 0 {
			return 0, false
		}
		return a / b, true
	case OpMod:
		if b == 0 {
			return 0, false
		}
		return a % b, true
	case OpEq:
		return b2i(a == b), true
	case OpLt:
		return b2i(a < b), true
	case OpGt:
		return b2i(a > b), true
	default:
		return 0, false
	}
}

func b2i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// Func looks up a function by name.
func (p *Program) Func(name string) *Func {
	for _, f := range p.Funcs {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// Clone returns a deep copy so failed optimizations can be discarded.
func (p *Program) Clone() *Program {
	q := &Program{
		Funcs: make([]*Func, len(p.Funcs)),
		Lines: append([]string(nil), p.Lines...),
	}
	for i, f := range p.Funcs {
		cp := &Func{
			Name:   f.Name,
			Params: append([]string(nil), f.Params...),
			Code:   append([]Instr(nil), f.Code...),
		}
		q.Funcs[i] = cp
	}
	return q
}
