package main

import (
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

// Batch B — error-path tests written against the P2 target behavior:
// parser entry points return errors / skip the bad input, and never panic or
// os.Exit. Every case is skipped in P1 and turns green after the M1 error-model
// refactor (docs/uastgo-wasm-plan.md §3.4). The assertion bodies are kept so the
// tests become real once the signatures allow an error to be observed.

// TestBadSyntaxFileReturnsError: a syntactically invalid file must not crash
// the process (today parseSingleFile panics via parser.ParseFile).
func TestBadSyntaxFileReturnsError(t *testing.T) {
	t.Skip("P2: error-model refactor")

	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.go")
	mustWriteFile(t, bad, "package p\n\nfunc broken( {\n")
	out := filepath.Join(dir, "out.json")

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("parseSingleFile panicked on bad syntax: %v", r)
		}
	}()
	parseSingleFile(bad, out)
	// Target: parseSingleFile returns a structured error (signature change in P2).
}

// TestEmptyDirReturnsError: a directory without any .go file is a request-level
// failure and should surface an error, not an empty output.
func TestEmptyDirReturnsError(t *testing.T) {
	t.Skip("P2: error-model refactor")

	dir := t.TempDir()
	if _, _, err := parsePackage(dir, token.NewFileSet()); err == nil {
		t.Fatalf("expected error for empty dir %s, got nil", dir)
	}
}

// TestNoGoModIsReported: findGoMod failure currently falls back to
// "__unknown_module__"; the request should be rejected explicitly instead.
func TestNoGoModIsReported(t *testing.T) {
	t.Skip("P2: error-model refactor")

	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.go"), "package p\n")
	paths, err := findAllGoMod(dir)
	if err == nil {
		t.Fatalf("expected error for directory without go.mod, got paths=%v", paths)
	}
}

// TestMultipleGoModDetected: several go.mod files currently pick goModPaths[0];
// after P2 the selection must be deterministic or reported.
func TestMultipleGoModDetected(t *testing.T) {
	t.Skip("P2: error-model refactor")

	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "go.mod"), "module root\n")
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(sub, "go.mod"), "module sub\n")

	paths, err := findAllGoMod(dir)
	if err != nil {
		t.Fatalf("findAllGoMod: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 go.mod paths, got %d: %v", len(paths), paths)
	}
}

// TestVendorDirSkipped: vendor/ must be skipped rather than parsed.
func TestVendorDirSkipped(t *testing.T) {
	t.Skip("P2: error-model refactor")

	dir := filepath.Join(t.TempDir(), "vendor")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := findAllGoMod(dir); err == nil {
		t.Fatal("expected vendor dir to be skipped/reported, got nil error")
	}
}

// TestUnknownNodeDoesNotExit: builder.visit currently calls os.Exit(-1) for a
// concrete ast node without a Visit method. The dispatch lives in package uast,
// so the real P2 assertion belongs in uast/ and must check that an unregistered
// node yields an error / Noop instead of terminating the process.
func TestUnknownNodeDoesNotExit(t *testing.T) {
	t.Skip("P2: error-model refactor")
}
