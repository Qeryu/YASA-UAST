package api

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

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
