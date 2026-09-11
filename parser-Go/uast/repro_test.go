package uast

import (
	"go/ast"
	"go/token"
	"testing"
)

// These tests replace the P1 re-exec repro harness. In P1 the two paths below
// terminated the process (os.Exit(-1) / panic), so they had to be asserted in a
// child process — see the P1 baseline commit "add re-exec repro". P2a turns
// them into in-process assertions: the builder now records a file-level error
// and continues, and GetResult returns an error.
//
// Note on builder.go:189 (funcName == "Visit"): that branch is unreachable for
// ast.Node values, because reflect.Type.String() for any ast node is "*ast.X"
// and always yields a non-empty method suffix. Only the "method not found"
// branch (:199) is reachable, and that is what the first test exercises.

// TestVisitUnknownNodeRecordsError verifies that dispatching a concrete ast node
// without a Visit* method no longer exits the process: it records a file-level
// error and degrades to *Noop.
func TestVisitUnknownNodeRecordsError(t *testing.T) {
	b := NewUASTBuilder("m", map[string]*ast.Package{}, token.NewFileSet())
	b.currentFile = "unknown.go"

	got := b.visit(&ast.CommentGroup{}) // no VisitCommentGroup
	if _, ok := got.(*Noop); !ok {
		t.Fatalf("visit returned %T, want *Noop (safe degradation)", got)
	}

	errs := b.Errors()
	if len(errs) != 1 {
		t.Fatalf("Errors() = %d entries, want 1: %+v", len(errs), errs)
	}
	if errs[0].File != "unknown.go" {
		t.Fatalf("error file = %q, want unknown.go", errs[0].File)
	}
	if errs[0].Message == "" {
		t.Fatal("error message should not be empty")
	}
}

// TestGetResultBeforeBuildReturnsError verifies GetResult no longer panics
// before Build; it returns a nil result and an error.
func TestGetResultBeforeBuildReturnsError(t *testing.T) {
	b := NewUASTBuilder("m", map[string]*ast.Package{}, token.NewFileSet())

	res, err := b.GetResult()
	if err == nil {
		t.Fatal("GetResult before Build: got nil error, want error")
	}
	if res != nil {
		t.Fatalf("GetResult before Build: got %#v, want nil result", res)
	}
}
