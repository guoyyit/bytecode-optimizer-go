package opt

import (
	"fmt"

	"github.com/guoyyit/bytecode-optimizer-go/pkg/bytecode"
)

// Verify 校验程序结构：跳转目标存在、栈深度在汇合点一致、无栈下溢。
func Verify(p *bytecode.Program) error {
	for _, f := range p.Funcs {
		if err := verifyFunc(f); err != nil {
			return fmt.Errorf("verify %s: %w", f.Name, err)
		}
	}
	return nil
}

func verifyFunc(f *bytecode.Function) error {
	if len(f.Code) == 0 {
		return fmt.Errorf("empty function body")
	}
	labels := f.LabelTargets()

	// depthAt[i] 是到达第 i 条指令时的栈深度；-1 表示尚不可达。
	depthAt := make([]int, len(f.Code))
	for i := range depthAt {
		depthAt[i] = -1
	}

	type work struct{ pc, depth int }
	queue := []work{{0, 0}}
	for len(queue) > 0 {
		w := queue[0]
		queue = queue[1:]
		if w.pc < 0 || w.pc >= len(f.Code) {
			return fmt.Errorf("control flow falls off code end at pc=%d", w.pc)
		}
		if d := depthAt[w.pc]; d >= 0 {
			if d != w.depth {
				return fmt.Errorf("stack depth mismatch at pc=%d: %d vs %d", w.pc, d, w.depth)
			}
			continue
		}
		depthAt[w.pc] = w.depth

		in := f.Code[w.pc]
		nd := w.depth
		switch in.Op {
		case bytecode.OpLabel:
		case bytecode.OpJmp:
			t, ok := labels[in.Target]
			if !ok {
				return fmt.Errorf("pc=%d jmp to undefined label %q", w.pc, in.Target)
			}
			queue = append(queue, work{t, nd})
			continue
		case bytecode.OpJz, bytecode.OpJnz:
			if nd < 1 {
				return fmt.Errorf("pc=%d branch with empty stack", w.pc)
			}
			t, ok := labels[in.Target]
			if !ok {
				return fmt.Errorf("pc=%d branch to undefined label %q", w.pc, in.Target)
			}
			nd--
			queue = append(queue, work{t, nd})
		case bytecode.OpRet:
			if nd < 1 {
				return fmt.Errorf("pc=%d %s with empty stack", w.pc, in.Op)
			}
			continue
		case bytecode.OpHalt:
			continue
		case bytecode.OpCall:
			argc := int(in.Arg)
			if nd < argc {
				return fmt.Errorf("pc=%d call underflow (depth=%d argc=%d)", w.pc, nd, argc)
			}
			nd = nd - argc + 1
		default:
			need := popCount(in.Op)
			if nd < need {
				return fmt.Errorf("pc=%d %s underflow (depth=%d need=%d)", w.pc, in.Op, nd, need)
			}
			nd += in.Op.StackEffect()
		}
		queue = append(queue, work{w.pc + 1, nd})
	}
	return nil
}

func popCount(op bytecode.Opcode) int {
	switch op {
	case bytecode.OpPop, bytecode.OpDup, bytecode.OpStore,
		bytecode.OpNeg, bytecode.OpPrint, bytecode.OpJz, bytecode.OpJnz:
		return 1
	case bytecode.OpAdd, bytecode.OpSub, bytecode.OpMul, bytecode.OpDiv, bytecode.OpMod,
		bytecode.OpEq, bytecode.OpNe, bytecode.OpLt, bytecode.OpLe, bytecode.OpGt, bytecode.OpGe:
		return 2
	default:
		return 0
	}
}
