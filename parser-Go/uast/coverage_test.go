package uast

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// TestVisitCoverageReport is report-only (it never fails): it compares the
// Visit* methods implemented on *Builder against the concrete go/ast node types
// actually present in examples/. Two lists are logged:
//
//	① concrete node types present in the corpus that have no Visit method
//	   (dispatching them would currently hit os.Exit(-1) in builder.visit);
//	② Visit methods that the corpus never triggers.
//
// Note: ① is an over-approximation — some corpus nodes (e.g. *ast.File,
// *ast.Field) are handled structurally and are never passed to builder.visit.
func TestVisitCoverageReport(t *testing.T) {
	visitMethods := map[string]bool{}
	bt := reflect.TypeOf(&Builder{})
	for i := 0; i < bt.NumMethod(); i++ {
		name := bt.Method(i).Name
		if strings.HasPrefix(name, "Visit") && name != "Visit" {
			visitMethods[name] = true
		}
	}

	_, thisFile, _, _ := runtime.Caller(0)
	examplesDir := filepath.Join(filepath.Dir(thisFile), "..", "examples")

	present := map[string]bool{}
	fset := token.NewFileSet()
	err := filepath.Walk(examplesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			t.Errorf("parse %s: %v", path, perr)
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil {
				return true
			}
			present[visitMethodFor(n)] = true
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", examplesDir, err)
	}

	var missing, unused []string
	for m := range present {
		if !visitMethods[m] {
			missing = append(missing, m)
		}
	}
	for m := range visitMethods {
		if !present[m] {
			unused = append(unused, m)
		}
	}
	sort.Strings(missing)
	sort.Strings(unused)

	t.Logf("corpus: %s", examplesDir)
	t.Logf("① node types present in examples but WITHOUT a Visit method (would os.Exit(-1) if dispatched): %v", missing)
	t.Logf("② Visit methods never triggered by the examples corpus: %v", unused)
}

func visitMethodFor(n ast.Node) string {
	s := reflect.TypeOf(n).String() // e.g. "*ast.Ident"
	if i := strings.LastIndex(s, "."); i >= 0 {
		s = s[i+1:]
	}
	return "Visit" + s
}
