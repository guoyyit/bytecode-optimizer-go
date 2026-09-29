package opt_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/guoyyit/bytecode-optimizer-go/pkg/bytecode"
	"github.com/guoyyit/bytecode-optimizer-go/pkg/opt"
)

func TestVerifyRejectsStackDepthMergeMismatch(t *testing.T) {
	// 顺序路径到达 join 时栈深 1；taken 跳转路径到达 join 时栈深 0。
	p := &bytecode.Program{Funcs: []*bytecode.Function{{
		Name: "main",
		Code: []*bytecode.Instr{
			{Op: bytecode.OpPush, Arg: 1, OrigPC: 0},
			{Op: bytecode.OpJz, Target: "join", OrigPC: 1}, // taken: 深 0
			{Op: bytecode.OpPush, Arg: 2, OrigPC: 2},       // 顺序路径: 深 1
			{Op: bytecode.OpLabel, Target: "join", OrigPC: 3},
			{Op: bytecode.OpHalt, OrigPC: 4}, // 汇合点深度冲突: 1 vs 0
		},
	}}}

	_, _, err := opt.Optimize(p, allOn())
	if err == nil {
		t.Fatalf("expected stack-depth mismatch to be rejected")
	}
	if !strings.Contains(err.Error(), "stack depth") {
		t.Fatalf("error should mention stack depth: %v", err)
	}
	var fe *opt.Failure
	if !errors.As(err, &fe) || fe.Kind != opt.FailInvalidInput {
		t.Fatalf("expected invalid_input failure, got %v", err)
	}
}

func TestVerifyRejectsUnderflow(t *testing.T) {
	p := &bytecode.Program{Funcs: []*bytecode.Function{{
		Name: "main",
		Code: []*bytecode.Instr{
			{Op: bytecode.OpAdd, OrigPC: 0},
			{Op: bytecode.OpHalt, OrigPC: 1},
		},
	}}}
	if _, _, err := opt.Optimize(p, allOn()); err == nil {
		t.Fatalf("expected underflow rejection")
	}
}
