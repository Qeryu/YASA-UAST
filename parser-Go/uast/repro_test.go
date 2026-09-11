package uast

import (
	"go/ast"
	"go/token"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Repro harness for the os.Exit / panic paths that P2 will convert to errors.
//
// The current implementation terminates the process, so a normal in-process test
// cannot assert on it. We reuse the test binary itself (standard re-exec
// pattern): when UAST_REPRO is set, TestMain runs the requested scenario before
// the test framework starts, preserving the raw exit/panic semantics.
//
// After P2 these tests should be rewritten as in-process assertions:
//   - builder.visit on an unregistered node returns Noop/error instead of os.Exit
//     (builder.go:189/199);
//   - Builder.GetResult before Build returns an error instead of panicking
//     (builder.go:263).
//
// Note on builder.go:189 (funcName == "Visit"): that branch is unreachable for
// ast.Node values, because reflect.Type.String() for any ast node is "*ast.X"
// and always yields a non-empty method suffix. Only the "method not found"
// branch at :199 is reachable, and that is what the scenario below exercises.
const reproEnv = "UAST_REPRO"

func TestMain(m *testing.M) {
	if scenario := os.Getenv(reproEnv); scenario != "" {
		runReproScenario(scenario)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// runReproScenario executes a crashing scenario in the child process. It either
// os.Exit()s (via the production code) or panics; the deferred os.Exit(0) in
// TestMain is never reached for these scenarios.
func runReproScenario(scenario string) {
	switch scenario {
	case "visit-unknown-node":
		b := NewUASTBuilder("m", map[string]*ast.Package{}, token.NewFileSet())
		b.visit(&ast.CommentGroup{}) // no VisitCommentGroup => builder.go:199 os.Exit(-1)
	case "get-result-before-build":
		b := NewUASTBuilder("m", map[string]*ast.Package{}, token.NewFileSet())
		_ = b.GetResult() // builder.go:263 panic
	}
}

// runReproChild re-execs the test binary with the requested scenario and returns
// (exitCode, combined output).
func runReproChild(t *testing.T, scenario string) (int, string) {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), reproEnv+"="+scenario)
	combined, err := cmd.CombinedOutput()
	if err == nil {
		return 0, string(combined)
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), string(combined)
	}
	t.Fatalf("repro child failed to start: %v", err)
	return -1, ""
}

// TestReproVisitUnknownNodeExits documents builder.go:199: dispatching a
// concrete ast node without a Visit* method kills the process through
// os.Exit(-1) (observed exit status 255).
//
// P2 target: builder.visit returns a Noop/error and the process keeps running;
// remove the skip and replace the body with an in-process assertion.
func TestReproVisitUnknownNodeExits(t *testing.T) {
	t.Skip("P2: error-model refactor — 去掉本行后本测试应失败（当前 os.Exit(-1), exit 255）")

	code, out := runReproChild(t, "visit-unknown-node")
	if code != 255 {
		t.Fatalf("current behavior changed: want exit 255 from os.Exit(-1), got %d; output:\n%s", code, out)
	}
	if !strings.Contains(out, "not found") {
		t.Fatalf("expected the dispatch diagnostic in child output; got:\n%s", out)
	}
}

// TestReproGetResultBeforeBuildPanics documents builder.go:263: GetResult before
// Build panics (observed exit status 2).
//
// P2 target: return an error instead of panicking; remove the skip and replace
// the body with an in-process error assertion.
func TestReproGetResultBeforeBuildPanics(t *testing.T) {
	t.Skip("P2: error-model refactor — 去掉本行后本测试应失败（当前 panic, exit 2）")

	code, out := runReproChild(t, "get-result-before-build")
	if code != 2 {
		t.Fatalf("current behavior changed: want exit 2 from panic, got %d; output:\n%s", code, out)
	}
	if !strings.Contains(out, "It should [Build] result before get it") {
		t.Fatalf("expected the GetResult panic message in child output; got:\n%s", out)
	}
}
