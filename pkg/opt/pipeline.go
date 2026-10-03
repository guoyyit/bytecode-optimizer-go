package opt

import (
	"fmt"

	"github.com/guoyyit/bytecode-optimizer-go/pkg/ir"
)

// Pass names.
const (
	PassConstantFold = "constfold"
	PassDCE          = "dce"
	PassJump         = "jump"
	PassPeephole     = "peephole"
)

// AllPasses lists every optimization pass in default pipeline order.
var AllPasses = []string{PassConstantFold, PassPeephole, PassJump, PassDCE}

// Stats is the per-pass report for one pipeline iteration.
type Stats struct {
	Name       string
	Iteration  int
	Removed    int
	CostBefore int64
	CostAfter  int64
	changed    bool
}

// FailureKind distinguishes failure causes reported on rollback.
type FailureKind int

func (k FailureKind) String() string {
	switch k {
	case FailureInvalidInput:
		return "invalid_input"
	case FailureInternal:
		return "internal_error"
	default:
		return "none"
	}
}

const (
	FailureNone FailureKind = iota
	FailureInvalidInput
	FailureInternal
)

// Config selects the enabled passes.
type Config struct {
	Passes map[string]bool
}

// DefaultConfig enables every pass.
func DefaultConfig() Config {
	cfg := Config{Passes: map[string]bool{}}
	for _, name := range AllPasses {
		cfg.Passes[name] = true
	}
	return cfg
}

// Report is the full optimization report.
type Report struct {
	Iterations   int
	Passes       []Stats
	Stable       bool
	Failed       bool
	Reason       string
	Kind         FailureKind
	TotalRemoved int
	CostBefore   int64
	CostAfter    int64
}

type passFunc func(f *ir.Func) Stats

var passTable = map[string]passFunc{
	PassConstantFold: passConstantFold,
	PassPeephole:     passPeephole,
	PassJump:         passJumpFold,
	PassDCE:          passDCE,
}

const maxIterations = 64

// Optimize runs the selected passes to a fixed point. It first validates the
// input and never mutates it: all work happens on a clone. When the input is
// illegal or an internal invariant breaks (a pass yields invalid bytecode or
// fails to reach a fixed point), the original program is returned untouched
// together with a failed Report carrying a distinguishable reason.
func Optimize(p *ir.Program, cfg Config) (*ir.Program, *Report) {
	if err := Validate(p); err != nil {
		return p, &Report{
			Failed: true,
			Kind:   FailureInvalidInput,
			Reason: err.Error(),
		}
	}

	for name := range cfg.Passes {
		if _, ok := passTable[name]; !ok {
			return p, &Report{
				Failed: true,
				Kind:   FailureInvalidInput,
				Reason: fmt.Sprintf("unknown optimization pass %q", name),
			}
		}
	}
	passes := selectedPasses(cfg)

	work := p.Clone()
	beforeCost := programCost(p)
	report := &Report{CostBefore: beforeCost}

	var recovered any
	apply := func(fn func()) (ok bool) {
		defer func() {
			if r := recover(); r != nil {
				recovered = r
				ok = false
			}
		}()
		fn()
		return true
	}

	for iter := 1; iter <= maxIterations; iter++ {
		changed := false
		for _, name := range passes {
			costBefore := programCost(work)
			var stats Stats
			if !apply(func() {
				for _, f := range work.Funcs {
					s := passTable[name](f)
					stats.Removed += s.Removed
					stats.changed = stats.changed || s.changed
				}
			}) {
				return rollback(p, report, FailureInternal,
					fmt.Sprintf("pass %s panicked: %v", name, recovered))
			}
			if err := Validate(work); err != nil {
				return rollback(p, report, FailureInternal,
					fmt.Sprintf("pass %s produced invalid bytecode: %v", name, err))
			}
			costAfter := programCost(work)
			stats.Name = name
			stats.Iteration = iter
			stats.CostBefore = costBefore
			stats.CostAfter = costAfter
			report.Passes = append(report.Passes, stats)
			report.TotalRemoved += stats.Removed
			changed = changed || stats.changed
		}

		report.Iterations = iter
		if !changed {
			// A full iteration changed nothing: every enabled pass is at its
			// own fixed point on this program, so the output is a true fixed
			// point of the whole pipeline. No transformation may happen after
			// this check — anything that mutates the program must run inside
			// the loop as a counted pass, or the result would not be stable
			// under re-optimization.
			report.Stable = true
			report.CostAfter = programCost(work)
			return work, report
		}
		if iter == maxIterations {
			return rollback(p, report, FailureInternal,
				fmt.Sprintf("optimization did not stabilize after %d iterations", maxIterations))
		}
	}
	return rollback(p, report, FailureInternal, "unreachable optimization state")
}

func selectedPasses(cfg Config) []string {
	if len(cfg.Passes) == 0 {
		return append([]string(nil), AllPasses...)
	}
	var out []string
	for _, name := range AllPasses {
		if cfg.Passes[name] {
			out = append(out, name)
		}
	}
	return out
}

func rollback(p *ir.Program, r *Report, kind FailureKind, reason string) (*ir.Program, *Report) {
	r.Failed = true
	r.Kind = kind
	r.Reason = reason
	r.Stable = false
	return p, r
}

// FuncCost sums the weighted execution costs of the executable instructions.
// Labels are pseudo-ops and contribute zero.
func FuncCost(f *ir.Func) int64 {
	var c int64
	for _, ins := range f.Code {
		if ins.Op != ir.OpLabel {
			c += ins.Op.Cost()
		}
	}
	return c
}

func programCost(p *ir.Program) int64 {
	var c int64
	for _, f := range p.Funcs {
		c += FuncCost(f)
	}
	return c
}
