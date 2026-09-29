package opt

import (
	"testing"

	"github.com/guoyyit/bytecode-optimizer-go/pkg/interp"
)

func TestExceptionBehaviorPreserved(t *testing.T) {
	cases := []string{
		// literal zero divisor must not be folded away
		`func main
  push 1
  push 2
  add
  print
  push 9
  push 0
  mod
  print
  ret
end
`,
		// divisor computed at runtime, dead code before the fault removed
		`func main
  push 2
  push 2
  sub
  store z
  jmp skip
  push 1
  print
skip:
  push 55
  load z
  div
  print
  ret
end
`,
		// fault inside a called function, with foldable code around it
		`func boom(n)
  push 6
  push 7
  mul
  pop
  push 10
  load n
  div
  ret
	end

func main
  push 0
  call boom 1
  ret
end
	`,
	}
	for i, src := range cases {
		p := mustProg(t, src)
		op, rep := Optimize(p, DefaultConfig())
		if rep.Failed {
			t.Fatalf("case %d: %s", i, rep.Reason)
		}
		_, o1, e1 := interp.Run(p, "main")
		_, o2, e2 := interp.Run(op, "main")
		if o1 != o2 {
			t.Fatalf("case %d: output before fault differs: %q vs %q", i, o1, o2)
		}
		if e1 == nil || e2 == nil || e1.Error() != e2.Error() {
			t.Fatalf("case %d: exception not preserved:\n before=%v\n after =%v", i, e1, e2)
		}
	}
}
