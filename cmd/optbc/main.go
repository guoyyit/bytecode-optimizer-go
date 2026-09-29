package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/guoyyit/bytecode-optimizer-go/pkg/interp"
	"github.com/guoyyit/bytecode-optimizer-go/pkg/ir"
	"github.com/guoyyit/bytecode-optimizer-go/pkg/opt"
	"github.com/guoyyit/bytecode-optimizer-go/pkg/parse"
)

const usage = `optbc - stack-bytecode optimizer and reference runner

usage:
  optbc parse <file>                      parse and pretty-print bytecode
  optbc run   <file> [-trace] [entry] [args...]  run on reference interpreter
  optbc opt   <file> [-o out] [-passes p] [-stats] [-verify] [-trace]

passes: constfold,dce,jump,peephole (default: all; repeat until stable)
exit code 2 on parse failure; 3 when optimization rolls back.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := normalizeFlags(os.Args[2:])

	var err error
	switch cmd {
	case "parse":
		err = cmdParse(args)
	case "run":
		err = cmdRun(args)
	case "opt":
		err = cmdOpt(args)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// normalizeFlags moves leading-looking flags ahead of the first positional
// argument, so both "opt file -stats" and "opt -stats file" parse.
func normalizeFlags(args []string) []string {
	var positional, flags []string
	valueFlags := map[string]bool{"-o": true, "-passes": true}
	for i := 0; i < len(args); i++ {
		a := args[i]
		key := a
		if eq := strings.IndexByte(a, '='); eq >= 0 {
			key = a[:eq]
		}
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
			if valueFlags[key] && !strings.Contains(a, "=") && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		} else {
			positional = append(positional, a)
		}
	}
	return append(flags, positional...)
}

func readProgram(file string) (*ir.Program, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	p, err := parse.Parse(string(data))
	return p, err
}

func cmdParse(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("parse expects one file")
	}
	p, err := readProgram(args[0])
	if err != nil {
		os.Exit(2)
		return err
	}
	if err := opt.Validate(p); err != nil {
		return err
	}
	fmt.Print(p.Format())
	return nil
}

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	trace := fs.Bool("trace", false, "trace interpreter steps to stderr")
	if err := fs.Parse(args); err != nil {
		return err
	}
	args = fs.Args()
	if len(args) < 1 {
		return fmt.Errorf("run expects <file> [entry] [args...]")
	}
	p, err := readProgram(args[0])
	if err != nil {
		os.Exit(2)
		return err
	}
	entry := "main"
	if len(args) >= 2 {
		entry = args[1]
	}
	var ints []int64
	if len(args) > 2 {
		for _, a := range args[2:] {
			v, err := strconv.ParseInt(a, 10, 64)
			if err != nil {
				return fmt.Errorf("invalid argument %q", a)
			}
			ints = append(ints, v)
		}
	}
	opts := interp.Options{}
	if *trace {
		opts.Trace = os.Stderr
	}
	result, out, rerr := interp.RunWith(p, entry, ints, opts)
	fmt.Print(out)
	if rerr != nil {
		return rerr
	}
	fmt.Println("=>", result)
	return nil
}

func cmdOpt(args []string) error {
	fs := flag.NewFlagSet("opt", flag.ContinueOnError)
	outFile := fs.String("o", "", "write optimized bytecode to file (default stdout)")
	passes := fs.String("passes", "", "comma-separated pass subset")
	showStats := fs.Bool("stats", false, "print per-pass statistics to stderr")
	verify := fs.Bool("verify", false, "re-run optimized program and compare observable behavior")
	trace := fs.Bool("trace", false, "trace interpreter steps when verifying")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("opt expects one input file")
	}
	p, err := readProgram(fs.Arg(0))
	if err != nil {
		os.Exit(2)
		return err
	}

	cfg := opt.Config{Passes: map[string]bool{}}
	if *passes != "" {
		for _, name := range strings.Split(*passes, ",") {
			name = strings.TrimSpace(name)
			if name != "" {
				cfg.Passes[name] = true
			}
		}
	}

	var entry string
	var runArgs []int64
	if *verify {
		entry = "main"
	}

	var beforeV int64
	var beforeOut string
	var beforeErr error
	if *verify {
		beforeV, beforeOut, beforeErr = interp.RunWith(p, entry, runArgs, interp.Options{})
	}

	optimized, rep := opt.Optimize(p, cfg)
	if rep.Failed {
		fmt.Fprintf(os.Stderr, "optimization rolled back (%s): %s\n", rep.Kind, rep.Reason)
		os.Exit(3)
	}
	if *showStats {
		printReport(rep)
	}

	if *verify {
		opts := interp.Options{}
		if *trace {
			opts.Trace = os.Stderr
		}
		afterV, afterOut, afterErr := interp.RunWith(optimized, entry, runArgs, opts)
		if !eqResult(beforeV, beforeOut, beforeErr, afterV, afterOut, afterErr) {
			fmt.Fprintf(os.Stderr, "verification FAILED: behavior differs after optimization\n")
			fmt.Fprintf(os.Stderr, "before: v=%d out=%q err=%v\n", beforeV, beforeOut, beforeErr)
			fmt.Fprintf(os.Stderr, "after:  v=%d out=%q err=%v\n", afterV, afterOut, afterErr)
			os.Exit(4)
		}
		fmt.Fprintln(os.Stderr, "verification OK: observable behavior identical")
	}

	text := optimized.Format()
	if *outFile == "" {
		fmt.Print(text)
	} else {
		if err := os.WriteFile(*outFile, []byte(text), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func eqResult(v1 int64, o1 string, e1 error, v2 int64, o2 string, e2 error) bool {
	if v1 != v2 || o1 != o2 {
		return false
	}
	return fmt.Sprint(e1) == fmt.Sprint(e2)
}

func printReport(r *opt.Report) {
	fmt.Fprintf(os.Stderr, "iterations=%d stable=%v total_removed=%d cost %d -> %d (saved %d, %.1f%%)\n",
		r.Iterations, r.Stable, r.TotalRemoved, r.CostBefore, r.CostAfter,
		r.CostBefore-r.CostAfter, 100*float64(r.CostBefore-r.CostAfter)/float64(max1(r.CostBefore)))
	for _, s := range r.Passes {
		fmt.Fprintf(os.Stderr, "  iter %d %-9s removed=%3d cost %3d -> %3d\n",
			s.Iteration, s.Name, s.Removed, s.CostBefore, s.CostAfter)
	}
}

func max1(v int64) int64 {
	if v <= 0 {
		return 1
	}
	return v
}
