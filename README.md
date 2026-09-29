# bytecode-optimizer-go

面向简单栈式虚拟机字节码的优化管线。支持算术、分支、循环与函数调用；
优化后的程序在参考解释器上的**可观测行为（输出与异常类别/原因）与优化前完全一致**，
并提供每遍优化的可量化收益统计。优化结果可继续用于执行与单步/断点调试。

## 目录结构

| 路径 | 职责 |
| --- | --- |
| `pkg/bytecode` | 指令集、操作码元数据、与解释器共享的整数运算语义（溢出/除零） |
| `pkg/parser` | 文本字节码解析与回写（保留源码行号与 `OrigPC` 调试映射） |
| `pkg/vm` | 参考解释器、运行时错误定位、断点与单步回调 |
| `pkg/opt` | 四个优化遍、结构校验、可回退的不动点管线 |
| `cmd/bcopt` | 命令行工具 |

## 字节码格式

行导向文本，`#` 起为注释。指令行尾可用 `# line=N pc=M` 记录源码行与
“优化前指令下标”。第一个函数为入口。

```
func NAME PARAMS LOCALS
  push INT            # 压入常量
  load SLOT [NAME]    # 读局部变量（NAME 为调试用变量名）
  store SLOT [NAME]   # 弹栈写入局部变量
  pop | dup
  add | sub | mul | div | mod | neg
  eq | ne | lt | le | gt | ge   # 结果压入 1/0
  label L
  jmp | jz | jnz L
  call NAME ARGC
  ret | print | halt
end
```

语义约定：

- 比较与 `jz/jnz` 以 `0` 为假，非 `0` 为真。
- `ret` 弹出栈顶作为函数返回值；`halt` 立即终结**整个程序**（不只是当前函数）。
- 整数为有符号 64 位；`add/sub/mul/div/mod/neg` 溢出、除零会抛出可定位的运行时错误。
- 参数占用局部槽位前缀（`LOCALS >= PARAMS`）。

## 优化安全语义

所有优化遍都在**函数内**进行，不改变函数签名、调用约定与可观测事件顺序。
管线在每一遍之后都执行结构校验（`pkg/opt/verify.go`）：

1. 所有跳转目标标号必须存在；
2. 控制流图汇合点的**栈深度必须一致**，任何路径都不得栈下溢；
3. 控制流不得坠出函数末尾。

只要任何一遍产生非法结构、内部失败或（可选的）差分检查失败，
`Optimize` 都会**整体回退到原始字节码**，并返回可区分原因的 `*opt.Failure`：

| `FailKind` | 含义 |
| --- | --- |
| `invalid_input` | 原始字节码未通过校验（坏跳转/栈冲突/下溢等） |
| `pass_internal` | 某优化遍内部返回错误 |
| `verify` | 优化结果未通过结构校验（理论上不应发生） |
| `equivalence` | 调用方提供的等价性检查否决了该遍结果 |
| `non_converging` | 超过最大迭代轮数仍未稳定 |

### 异常行为保真

常量折叠只在能证明**不会**触发运行时错误时执行：

- `push a; push b; div|mod` 当 `b == 0` 时**不折叠**（保留除零）；
- 任何会溢出的 `add/sub/mul/div/mod/neg` **不折叠**（保留溢出）；
- `push c; jz|jnz` 折叠为直接跳转/删除时，常量条件不产生异常，安全。

折叠器与解释器共享 `pkg/bytecode` 中的同一套运算函数，避免语义漂移。

### 跳转目标、重定位与栈深度

- 任何跨标号的多指令模式都**不会**被折叠或合并，防止跳转从模式中间进入。
- 跳转链折叠只重定向到链的最终目标，并能识别回环；标号紧邻的无条件 `jmp`
  仅在其目标标号紧随其后时删除（fall-through 等价）。
- 死代码消除基于控制流图可达性，并只保留仍被可达分支引用的标号。
- 删除/重排后指令的 `OrigPC` 仍指向原始指令，跳转目标始终以标号解析，
  因此重定位后目标地址自动正确。

### 调试信息保真

- 每条指令保留源码行号 `Line` 与优化前下标 `OrigPC`；变量名随 `load/store` 保留。
- 折叠得到的常量指令继承被折叠运算指令的位置，单步只停在真实执行的指令上。
- 断点按“优化前 PC”设置；优化后命中间隙内的指令不产生额外停点，
  命中次数与优化前一致（见 `pkg/vm/debug_test.go`）。
- `parser.Print` 输出的字节码可被 `parser.Parse` 无损读回（含调试映射），
  可继续用于执行与调试。

## 各遍说明与适用条件

| 遍 | 名称 | 作用 | 安全适用条件 |
| | --- | --- | --- |
| 常量折叠 | `constfold` | 常量二元运算/比较、`neg`、常量条件分支 | 运算不溢出、不除零；模式区间无标号 |
| 死代码消除 | `dce` | 删除终结指令后不可达代码与不可达孤立标号 | 基于 CFG 可达性，保守安全 |
| 跳转链折叠 | `jumpthread` | 把分支重定向到链终目标；删除紧邻标号的冗余 `jmp` | 仅跟随纯无条件跳转，识别回环 |
| 冗余指令合并 | `peephole` | `push/load;pop`、`dup;pop`、`dup;store;pop` | 纯生产者无副作用；模式区间无标号 |

各遍可独立启用（CLI `--only` / `--no-*`，或 `opt.Config` 字段）。
管线按 `constfold → dce → jumpthread → peephole` 的顺序重复执行，
直到一整轮中没有任何遍产生变化（不动点）。对已最优程序再优化：
一轮内所有遍报告零变化、程序逐字节不变。

## 收益统计

每遍产出 `PassStats{Removed, Folded, CostBefore, CostAfter}`：

- `Removed`：删除或合并掉的指令条数；
- `Folded`：折叠掉的常量运算/常量分支数；
- `Cost*`：按操作码静态权重估算的执行成本（`div/mod` 更贵、`call` 最贵、标号为 0）。

CLI 使用 `--report` 在 stderr 打印逐轮逐遍报告。

## 用法

```bash
go run ./cmd/bcopt examples/sum_squares.bc -report     # 优化并打印报告（输出到 stdout）
go run ./cmd/bcopt examples/sum_squares.bc -run         # 优化后在参考解释器上运行
go run ./cmd/bcopt examples/sum_squares.bc -run-original
go run ./cmd/bcopt examples/sum_squares.bc --only constfold
go run ./cmd/bcopt examples/sum_squares.bc --no-dce -o out.bc
```

作为库使用：

```go
prog, _ := parser.Parse(file)
opt2, report, err := opt.Optimize(prog, opt.Config{
    ConstantFold: true, DeadCodeElim: true,
    JumpThreading: true, RedundantMerge: true,
    // 可选：在管线内做差分等价验证，失败即整体回退
    EquivCheck: func(before, after *bytecode.Program) error { return nil },
})
var fe *opt.Failure
if errors.As(err, &fe) { /* fe.Kind 区分原因；opt2 已是原始程序副本 */ }
_ = vm.New(opt2, os.Stdout).Run()
```

## 测试

```bash
go test ./... -race
```

测试覆盖：

- 语义等价差分（算术、分支、循环、递归/嵌套调用、除零、溢出）；
- 非法输入解析失败、栈深度汇合冲突/下溢触发回退、可区分失败原因；
- 调试信息：优化后单步序列是原始序列的子序列、断点命中数一致、Print/Parse 往返保映射；
- 不动点：已最优程序重复优化内容不变、各遍独立启用也收敛；
- 共享算术语义的溢出/除零边界。
