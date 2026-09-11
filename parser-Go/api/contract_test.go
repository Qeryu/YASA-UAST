package api

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
)

var tmpNRe = regexp.MustCompile(`tmp[0-9]+`)

func normalizeTmpN(s string) string {
	return tmpNRe.ReplaceAllString(s, "tmpX")
}

// TestParseSourceMatchesSingleCLI is the P3 prerequisite contract: the in-memory
// core parseSource(name, src) must produce byte-identical JSON to the `-single`
// CLI path (which is now read-file + delegate to the same core). This pins the
// serialization (json.Encoder, trailing newline) and the loc/sourcefile naming
// before the wasm resident API starts consuming parseSource.
func TestParseSourceMatchesSingleCLI(t *testing.T) {
	const pgDir = ".."
	examples := []string{
		"examples/compositeLit.go",
		"examples/method.go",
		"examples/imports.go",
		"examples/struct_promotion.go",
		"examples/typeAsserts.go",
		"examples/select.go",
	}
	if len(examples) < 5 {
		t.Fatalf("need >= 5 coverage examples, have %d", len(examples))
	}

	cli := buildContractCLI(t, pgDir)

	for _, rel := range examples {
		rel := rel
		t.Run(rel, func(t *testing.T) {
			// CLI path: path is used verbatim as sourcefile.
			out := filepath.Join(t.TempDir(), "cli.json")
			cmd := exec.Command(cli, "-single", "-rootDir="+rel, "-output="+out)
			cmd.Dir = pgDir
			if combined, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("CLI -single %s failed: %v\n%s", rel, err, combined)
			}
			cliBytes, err := os.ReadFile(out)
			if err != nil {
				t.Fatalf("read CLI output: %v", err)
			}

			// In-memory path.
			src, err := os.ReadFile(filepath.Join(pgDir, rel))
			if err != nil {
				t.Fatalf("read source: %v", err)
			}
			memBytes, errs, perr := parseSource(rel, src)
			if perr != nil {
				t.Fatalf("parseSource request error: %v", perr)
			}
			if len(errs) != 0 {
				t.Fatalf("parseSource errs = %+v, want none", errs)
			}

			if !bytes.Equal(cliBytes, memBytes) {
				t.Fatalf("in-memory output differs from CLI for %s\nCLI (%d bytes): %q\nmem (%d bytes): %q",
					rel, len(cliBytes), cliBytes, len(memBytes), memBytes)
			}
		})
	}
}

// TestParseSourcesMatchesProjectCLI is the in-memory project contract: for a
// single-package fixture, ParseSources(files) must match the CLI -rootDir output
// after tmpN normalization (cross-file order inside a package is map-ordered).
func TestParseSourcesMatchesProjectCLI(t *testing.T) {
	const pgDir = ".."
	dir := t.TempDir()

	goMod := "module fixture\n\ngo 1.22\n"
	aGo := "package fixture\n\ntype Point struct {\n\tX int\n\tY int\n}\n\nfunc A() Point { return Point{X: 1, Y: 2} }\n"
	bGo := "package fixture\n\nfunc B(s string) string { return s + \"!\" }\n"
	for name, content := range map[string]string{"go.mod": goMod, "a.go": aGo, "b.go": bGo} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cli := buildContractCLI(t, pgDir)
	out := filepath.Join(dir, "cli.json")
	cmd := exec.Command(cli, "-rootDir="+dir, "-output="+out)
	cmd.Dir = pgDir
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("CLI -rootDir failed: %v\n%s", err, combined)
	}
	cliBytes, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read CLI output: %v", err)
	}

	memBytes, errs, perr := ParseSources([]SourceFile{
		{Name: filepath.Join(dir, "go.mod"), Content: []byte(goMod)},
		{Name: filepath.Join(dir, "a.go"), Content: []byte(aGo)},
		{Name: filepath.Join(dir, "b.go"), Content: []byte(bGo)},
	})
	if perr != nil {
		t.Fatalf("ParseSources request error: %v", perr)
	}
	if len(errs) != 0 {
		t.Fatalf("ParseSources errs = %+v, want none", errs)
	}

	cliNorm := normalizeTmpN(string(cliBytes))
	memNorm := normalizeTmpN(string(memBytes))
	if cliNorm != memNorm {
		t.Fatalf("ParseSources differs from CLI (-rootDir) for the fixture\nCLI:\n%s\nmem:\n%s", cliNorm, memNorm)
	}
}

func buildContractCLI(t *testing.T, dir string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "uast4go")
	cmd := exec.Command("go", "build", "-buildvcs=false", "-o", bin, ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, out)
	}
	return bin
}

func writeContractFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sourcesFromTree(t *testing.T, root string) []SourceFile {
	t.Helper()
	var files []SourceFile
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		content, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		files = append(files, SourceFile{Name: path, Content: content})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func mustRunProjectCLI(t *testing.T, cli, rootDir string) []byte {
	t.Helper()
	out := filepath.Join(t.TempDir(), "cli.json")
	cmd := exec.Command(cli, "-rootDir="+rootDir, "-output="+out)
	cmd.Dir = ".."
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("CLI -rootDir=%s failed: %v\n%s", rootDir, err, combined)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read CLI output: %v", err)
	}
	return b
}

// TestParseSourcesDotDirs locks the CLI's filepath.Walk pruning semantics:
// a dot directory is pruned (with its subtree) only when it directly contains a
// .go file; a dot directory with no direct .go files is descended into and its
// descendants are parsed.
func TestParseSourcesDotDirs(t *testing.T) {
	cli := buildContractCLI(t, "..")

	t.Run("dot-dir-with-direct-go-prunes-subtree", func(t *testing.T) {
		dir := t.TempDir()
		writeContractFile(t, filepath.Join(dir, "go.mod"), "module fixture\n\ngo 1.22\n")
		writeContractFile(t, filepath.Join(dir, "keep.go"), "package fixture\n\nfunc Keep() {}\n")
		writeContractFile(t, filepath.Join(dir, ".hidden", "a.go"), "package hidden\n\nfunc A() {}\n")
		writeContractFile(t, filepath.Join(dir, ".hidden", "sub", "x.go"), "package sub\n\nfunc X() {}\n")

		cliBytes := mustRunProjectCLI(t, cli, dir)
		memBytes, errs, perr := ParseSources(sourcesFromTree(t, dir))
		if perr != nil {
			t.Fatalf("ParseSources: %v", perr)
		}
		if len(errs) != 0 {
			t.Fatalf("ParseSources errs = %+v, want none", errs)
		}
		got := string(memBytes)
		if !bytes.Contains(memBytes, []byte("keep.go")) {
			t.Fatalf("keep.go missing from memory output:\n%s", got)
		}
		if bytes.Contains(memBytes, []byte(".hidden")) {
			t.Fatalf("pruned dot-dir leaked into memory output:\n%s", got)
		}
		if normalizeTmpN(string(cliBytes)) != normalizeTmpN(got) {
			t.Fatalf("memory differs from CLI\nCLI:\n%s\nmem:\n%s", normalizeTmpN(string(cliBytes)), normalizeTmpN(got))
		}
	})

	t.Run("dot-dir-without-direct-go-is-descended", func(t *testing.T) {
		dir := t.TempDir()
		writeContractFile(t, filepath.Join(dir, "go.mod"), "module fixture\n\ngo 1.22\n")
		writeContractFile(t, filepath.Join(dir, "keep.go"), "package fixture\n\nfunc Keep() {}\n")
		writeContractFile(t, filepath.Join(dir, ".hidden", "sub", "x.go"), "package sub\n\nfunc X() {}\n")

		cliBytes := mustRunProjectCLI(t, cli, dir)
		memBytes, errs, perr := ParseSources(sourcesFromTree(t, dir))
		if perr != nil {
			t.Fatalf("ParseSources: %v", perr)
		}
		if len(errs) != 0 {
			t.Fatalf("ParseSources errs = %+v, want none", errs)
		}
		got := string(memBytes)
		if !bytes.Contains(memBytes, []byte(".hidden")) {
			t.Fatalf("descendant of dot-dir should be parsed:\n%s", got)
		}
		if normalizeTmpN(string(cliBytes)) != normalizeTmpN(got) {
			t.Fatalf("memory differs from CLI\nCLI:\n%s\nmem:\n%s", normalizeTmpN(string(cliBytes)), normalizeTmpN(got))
		}
	})
}

// TestParseSourcesExplicitRoot verifies the optional root overrides the
// common-ancestor heuristic and matches the CLI -rootDir value (here the
// parent of the module dir, so package paths become /mod and /mod/sub).
func TestParseSourcesExplicitRoot(t *testing.T) {
	cli := buildContractCLI(t, "..")
	base := t.TempDir()
	modDir := filepath.Join(base, "mod")
	writeContractFile(t, filepath.Join(modDir, "go.mod"), "module fixture\n\ngo 1.22\n")
	writeContractFile(t, filepath.Join(modDir, "a.go"), "package fixture\n\nfunc A() {}\n")
	writeContractFile(t, filepath.Join(modDir, "sub", "b.go"), "package sub\n\nfunc B() {}\n")

	cliBytes := mustRunProjectCLI(t, cli, base) // -rootDir = parent
	memBytes, errs, perr := ParseSources(sourcesFromTree(t, base), base)
	if perr != nil {
		t.Fatalf("ParseSources: %v", perr)
	}
	if len(errs) != 0 {
		t.Fatalf("ParseSources errs = %+v, want none", errs)
	}
	if normalizeTmpN(string(cliBytes)) != normalizeTmpN(string(memBytes)) {
		t.Fatalf("explicit root differs from CLI -rootDir=%s\nCLI:\n%s\nmem:\n%s",
			base, normalizeTmpN(string(cliBytes)), normalizeTmpN(string(memBytes)))
	}
	// Explicit root must widen package paths beyond the common ancestor.
	if !bytes.Contains(memBytes, []byte("/mod")) {
		t.Fatalf("explicit root not applied; no /mod package path in:\n%s", string(memBytes))
	}
}
