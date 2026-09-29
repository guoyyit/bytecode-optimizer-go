package parser

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/guoyyit/bytecode-optimizer-go/pkg/bytecode"
)

// Parse 从文本字节码格式解析出 Program。
//
// 语法（行导向，# 起始为注释；指令行尾的 # 数字表示源码行号）：
//
//	func NAME PARAMS LOCALS
//	  push INT
//	  load SLOT [VARNAME]
//	  store SLOT [VARNAME]
//	  label LABEL
//	  jmp|jz|jnz LABEL
//	  call NAME ARGC
//	  pop|dup|add|sub|mul|div|mod|neg|
//	  eq|ne|lt|le|gt|ge|ret|print|halt
//	end
//
// 第一个函数为入口函数。
func Parse(r io.Reader) (*bytecode.Program, error) {
	var prog bytecode.Program
	var cur *bytecode.Function
	seenFuncs := make(map[string]bool)

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<16), 1<<22)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		raw := sc.Text()
		srcLine, origPC := 0, -1
		if i := commentStart(raw); i >= 0 {
			comment := strings.TrimSpace(raw[i+1:])
			for _, field := range strings.Fields(comment) {
				kv := strings.SplitN(field, "=", 2)
				if len(kv) == 2 {
					switch kv[0] {
					case "line":
						if n, err := strconv.Atoi(kv[1]); err == nil {
							srcLine = n
						}
					case "pc":
						if n, err := strconv.Atoi(kv[1]); err == nil {
							origPC = n
						}
					}
				} else if n, err := strconv.Atoi(field); err == nil {
					srcLine = n
				}
			}
			raw = raw[:i]
		}
		text := strings.TrimSpace(raw)
		if text == "" {
			continue
		}
		fields := strings.Fields(text)
		head := fields[0]

		switch head {
		case "func":
			if cur != nil {
				return nil, fmt.Errorf("line %d: missing end before func", lineNo)
			}
			if len(fields) != 4 {
				return nil, fmt.Errorf("line %d: func NAME PARAMS LOCALS", lineNo)
			}
			params, err := strconv.Atoi(fields[2])
			if err != nil || params < 0 {
				return nil, fmt.Errorf("line %d: invalid params", lineNo)
			}
			locals, err := strconv.Atoi(fields[3])
			if err != nil || locals < params {
				return nil, fmt.Errorf("line %d: invalid locals", lineNo)
			}
			if seenFuncs[fields[1]] {
				return nil, fmt.Errorf("line %d: duplicate function %q", lineNo, fields[1])
			}
			seenFuncs[fields[1]] = true
			cur = &bytecode.Function{Name: fields[1], Params: params, Locals: locals}
		case "end":
			if cur == nil {
				return nil, fmt.Errorf("line %d: end without func", lineNo)
			}
			prog.Funcs = append(prog.Funcs, cur)
			cur = nil
		default:
			if cur == nil {
				return nil, fmt.Errorf("line %d: instruction outside function: %q", lineNo, head)
			}
			in, err := parseInstr(fields, lineNo)
			if err != nil {
				return nil, err
			}
			in.Line = srcLine
			if origPC >= 0 {
				in.OrigPC = origPC
			} else {
				in.OrigPC = len(cur.Code)
			}
			cur.Code = append(cur.Code, in)
		}
	}

	if err := sc.Err(); err != nil {
		return nil, err
	}
	if cur != nil {
		return nil, fmt.Errorf("line %d: unterminated function %q", lineNo, cur.Name)
	}
	if len(prog.Funcs) == 0 {
		return nil, fmt.Errorf("empty program: no functions")
	}
	if err := crossCheck(&prog); err != nil {
		return nil, err
	}
	return &prog, nil
}

// commentStart 返回调试注释 '#' 的位置：它必须位于行首或空白之后，
// 这样操作数文本里的 '#' 不会被误认为注释。
func commentStart(raw string) int {
	i := strings.IndexByte(raw, '#')
	if i < 0 {
		return -1
	}
	if i == 0 || raw[i-1] == ' ' || raw[i-1] == '\t' {
		return i
	}
	return -1
}

func parseInstr(fields []string, lineNo int) (*bytecode.Instr, error) {
	name := fields[0]
	args := fields[1:]
	need := func(n int) error {
		if len(args) != n {
			return fmt.Errorf("line %d: %s expects %d operand(s)", lineNo, name, n)
		}
		return nil
	}
	intArg := func(pos int) (int64, error) {
		v, err := strconv.ParseInt(args[pos], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("line %d: %s has invalid integer %q", lineNo, name, args[pos])
		}
		return v, nil
	}

	make0 := func(op bytecode.Opcode) (*bytecode.Instr, error) {
		if err := need(0); err != nil {
			return nil, err
		}
		return &bytecode.Instr{Op: op}, nil
	}

	switch name {
	case "push":
		if err := need(1); err != nil {
			return nil, err
		}
		v, err := intArg(0)
		if err != nil {
			return nil, err
		}
		return &bytecode.Instr{Op: bytecode.OpPush, Arg: v}, nil
	case "pop":
		return make0(bytecode.OpPop)
	case "dup":
		return make0(bytecode.OpDup)
	case "load", "store":
		op := bytecode.OpLoad
		if name == "store" {
			op = bytecode.OpStore
		}
		if len(args) != 1 && len(args) != 2 {
			return nil, fmt.Errorf("line %d: %s SLOT [NAME]", lineNo, name)
		}
		slot, err := strconv.Atoi(args[0])
		if err != nil || slot < 0 {
			return nil, fmt.Errorf("line %d: invalid slot", lineNo)
		}
		in := &bytecode.Instr{Op: op, Arg: int64(slot)}
		if len(args) == 2 {
			in.Name = args[1]
		}
		return in, nil
	case "label":
		if err := need(1); err != nil {
			return nil, err
		}
		return &bytecode.Instr{Op: bytecode.OpLabel, Target: args[0]}, nil
	case "jmp", "jz", "jnz":
		if err := need(1); err != nil {
			return nil, err
		}
		op := bytecode.OpJmp
		switch name {
		case "jz":
			op = bytecode.OpJz
		case "jnz":
			op = bytecode.OpJnz
		}
		return &bytecode.Instr{Op: op, Target: args[0]}, nil
	case "call":
		if err := need(2); err != nil {
			return nil, err
		}
		argc, err := strconv.Atoi(args[1])
		if err != nil || argc < 0 {
			return nil, fmt.Errorf("line %d: call expects NAME ARGC>=0", lineNo)
		}
		return &bytecode.Instr{Op: bytecode.OpCall, Name: args[0], Arg: int64(argc)}, nil
	case "add":
		return make0(bytecode.OpAdd)
	case "sub":
		return make0(bytecode.OpSub)
	case "mul":
		return make0(bytecode.OpMul)
	case "div":
		return make0(bytecode.OpDiv)
	case "mod":
		return make0(bytecode.OpMod)
	case "neg":
		return make0(bytecode.OpNeg)
	case "eq":
		return make0(bytecode.OpEq)
	case "ne":
		return make0(bytecode.OpNe)
	case "lt":
		return make0(bytecode.OpLt)
	case "le":
		return make0(bytecode.OpLe)
	case "gt":
		return make0(bytecode.OpGt)
	case "ge":
		return make0(bytecode.OpGe)
	case "ret":
		return make0(bytecode.OpRet)
	case "print":
		return make0(bytecode.OpPrint)
	case "halt":
		return make0(bytecode.OpHalt)
	default:
		return nil, fmt.Errorf("line %d: unknown opcode %q", lineNo, name)
	}
}

func crossCheck(prog *bytecode.Program) error {
	funcNames := make(map[string]bool, len(prog.Funcs))
	for _, f := range prog.Funcs {
		funcNames[f.Name] = true
	}
	for _, f := range prog.Funcs {
		labels := make(map[string]bool)
		for pc, in := range f.Code {
			switch in.Op {
			case bytecode.OpLabel:
				if labels[in.Target] {
					return fmt.Errorf("func %s: duplicate label %q", f.Name, in.Target)
				}
				labels[in.Target] = true
			case bytecode.OpLoad, bytecode.OpStore:
				if int(in.Arg) >= f.Locals {
					return fmt.Errorf("func %s pc %d: slot %d out of range (locals=%d)",
						f.Name, pc, in.Arg, f.Locals)
				}
			case bytecode.OpCall:
				if !funcNames[in.Name] {
					return fmt.Errorf("func %s pc %d: call to undefined function %q",
						f.Name, pc, in.Name)
				}
				if callee := prog.Func(in.Name); int(in.Arg) != callee.Params {
					return fmt.Errorf("func %s pc %d: call %q expects %d args, got %d",
						f.Name, pc, in.Name, callee.Params, in.Arg)
				}
			}
		}
		for pc, in := range f.Code {
			if in.Op.IsBranch() && !labels[in.Target] {
				return fmt.Errorf("func %s pc %d: jump to undefined label %q",
					f.Name, pc, in.Target)
			}
		}
	}
	return nil
}
