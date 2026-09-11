package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"uast4go/api"
)

// Batch B — error-path contracts (now active, no longer skipped).
//
// They drive a freshly built CLI binary in a subprocess so the raw process
// semantics (exit code, stderr) can be observed. Contract:
//
//   - single-file parse failure -> non-zero exit, no panic, stderr names the file;
//   - project with a bad file    -> good files still emitted, failure reported;
//   - request-level failure      -> non-zero exit;
//   - missing go.mod             -> surfaced (stderr), not silently swallowed.
//
// Not covered here because the symbol lives in package uast (and cannot be
// reached from package main): GetResult-before-Build and the unregistered-node
// dispatch path. Both are owned by uast/repro_test.go.

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
func TestBadSyntaxSingleFile(t *testing.T) {
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
	// S1: parse failure must not write a product.
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("target: parse failure must not write output; stat err = %v", err)
	}
}

// TestProjectPartialFailure: D11 semantics — one bad file in a project must not
// discard the good files; the good ones still appear and the failure is recorded.
func TestProjectPartialFailure(t *testing.T) {
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
func TestRootDirMissing(t *testing.T) {
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
func TestNoGoFilesDir(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.json")

	res := runCLI(t, "-rootDir="+dir, "-output="+out)
	if res.exitCode == 0 {
		t.Fatalf("request-level failure (no .go files) must return non-zero; got 0\nstderr:\n%s", res.stderr)
	}
}

// TestNoGoModReported: a project without go.mod must not be silently accepted.
// The assertion deliberately does not prescribe the exact shape (warning vs
// error exit) — only that the condition is surfaced.
func TestNoGoModReported(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.go"), "package p\n\nfunc Ok() {}\n")
	out := filepath.Join(dir, "out.json")

	res := runCLI(t, "-rootDir="+dir, "-output="+out)
	if res.exitCode == 0 && strings.TrimSpace(res.stderr) == "" {
		t.Fatal("missing go.mod must be reported (non-zero exit or stderr); got exit 0 and empty stderr")
	}
}

// S1: single-file semantics are unified through severity classification.
// Warning-only entries (builder-level degradation, e.g. an unsupported node
// turned into Noop) must still write the product and exit 0 — consistent with
// project mode; error-severity entries (parse/read failure) exit 1 with no
// product. runSingleFileWith's parse seam makes both paths testable without a
// synthetic Go source that triggers builder degradation.

func TestRunSingleFileWarningOnlyWritesProduct(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.json")
	payload := []byte("{\"ok\":true}\n")
	parse := func(string) ([]byte, []api.ParseError, error) {
		return payload, []api.ParseError{{
			File:     "x.go",
			Message:  "unsupported node",
			Severity: api.SeverityWarning,
			Kind:     api.KindUnsupportedNode,
		}}, nil
	}

	if code := runSingleFileWith(parse, "x.go", out); code != 0 {
		t.Fatalf("warning-only exit = %d, want 0", code)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("warning-only must write the product: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("product = %q, want %q", got, payload)
	}
}

func TestRunSingleFileErrorSeverityWritesNothing(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.json")
	parse := func(string) ([]byte, []api.ParseError, error) {
		return nil, []api.ParseError{{
			File:     "bad.go",
			Message:  "syntax error",
			Severity: api.SeverityError,
			Kind:     api.KindParseError,
		}}, nil
	}

	if code := runSingleFileWith(parse, "bad.go", out); code != 1 {
		t.Fatalf("error-severity exit = %d, want 1", code)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("error-severity must not write a product; stat err = %v", err)
	}
}
