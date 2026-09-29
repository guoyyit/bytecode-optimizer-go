package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/guoyyit/bytecode-optimizer-go/pkg/opt"
	"github.com/guoyyit/bytecode-optimizer-go/pkg/parser"
	"github.com/guoyyit/bytecode-optimizer-go/pkg/vm"
)

func main() {
	var (
		run      = flag.Bool("run", false, "run the program on the reference interpreter after optimization")
		runOrig  = flag.Bool("run-original", false, "run the unoptimized program")
		report   = flag.Bool("report", false, "print per-pass statistics report to stderr")
		outFile  = flag.String("o", "", "write optimized bytecode to file (default stdout)")
		maxRound = flag.Int("max-rounds", 64, "fixpoint iteration limit")
		noFold   = flag.Bool("no-constfold", false, "disable constant folding")
		noDCE    = flag.Bool("no-dce", false, "disable dead code elimination")
		noJT     = flag.Bool("no-jumpthread", false, "disable jump threading")
		noPeep   = flag.Bool("no-peephole", false, "disable redundant instruction merge")
		only     = flag.String("only", "", "run only the named pass: constfold|dce|jumpthread|peephole")
	)
	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: bcopt [flags] FILE.bc")
		os.Exit(2)
	}

	f, err := os.Open(flag.Arg(0))
	if err != nil {
		fatal(err)
	}
	orig, err := parser.Parse(f)
	f.Close()
	if err != nil {
		fatalf("parse error: %v", err)
	}

	if *runOrig {
		if err := vm.New(orig, os.Stdout).Run(); err != nil {
			fatal(err)
		}
		return
	}

	cfg := opt.Config{
		ConstantFold:   !*noFold,
		DeadCodeElim:   !*noDCE,
		JumpThreading:  !*noJT,
		RedundantMerge: !*noPeep,
		MaxRounds:      *maxRound,
	}
	if *only != "" {
		cfg = opt.Config{MaxRounds: *maxRound}
		switch *only {
		case "constfold":
			cfg.ConstantFold = true
		case "dce":
			cfg.DeadCodeElim = true
		case "jumpthread":
			cfg.JumpThreading = true
		case "peephole":
			cfg.RedundantMerge = true
		default:
			fatalf("unknown pass %q", *only)
		}
	}

	optimized, rep, err := opt.Optimize(orig, cfg)
	if err != nil {
		// 任何失败都回退到原始字节码，但仍以非零状态退出并报告可区分原因。
		fmt.Fprintf(os.Stderr, "%v\n(fell back to original bytecode)\n", err)
		optimized = orig
	}
	if *report {
		printReport(rep)
	}

	if *run {
		if err := vm.New(optimized, os.Stdout).Run(); err != nil {
			fatal(err)
		}
		return
	}

	w := os.Stdout
	if *outFile != "" {
		wf, err := os.Create(*outFile)
		if err != nil {
			fatal(err)
		}
		defer wf.Close()
		w = wf
	}
	if err := parser.Print(w, optimized); err != nil {
		fatal(err)
	}
}

func printReport(rep *opt.Report) {
	fmt.Fprintln(os.Stderr, "== optimization report ==")
	for _, rd := range rep.Rounds {
		fmt.Fprintf(os.Stderr, "round %d:\n", rd.Round)
		for _, st := range rd.Passes {
			fmt.Fprintf(os.Stderr, "  %-11s removed=%-3d folded=%-3d cost %d -> %d (delta=%d)\n",
				st.Name, st.Removed, st.Folded, st.CostBefore, st.CostAfter, st.Delta())
		}
	}
	if rep.Stable {
		fmt.Fprintln(os.Stderr, "fixpoint reached: re-optimizing would not change the program")
	}
	if rep.FellBack {
		fmt.Fprintf(os.Stderr, "FELL BACK TO ORIGINAL (reason=%s)\n", rep.Reason)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "bcopt:", err)
	os.Exit(1)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "bcopt: "+format+"\n", args...)
	os.Exit(1)
}
