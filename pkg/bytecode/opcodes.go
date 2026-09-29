package bytecode

import (
	"math"
)

// Opcode 标识一条栈式字节码指令的操作码。
type Opcode int

const (
	OpInvalid Opcode = iota
	OpLabel          // 伪指令：跳转标号
	OpPush           // Arg: 立即数
	OpPop
	OpDup
	OpLoad  // Arg: 局部槽位, Name: 变量名(调试信息)
	OpStore // Arg: 局部槽位, Name: 变量名(调试信息)
	OpAdd
	OpSub
	OpMul
	OpDiv
	OpMod
	OpNeg
	OpEq
	OpNe
	OpLt
	OpLe
	OpGt
	OpGe
	OpJmp  // Target: 标号
	OpJz   // Target: 标号; 弹出栈顶, 为 0 跳转
	OpJnz  // Target: 标号; 弹出栈顶, 非 0 跳转
	OpCall // Name: 函数名, Arg: 参数个数
	OpRet
	OpPrint
	OpHalt
)

// String 返回操作码的助记符。
func (op Opcode) String() string {
	if int(op) < 0 || int(op) >= len(opNames) {
		return "INVALID"
	}
	return opNames[op]
}

var opNames = [...]string{
	OpInvalid: "INVALID",
	OpLabel:   "label",
	OpPush:    "push",
	OpPop:     "pop",
	OpDup:     "dup",
	OpLoad:    "load",
	OpStore:   "store",
	OpAdd:     "add",
	OpSub:     "sub",
	OpMul:     "mul",
	OpDiv:     "div",
	OpMod:     "mod",
	OpNeg:     "neg",
	OpEq:      "eq",
	OpNe:      "ne",
	OpLt:      "lt",
	OpLe:      "le",
	OpGt:      "gt",
	OpGe:      "ge",
	OpJmp:     "jmp",
	OpJz:      "jz",
	OpJnz:     "jnz",
	OpCall:    "call",
	OpRet:     "ret",
	OpPrint:   "print",
	OpHalt:    "halt",
}

// StackEffect 返回指令对操作数栈的净影响（正=压入, 负=弹出）。
func (op Opcode) StackEffect() int {
	switch op {
	case OpPush, OpDup, OpLoad:
		return 1
	case OpPop, OpStore:
		return -1
	case OpAdd, OpSub, OpMul, OpDiv, OpMod, OpEq, OpNe, OpLt, OpLe, OpGt, OpGe:
		return -1
	case OpPrint:
		return -1
	case OpJz, OpJnz:
		return -1
	default:
		return 0
	}
}

// Cost 返回指令的静态执行成本估计，用于收益统计。
func (op Opcode) Cost() int {
	switch op {
	case OpLabel:
		return 0
	case OpPush, OpPop, OpDup, OpLoad, OpStore, OpJmp, OpRet:
		return 1
	case OpAdd, OpSub, OpMul, OpNeg, OpEq, OpNe, OpLt, OpLe, OpGt, OpGe, OpJz, OpJnz:
		return 2
	case OpDiv, OpMod:
		return 4
	case OpPrint:
		return 3
	case OpCall:
		return 5
	default:
		return 0
	}
}

// IsBranch 报告指令是否携带标号跳转目标。
func (op Opcode) IsBranch() bool {
	return op == OpJmp || op == OpJz || op == OpJnz
}

// IsTerminator 报告指令是否无条件终止当前基本块的顺序流。
func (op Opcode) IsTerminator() bool {
	return op == OpJmp || op == OpRet || op == OpHalt
}

// IsLabel 报告指令是否为标号伪指令。
func (op Opcode) IsLabel() bool { return op == OpLabel }

// ErrDivByZero 在除数为 0 时由解释器与折叠逻辑共享。
type ErrDivByZero struct{}

func (ErrDivByZero) Error() string { return "integer divide by zero" }

// ErrOverflow 在有符号整数运算溢出时由解释器与折叠逻辑共享。
type ErrOverflow struct{}

func (ErrOverflow) Error() string { return "integer overflow" }

// AddInt 与解释器共享同一套整数运算语义。
func AddInt(a, b int64) (int64, error) {
	r := a + b
	if (a >= 0 && b >= 0 && r < 0) || (a < 0 && b < 0 && r >= 0) {
		return 0, ErrOverflow{}
	}
	return r, nil
}

func SubInt(a, b int64) (int64, error) {
	r := a - b
	if (a >= 0 && b < 0 && r < 0) || (a < 0 && b >= 0 && r >= 0) {
		return 0, ErrOverflow{}
	}
	return r, nil
}

func MulInt(a, b int64) (int64, error) {
	if a == 0 || b == 0 {
		return 0, nil
	}
	if a == math.MinInt64 && b == -1 {
		return 0, ErrOverflow{}
	}
	r := a * b
	if r/b == a {
		return r, nil
	}
	return 0, ErrOverflow{}
}

func DivInt(a, b int64) (int64, error) {
	if b == 0 {
		return 0, ErrDivByZero{}
	}
	if a == math.MinInt64 && b == -1 {
		return 0, ErrOverflow{}
	}
	return a / b, nil
}

func ModInt(a, b int64) (int64, error) {
	if b == 0 {
		return 0, ErrDivByZero{}
	}
	if a == math.MinInt64 && b == -1 {
		return 0, ErrOverflow{}
	}
	return a % b, nil
}

func NegInt(a int64) (int64, error) {
	if a == math.MinInt64 {
		return 0, ErrOverflow{}
	}
	return -a, nil
}
