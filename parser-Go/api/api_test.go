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
// syntax error yields a file-level error/parse_error with nil JSON and nil
// request error, so the CLI can name the file and exit non-zero without
// panicking.
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
	if errs[0].Severity != SeverityError || errs[0].Kind != KindParseError {
		t.Fatalf("ParseError = %s/%s, want error/parse_error", errs[0].Severity, errs[0].Kind)
	}
}

// TestParseSingleFileMissingIsFileLevel: an unreadable path is a file-level
// error/read_error (exit-1 semantics), recorded so S4's asymmetry with project
// rootDir-missing (exit 2) is explicit rather than accidental.
func TestParseSingleFileMissingIsFileLevel(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.go")

	jsonBytes, errs, err := ParseSingleFile(missing)
	if err != nil {
		t.Fatalf("ParseSingleFile request error = %v, want nil", err)
	}
	if jsonBytes != nil {
		t.Fatalf("ParseSingleFile returned %d JSON bytes, want nil", len(jsonBytes))
	}
	if len(errs) != 1 {
		t.Fatalf("ParseSingleFile errs = %d, want 1: %+v", len(errs), errs)
	}
	if errs[0].Severity != SeverityError || errs[0].Kind != KindReadError {
		t.Fatalf("ParseError = %s/%s, want error/read_error", errs[0].Severity, errs[0].Kind)
	}
}

// TestHasErrors checks which severities block single-file product emission.
func TestHasErrors(t *testing.T) {
	if HasErrors(nil) {
		t.Fatal("HasErrors(nil) = true, want false")
	}
	if !HasErrors([]ParseError{{Severity: SeverityError}}) {
		t.Fatal("HasErrors([error]) = false, want true")
	}
	if HasErrors([]ParseError{{Severity: SeverityWarning}}) {
		t.Fatal("HasErrors([warning]) = true, want false")
	}
}
