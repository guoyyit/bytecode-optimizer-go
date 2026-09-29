package opt_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/guoyyit/bytecode-optimizer-go/pkg/bytecode"
	"github.com/guoyyit/bytecode-optimizer-go/pkg/opt"
	"github.com/guoyyit/bytecode-optimizer-go/pkg/parser"
)

func TestInvalidBytecodeParseErrors(t *testing.T) {
	cases := map[string]string{
		"unknown opcode":    "func main 0 0\n  frobnicate\nend\n",
		"missing end":       "func main 0 0\n  halt\n",
		"bad label":         "func main 0 0\n  jmp nowhere\n  halt\nend\n",
		"undefined call":    "func main 0 0\n  call ghost 0\n  halt\nend\n",
		"arity mismatch":    "func main 0 0\n  call f 1\n  halt\nend\nfunc f 0 0\n  push 1\n  ret\nend\n",
		"slot out of range": "func main 0 1\n  push 5\n  store 3\n  halt\nend\n",
		"duplicate label":   "func main 0 0\nlabel a\n  halt\nlabel a\n  halt\nend\n",
		"bad integer":       "func main 0 0\n  push abc\n  halt\nend\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parser.Parse(strings.NewReader(src)); err == nil {
				t.Fatalf("expected parse error for %s", name)
			}
		})
	}
}

// handBuild 绕过 parser 构造结构非法（栈下溢）程序，供校验器拦截。
func handUnderflow() *bytecode.Program {
	return &bytecode.Program{Funcs: []*bytecode.Function{{
		Name: "main",
		Code: []*bytecode.Instr{
			{Op: bytecode.OpAdd, OrigPC: 0},
			{Op: bytecode.OpPrint, OrigPC: 1},
			{Op: bytecode.OpHalt, OrigPC: 2},
		},
	}}}
}

func TestFallbackOnInvalidProgram(t *testing.T) {
	orig := handUnderflow()
	before := canonicalText(t, orig)

	got, rep, err := opt.Optimize(orig, allOn())
	if err == nil {
		t.Fatalf("expected optimization failure")
	}
	var fe *opt.Failure
	if !errors.As(err, &fe) {
		t.Fatalf("expected *opt.Failure, got %T %v", err, err)
	}
	if fe.Kind != opt.FailInvalidInput {
		t.Fatalf("expected invalid_input, got %s", fe.Kind)
	}
	if !rep.FellBack {
		t.Fatalf("report must record fallback")
	}
	if canonicalText(t, got) != before {
		t.Fatalf("must return byte-identical original program on failure")
	}
}

func TestEquivalenceFailureFallsBack(t *testing.T) {
	// 注入一个“总判定不等价”的检查器，模拟内部安全检查失败。
	src := "func main 0 0\n  push 1\n  push 2\n  add\n  print\n  halt\nend\n"
	orig := mustParse(t, src)
	cfg := allOn()
	cfg.EquivCheck = func(_, _ *bytecode.Program) error {
		return errors.New("simulated equivalence failure")
	}

	got, rep, err := opt.Optimize(orig, cfg)
	if err == nil {
		t.Fatalf("expected failure")
	}
	var fe *opt.Failure
	if !errors.As(err, &fe) || fe.Kind != opt.FailEquivalence {
		t.Fatalf("expected equivalence failure, got %v", err)
	}
	if !rep.FellBack || canonicalText(t, got) != canonicalText(t, orig) {
		t.Fatalf("must fully fall back to original")
	}
}

func TestFailureKindsAreDistinguishable(t *testing.T) {
	_, _, err := opt.Optimize(nil, allOn())
	var fe *opt.Failure
	if !errors.As(err, &fe) || fe.Kind != opt.FailInvalidInput {
		t.Fatalf("nil program must yield invalid_input, got %v", err)
	}

	// 未启用任何遍：立即稳定、原样返回、无错误。
	src := "func main 0 0\n  push 1\n  print\n  halt\nend\n"
	got, rep, err := opt.Optimize(mustParse(t, src), opt.Config{})
	if err != nil || !rep.Stable || canonicalText(t, got) != canonicalText(t, mustParse(t, src)) {
		t.Fatalf("no-pass config should be a no-op")
	}
}
