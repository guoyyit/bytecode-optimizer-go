package bytecode

// Instr 表示一条字节码指令（含调试与溯源信息）。
type Instr struct {
	Op     Opcode
	Arg    int64  // 立即数 / 槽位 / 参数个数
	Target string // 跳转目标标号或标号自身的名字
	Name   string // CALL 的函数名 / LOAD/STORE 的变量名
	Line   int    // 源码行号（调试信息）
	OrigPC int    // 优化前函数内指令下标（调试/断点映射）
}

// Function 表示一个函数：参数、局部变量槽位数与指令序列。
type Function struct {
	Name   string
	Params int
	Locals int
	Code   []*Instr
}

// Program 是一组函数构成的完整字节码程序。
type Program struct {
	Funcs []*Function
}

// Func 按名字查找函数。
func (p *Program) Func(name string) *Function {
	for _, f := range p.Funcs {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// Clone 深拷贝程序，保证优化过程不就地破坏原始程序。
func (p *Program) Clone() *Program {
	cp := &Program{Funcs: make([]*Function, len(p.Funcs))}
	for i, f := range p.Funcs {
		fc := &Function{
			Name:   f.Name,
			Params: f.Params,
			Locals: f.Locals,
			Code:   make([]*Instr, len(f.Code)),
		}
		for j, in := range f.Code {
			ic := *in
			fc.Code[j] = &ic
		}
		cp.Funcs[i] = fc
	}
	return cp
}

// LabelTargets 返回函数内所有标号名到指令下标的映射。
func (f *Function) LabelTargets() map[string]int {
	m := make(map[string]int)
	for i, in := range f.Code {
		if in.Op == OpLabel {
			m[in.Target] = i
		}
	}
	return m
}

// StaticCost 计算函数全部指令的静态成本之和（标号不计成本）。
func (f *Function) StaticCost() int {
	c := 0
	for _, in := range f.Code {
		c += in.Op.Cost()
	}
	return c
}

// StaticCost 计算整个程序的静态成本。
func (p *Program) StaticCost() int {
	c := 0
	for _, f := range p.Funcs {
		c += f.StaticCost()
	}
	return c
}
