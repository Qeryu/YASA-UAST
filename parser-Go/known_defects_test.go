package main

import (
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"uast4go/api"
)

// TestParsePackageRandomSelection documents a known defect in the original
// implementation: ParsePackage picks a package from a Go map iteration, so a
// directory containing both `p` and `p_test` may randomly drop the main
// package. Decision for this change: keep the original behavior, record the
// defect and skip. Remove the skip once deterministic selection lands; the
// assertion body is the regression guard.
func TestParsePackageRandomSelection(t *testing.T) {
	t.Skip("known defect: 原版随机选包（map 迭代顺序），本次不修")

	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "p.go"), "package p\n\nconst Main = 1\n")
	mustWriteFile(t, filepath.Join(dir, "p_test.go"), "package p_test\n\nconst TestCase = 1\n")

	fset := token.NewFileSet()
	var firstPkg string
	var firstFiles []string
	for i := 0; i < 20; i++ {
		pkgName, files, _, err := api.ParsePackage(dir, fset)
		if err != nil {
			t.Fatalf("iteration %d: ParsePackage: %v", i, err)
		}
		names := make([]string, 0, len(files))
		for name := range files {
			names = append(names, name)
		}
		sort.Strings(names)
		if i == 0 {
			firstPkg, firstFiles = pkgName, names
			continue
		}
		if pkgName != firstPkg || !reflect.DeepEqual(names, firstFiles) {
			t.Fatalf("non-deterministic package selection at iteration %d: got %q %v, want %q %v (known defect)",
				i, pkgName, names, firstPkg, firstFiles)
		}
	}
}

// mustWriteFile writes content to path or fails the test. Shared by the
// package main test files.
func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
