package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCLIAsSubprocess drives the built binary so exit codes can be asserted.
func TestCLIAsSubprocess(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "optbc")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	good := filepath.Join(t.TempDir(), "good.bc")
	if err := os.WriteFile(good, []byte("func main\n  push 2\n  push 3\n  add\n  print\n  ret\nend\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(t.TempDir(), "bad.bc")
	if err := os.WriteFile(bad, []byte("func main\n  jz missing\n  ret\nend\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) (string, int) {
		cmd := exec.Command(bin, args...)
		out, _ := cmd.CombinedOutput()
		code := 0
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		}
		return string(out), code
	}

	out, code := run("run", good)
	if code != 0 || !strings.Contains(out, "5") {
		t.Fatalf("run: code=%d out=%q", code, out)
	}

	out, code = run("opt", "-stats", "-verify", good)
	if code != 0 || !strings.Contains(out, "PUSH 5") || !strings.Contains(out, "verification OK") {
		t.Fatalf("opt: code=%d out=%q", code, out)
	}

	_, code = run("opt", bad)
	if code != 3 {
		t.Fatalf("rollback exit code = %d, want 3", code)
	}

	_, code = run("run", bad)
	if code != 1 {
		t.Fatalf("runtime/invalid run exit code = %d, want 1", code)
	}
}
