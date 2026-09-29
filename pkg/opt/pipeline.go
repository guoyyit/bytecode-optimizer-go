package opt

import (
	"errors"
	"fmt"

	"github.com/guoyyit/bytecode-optimizer-go/pkg/bytecode"
)

// Config 控制各遍的独立启用与组合。
type Config struct {
	ConstantFold   bool
	DeadCodeElim   bool
	JumpThreading  bool
	RedundantMerge bool
	MaxRounds      int
	EquivCheck     func(before, after *bytecode.Program) error
}

// FailKind 区分优化失败的原因类别。
type FailKind string

const (
	FailNone          FailKind = ""
	FailInvalidInput  FailKind = "invalid_input"
	FailVerify        FailKind = "verify"
	FailPassInternal  FailKind = "pass_internal"
	FailEquivalence   FailKind = "equivalence"
	FailNonConverging FailKind = "non_converging"
)

// Failure 携带可区分的失败原因；返回后调用方拿到的程序是原始字节码。
type Failure struct {
	Kind  FailKind
	Round int
	Pass  string
	Err   error
}

func (e *Failure) Error() string {
	return fmt.Sprintf("optimization failed (%s) at round=%d pass=%s: %v",
		e.Kind, e.Round, e.Pass, e.Err)
}

func (e *Failure) Unwrap() error { return e.Err }

// RoundReport 是一轮管线中各遍的统计。
type RoundReport struct {
	Round  int
	Passes []PassStats
}

// Report 是完整优化报告，包含不动点与回退信息。
type Report struct {
	Rounds   []RoundReport
	Stable   bool
	FellBack bool
	Reason   string
}

// Optimize 重复运行完整管线直到稳定；任何失败都整体回退到原始字节码。
func Optimize(orig *bytecode.Program, cfg Config) (*bytecode.Program, *Report, error) {
	rep := &Report{}
	if orig == nil {
		return nil, rep, &Failure{Kind: FailInvalidInput, Err: errors.New("nil program")}
	}

	if err := Verify(orig); err != nil {
		rep.FellBack = true
		rep.Reason = string(FailInvalidInput)
		return orig.Clone(), rep, &Failure{Kind: FailInvalidInput, Err: err}
	}

	passes := buildPasses(cfg)
	if len(passes) == 0 {
		rep.Stable = true
		return orig.Clone(), rep, nil
	}
	maxRounds := cfg.MaxRounds
	if maxRounds <= 0 {
		maxRounds = 64
	}

	current := orig.Clone()
	for round := 1; round <= maxRounds; round++ {
		roundRep := RoundReport{Round: round}
		changed := false

		for _, p := range passes {
			before := current.Clone()
			after, st, err := p.fn(current)
			if err != nil {
				rep.FellBack = true
				rep.Reason = string(FailPassInternal)
				rep.Rounds = append(rep.Rounds, roundRep)
				return orig.Clone(), rep, &Failure{
					Kind: FailPassInternal, Round: round, Pass: p.name, Err: err,
				}
			}
			if err := Verify(after); err != nil {
				rep.FellBack = true
				rep.Reason = string(FailVerify)
				rep.Rounds = append(rep.Rounds, roundRep)
				return orig.Clone(), rep, &Failure{
					Kind: FailVerify, Round: round, Pass: p.name, Err: err,
				}
			}
			if cfg.EquivCheck != nil && st.Changed() {
				if err := cfg.EquivCheck(before, after); err != nil {
					rep.FellBack = true
					rep.Reason = string(FailEquivalence)
					rep.Rounds = append(rep.Rounds, roundRep)
					return orig.Clone(), rep, &Failure{
						Kind: FailEquivalence, Round: round, Pass: p.name, Err: err,
					}
				}
			}
			roundRep.Passes = append(roundRep.Passes, st)
			if st.Changed() {
				changed = true
			}
			current = after
		}

		rep.Rounds = append(rep.Rounds, roundRep)
		if !changed {
			rep.Stable = true
			return current, rep, nil
		}
	}

	rep.FellBack = true
	rep.Reason = string(FailNonConverging)
	return orig.Clone(), rep, &Failure{
		Kind: FailNonConverging,
		Err:  fmt.Errorf("optimizer did not converge within %d rounds", maxRounds),
	}
}

type pass struct {
	name string
	fn   func(*bytecode.Program) (*bytecode.Program, PassStats, error)
}

func buildPasses(cfg Config) []pass {
	var ps []pass
	if cfg.ConstantFold {
		ps = append(ps, pass{"constfold", ConstantFold})
	}
	if cfg.DeadCodeElim {
		ps = append(ps, pass{"dce", DeadCodeElim})
	}
	if cfg.JumpThreading {
		ps = append(ps, pass{"jumpthread", JumpThreading})
	}
	if cfg.RedundantMerge {
		ps = append(ps, pass{"peephole", RedundantMerge})
	}
	return ps
}
