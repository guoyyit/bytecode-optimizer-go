package opt

import (
	"math/rand"
	"testing"

	"github.com/guoyyit/bytecode-optimizer-go/pkg/interp"
)

// genProgram builds a straight-line program with a single backward loop and
// constants chosen so the loop is bounded. It only uses validated patterns.
func genProgram(r *rand.Rand) string {
	n := 1 + r.Intn(6)
	var b []byte
	b = append(b, "func main\n"...)
	b = append(b, "  push 0\n  store acc\n"...)
	b = append(b, "  push "...)
	b = append(b, itoaTest(int64(n))...)
	b = append(b, "\n  store i\nloop:\n  load i\n  jz done\n"...)

	// Random constant arithmetic inside the loop; constants include zero and
	// one to exercise peephole identities but divisors are never zero.
	ops := []string{"add", "mul", "sub"}
	for k := 0; k < 1+r.Intn(4); k++ {
		c := r.Intn(3) // 0,1,2
		b = append(b, "  load acc\n  push "...)
		b = append(b, itoaTest(int64(c))...)
		b = append(b, "\n  "...)
		b = append(b, ops[r.Intn(len(ops))]...)
		b = append(b, "\n  store acc\n"...)
	}
	b = append(b, "  load i\n  push 1\n  sub\n  store i\n  jmp loop\ndone:\n  load acc\n  print\n  ret\nend\n"...)
	return string(b)
}

func itoaTest(v int64) string {
	return string(rune('0' + v))
}

func TestRandomDifferential(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	configs := []Config{
		DefaultConfig(),
		cfgOnly(PassConstantFold),
		cfgOnly(PassPeephole),
		cfgOnly(PassJump),
		cfgOnly(PassDCE),
		{Passes: map[string]bool{PassConstantFold: true, PassJump: true}},
		{Passes: map[string]bool{PassPeephole: true, PassDCE: true}},
	}
	for iter := 0; iter < 200; iter++ {
		src := genProgram(r)
		p := mustProg(t, src)
		v1, o1, e1 := interp.Run(p, "main")
		for ci, cfg := range configs {
			op, rep := Optimize(p, cfg)
			if rep.Failed {
				t.Fatalf("iter %d cfg %d: rollback: %s\n%s", iter, ci, rep.Reason, src)
			}
			if err := Validate(op); err != nil {
				t.Fatalf("iter %d cfg %d: post validate: %v", iter, ci, err)
			}
			v2, o2, e2 := interp.Run(op, "main")
			if !sameResult(v1, o1, e1, v2, o2, e2) {
				t.Fatalf("iter %d cfg %d:\n%s\nbefore v=%d out=%q err=%v\nafter  v=%d out=%q err=%v",
					iter, ci, src, v1, o1, e1, v2, o2, e2)
			}
		}
	}
}

// genBranchProgram builds straight-line code with a random forward JZ that
// may skip a block, plus a bounded backward loop and redundant patterns.
func genBranchProgram(r *rand.Rand) string {
	var b []byte
	b = append(b, "func main\n  push 0\n  store sum\n"...)
	limit := 1 + r.Intn(4)
	b = append(b, "  push "...)
	b = append(b, digits(limit)...)
	b = append(b, "\n  store i\nloop:\n  load i\n  jz done\n"...)

	// Randomly push a zero/non-zero condition; the skipped block only adds a
	// constant to sum, so total behavior is fully deterministic.
	cond := r.Intn(2)
	b = append(b, "  push "...)
	b = append(b, digits(cond)...)
	b = append(b, "\n  jz skip\n  load sum\n  push 3\n  add\n  store sum\n"...)
	if r.Intn(2) == 0 {
		b = append(b, "  push 42\n  pop\n"...)
	}
	b = append(b, "skip:\n  load sum\n  neg\n  neg\n  store sum\n"...)
	b = append(b, "  load i\n  push 1\n  sub\n  store i\n  jmp loop\ndone:\n  load sum\n  print\n  ret\nend\n"...)
	return string(b)
}

func digits(v int) string {
	if v == 0 {
		return "0"
	}
	var d []byte
	for v > 0 {
		d = append([]byte{byte('0' + v%10)}, d...)
		v /= 10
	}
	return string(d)
}

func TestRandomBranchDifferential(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	for iter := 0; iter < 300; iter++ {
		src := genBranchProgram(r)
		p := mustProg(t, src)
		v1, o1, e1 := interp.Run(p, "main")
		op, rep := Optimize(p, DefaultConfig())
		if rep.Failed {
			t.Fatalf("iter %d rollback: %s\n%s", iter, rep.Reason, src)
		}
		v2, o2, e2 := interp.Run(op, "main")
		if !sameResult(v1, o1, e1, v2, o2, e2) {
			t.Fatalf("iter %d:\n%s\nbefore v=%d out=%q err=%v\nafter  v=%d out=%q err=%v\n%s",
				iter, src, v1, o1, e1, v2, o2, e2, op.Format())
		}
		op2, r2 := Optimize(op, DefaultConfig())
		if r2.Failed || op2.Format() != op.Format() {
			t.Fatalf("iter %d: not a fixed point", iter)
		}
	}
}
