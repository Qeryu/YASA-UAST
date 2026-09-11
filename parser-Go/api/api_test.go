package api

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRecoverBoundaryConvertsPanic verifies the library boundary turns a
// residual panic into a request-level error instead of crashing the caller.
func TestRecoverBoundaryConvertsPanic(t *testing.T) {
	var out []byte
	var errs []ParseError
	var err error

	func() {
		defer recoverBoundary("TestOp", "subject", &out, &errs, &err)
		panic("boom")
	}()

	if err == nil {
		t.Fatal("recoverBoundary: got nil error after panic")
	}
	if out != nil {
		t.Fatalf("recoverBoundary: out = %v, want nil", out)
	}
	if errs != nil {
		t.Fatalf("recoverBoundary: errs = %v, want nil", errs)
	}
}

// TestParseSingleFileBadSyntaxIsFileLevel locks the error classification: a
// syntax error yields a file-level ParseError with nil JSON and nil request
// error, so the CLI can name the file and exit non-zero without panicking.
func TestParseSingleFileBadSyntaxIsFileLevel(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.go")
	if err := os.WriteFile(bad, []byte("package p\n\nfunc broken( {\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	jsonBytes, errs, err := ParseSingleFile(bad)
	if err != nil {
		t.Fatalf("ParseSingleFile request error = %v, want nil", err)
	}
	if jsonBytes != nil {
		t.Fatalf("ParseSingleFile returned %d JSON bytes, want nil", len(jsonBytes))
	}
	if len(errs) != 1 {
		t.Fatalf("ParseSingleFile errs = %d, want 1: %+v", len(errs), errs)
	}
	if errs[0].File != bad {
		t.Fatalf("ParseError.File = %q, want %q", errs[0].File, bad)
	}
}
