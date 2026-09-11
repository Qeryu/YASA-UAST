package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Batch B — error-path target contracts for the P2 error-model refactor.
//
// These tests are skipped in P1, but the assertion bodies are *falsifiable*:
// removing a t.Skip makes the test fail against the current implementation,
// because every assertion describes the P2 target (graceful failure, partial
// success, structured reporting) rather than today's panic/os.Exit/silent-empty
// behavior. They drive a freshly built CLI binary in a subprocess so the raw
// process semantics (exit code, stderr) can be observed; once P2 changes the
// function signatures these can be rewritten as in-process assertions.
//
// Not covered here because the symbol lives in package uast (and cannot be
// reached from package main):
//   - GetResult before Build returning an error instead of panicking.
// That case is covered by uast/repro_test.go. The os.Exit dispatch path for an
// unregistered ast node is also owned by uast/repro_test.go.

var (
	cliOnce sync.Once
	cliPath string
	cliErr  error
)

// buildCLI builds the current CLI once per test binary and returns its path.
func buildCLI(t *testing.T) string {
	t.Helper()
	cliOnce.Do(func() {
		goBin, err := exec.LookPath("go")
		if err != nil {
			cliErr = fmt.Errorf("go not found in PATH: %w", err)
			return
		}
		tmp, err := os.MkdirTemp("", "uast4go-cli-*")
		if err != nil {
			cliErr = err
			return
		}
		cliPath = filepath.Join(tmp, "uast4go")
		cmd := exec.Command(goBin, "build", "-buildvcs=false", "-o", cliPath, ".")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			cliErr = fmt.Errorf("go build failed: %v\n%s", err, out)
		}
	})
	if cliErr != nil {
		t.Fatalf("cannot build CLI under test: %v", cliErr)
	}
	return cliPath
}

type cliResult struct {
	stdout   string
	stderr   string
	exitCode int
}

func runCLI(t *testing.T, args ...string) cliResult {
	t.Helper()
	cmd := exec.Command(buildCLI(t), args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run CLI: %v", err)
		}
		code = ee.ExitCode()
	}
	return cliResult{stdout: stdout.String(), stderr: stderr.String(), exitCode: code}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// TestBadSyntaxSingleFile: a syntactically invalid file must fail gracefully:
// non-zero exit, no panic, and stderr must identify the offending file.
// Current behavior: panic (exit 2, "panic:" in stderr) => red when unskipped.
func TestBadSyntaxSingleFile(t *testing.T) {
	t.Skip("P2: error-model refactor — 去掉本行后本测试应失败（当前 panic, exit 2）")

	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.go")
	mustWriteFile(t, bad, "package p\n\nfunc broken( {\n")
	out := filepath.Join(dir, "out.json")

	res := runCLI(t, "-single", "-rootDir="+bad, "-output="+out)
	if res.exitCode == 0 {
		t.Fatalf("target: bad syntax must fail gracefully with non-zero exit; got 0\nstderr:\n%s", res.stderr)
	}
	combined := res.stderr + res.stdout
	if strings.Contains(combined, "panic:") {
		t.Fatalf("target: bad input must not panic; got:\n%s", combined)
	}
	if !strings.Contains(combined, "bad.go") {
		t.Fatalf("target: error must mention the offending file bad.go; got:\n%s", combined)
	}
}

// TestProjectPartialFailure: D11 semantics — one bad file in a project must not
// discard the good files; the good ones still appear and the failure is recorded.
// Current behavior: parser.ParseDir errors and the whole package is dropped
// (empty output) => red when unskipped.
func TestProjectPartialFailure(t *testing.T) {
	t.Skip("P2/D11: error-model refactor — 去掉本行后本测试应失败（当前 ParseDir 失败会丢弃整个包）")

	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "go.mod"), "module fixture\n\ngo 1.22\n")
	mustWriteFile(t, filepath.Join(dir, "good1.go"), "package fixture\n\nfunc Good1() int { return 1 }\n")
	mustWriteFile(t, filepath.Join(dir, "good2.go"), "package fixture\n\nfunc Good2() int { return 2 }\n")
	mustWriteFile(t, filepath.Join(dir, "bad.go"), "package fixture\n\nfunc broken( {\n")
	out := filepath.Join(dir, "out.json")

	res := runCLI(t, "-rootDir="+dir, "-output="+out)
	raw := mustReadFile(t, out)
	if !strings.Contains(raw, "good1.go") || !strings.Contains(raw, "good2.go") {
		t.Fatalf("D11 target: good files must still be emitted; got output:\n%.600s", raw)
	}
	if strings.Contains(res.stderr, "panic:") {
		t.Fatalf("D11 target: must not panic; stderr:\n%s", res.stderr)
	}
	// The exact reporting shape is P2's choice: stderr, or an error entry in the
	// output. Only require that it is surfaced somewhere.
	if strings.TrimSpace(res.stderr) == "" && !strings.Contains(raw, "bad.go") {
		t.Fatal("D11 target: the failed file must be recorded (stderr or output); stderr is empty and output does not mention bad.go")
	}
}

// TestRootDirMissing: a missing rootDir is a request-level failure and must
// return a non-zero status, not silently write an empty result.
// Current behavior: exit 0 => red when unskipped.
func TestRootDirMissing(t *testing.T) {
	t.Skip("P2: error-model refactor — 去掉本行后本测试应失败（当前 exit 0 且写空结果）")

	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist")
	out := filepath.Join(dir, "out.json")

	res := runCLI(t, "-rootDir="+missing, "-output="+out)
	if res.exitCode == 0 {
		t.Fatalf("request-level failure (missing rootDir) must return non-zero; got 0\nstderr:\n%s", res.stderr)
	}
	if !strings.Contains(res.stderr+res.stdout, "does-not-exist") {
		t.Fatalf("error should identify the missing rootDir; got:\n%s", res.stderr+res.stdout)
	}
}

// TestNoGoFilesDir: a directory with no .go files is a request-level failure.
// Current behavior: exit 0 with an empty result => red when unskipped.
func TestNoGoFilesDir(t *testing.T) {
	t.Skip("P2: error-model refactor — 去掉本行后本测试应失败（当前 exit 0 且写空结果）")

	dir := t.TempDir()
	out := filepath.Join(dir, "out.json")

	res := runCLI(t, "-rootDir="+dir, "-output="+out)
	if res.exitCode == 0 {
		t.Fatalf("request-level failure (no .go files) must return non-zero; got 0\nstderr:\n%s", res.stderr)
	}
}

// TestNoGoModReported: a project without go.mod must not be silently accepted.
// The assertion deliberately does not prescribe P2's exact shape (warning vs
// error exit) — only that the condition is surfaced.
// Current behavior: exit 0, empty stderr, "__unknown_module__" in output =>
// red when unskipped.
func TestNoGoModReported(t *testing.T) {
	t.Skip("P2: error-model refactor — 去掉本行后本测试应失败（当前静默 __unknown_module__，无上报）")

	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.go"), "package p\n\nfunc Ok() {}\n")
	out := filepath.Join(dir, "out.json")

	res := runCLI(t, "-rootDir="+dir, "-output="+out)
	if res.exitCode == 0 && strings.TrimSpace(res.stderr) == "" {
		t.Fatal("missing go.mod must be reported (non-zero exit or stderr); got exit 0 and empty stderr")
	}
}
