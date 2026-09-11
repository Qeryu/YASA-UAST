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

// TestParseSourceMatchesSingleCLI 是 P3 的前置契约：内存核心
// parseSource(name, src) 必须产出与 `-single` CLI 路径（现为读文件 + 委派到同一
// 核心）逐字节一致的 JSON。它在 wasm 常驻 API 开始消费 parseSource 之前，锁定
// 序列化（json.Encoder、末尾换行）与 loc/sourcefile 命名。
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
			// CLI 路径：path 原样用作 sourcefile。
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

			// 内存路径。
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

// TestParseSourcesMatchesProjectCLI 是内存项目契约：对单 package fixture，
// ParseSources(files) 在 tmpN 归一化后必须与 CLI `-rootDir` 输出一致（同一
// package 内的跨文件顺序由 map 决定）。
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

// TestParseSourcesDotDirs 锁定 CLI 的 filepath.Walk 剪枝语义：dot 目录仅当
// **直接包含 .go 文件**时才连同子树被剪掉；没有直接 .go 文件的 dot 目录会被
// 下钻，其子孙会被解析。
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

// TestParseSourcesExplicitRoot 验证可选 root 覆盖共同祖先启发式，并与 CLI
// `-rootDir` 值一致（这里取 module 目录的父目录，因此 package 路径变为 /mod
// 与 /mod/sub）。
func TestParseSourcesExplicitRoot(t *testing.T) {
	cli := buildContractCLI(t, "..")
	base := t.TempDir()
	modDir := filepath.Join(base, "mod")
	writeContractFile(t, filepath.Join(modDir, "go.mod"), "module fixture\n\ngo 1.22\n")
	writeContractFile(t, filepath.Join(modDir, "a.go"), "package fixture\n\nfunc A() {}\n")
	writeContractFile(t, filepath.Join(modDir, "sub", "b.go"), "package sub\n\nfunc B() {}\n")

	cliBytes := mustRunProjectCLI(t, cli, base) // -rootDir = 父目录
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
	// 显式 root 必须把 package 路径扩展到共同祖先之外。
	if !bytes.Contains(memBytes, []byte("/mod")) {
		t.Fatalf("explicit root not applied; no /mod package path in:\n%s", string(memBytes))
	}
}
