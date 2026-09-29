package opt_test

import (
	"testing"

	"github.com/guoyyit/bytecode-optimizer-go/pkg/opt"
)

// 每个模板都是合法、可验证通过的程序；优化前后在参考解释器上必须完全一致。
var corpus = map[string]string{
	"jump_chain": `
func main 0 0
  push 1
  jnz a
  push 9
  print
  halt
label a
  jmp b
label b
  jmp c
label c
  push 7
  print
  halt
end`,
	"dead_after_terminator": `
func main 0 0
  push 5
  print
  halt
  push 6
  print
  halt
end`,
	"dead_block_after_label": `
func main 0 0
  push 5
  print
  halt
label ghost
  push 6
  print
  halt
end`,
	"constant_branch_taken": `
func main 0 0
  push 1
  jnz live
  push 11
  print
label live
  push 22
  print
  halt
end`,
	"constant_branch_not_taken": `
func main 0 0
  push 0
  jz live
  push 11
  print
label live
  push 22
  print
  halt
end`,
	"push_pop_peephole": `
func main 0 0
  push 99
  pop
  push 42
  print
  halt
end`,
	"dup_store_pop": `
func main 0 1
  push 17
  dup
  store 0 x
  pop
  load 0 x
  print
  halt
end`,
	"nested_calls": `
func main 0 0
  push 3
  call inc 1
  call double 1
  print
  halt
end
func inc 1 1
  load 0 a
  push 1
  add
  ret
end
func double 1 1
  load 0 a
  push 2
  mul
  ret
end`,
	"loop_with_break_path": `
func main 0 2
  push 0
  store 1 acc
  push 0
  store 0 i
label top
  load 0 i
  push 10
  ge
  jnz exit
  load 0 i
  push 5
  eq
  jz body
  jmp exit
label body
  load 1 acc
  load 0 i
  add
  store 1 acc
  load 0 i
  push 1
  add
  store 0 i
  jmp top
label exit
  load 1 acc
  print
  halt
end`,
	"compare_ops": `
func main 0 0
  push 3
  push 4
  lt
  print
  push 4
  push 4
  le
  print
  push 5
  push 5
  ne
  print
  halt
end`,
}

func TestDifferentialCorpus(t *testing.T) {
	for name, src := range corpus {
		t.Run(name, func(t *testing.T) {
			p := mustParse(t, src)
			diffRun(t, p, allOn())

			// 单独启用每个遍也要保持语义。
			for _, cfg := range []opt.Config{
				{ConstantFold: true},
				{DeadCodeElim: true},
				{JumpThreading: true},
				{RedundantMerge: true},
			} {
				diffRun(t, mustParse(t, src), cfg)
			}
		})
	}
}
