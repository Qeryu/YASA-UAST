// Package api is the importable library entry point for the Go UAST parser.
//
// Error semantics (D11):
//   - file-level failures (a single .go file that does not parse) are recorded
//     as ParseError and the build continues with the remaining files;
//   - request-level failures (unreadable rootDir, no packages at all, ...) are
//     returned as a plain error and no JSON is produced;
//   - the library never calls os.Exit and never panics: the exported entry
//     points install a recover() boundary that converts a residual panic into a
//     request-level error.
//
// On success the returned JSON is byte-identical to the historical CLI output
// (json.Encoder.Encode, including the trailing newline).
package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"uast4go/uast"
)

// ParseError is a file-level (recoverable) error. It is an alias of
// uast.ParseError so callers of the library do not need to import uast.
type ParseError = uast.ParseError

// Output is the top-level JSON document. Its shape and field order are frozen
// for CLI compatibility.
type Output struct {
	PackageInfo *uast.PackagePathInfo `json:"packageInfo"`
	ModuleName  string                `json:"moduleName"`
	GoModPath   string                `json:"goModPath"`
	NumOfGoMod  int                   `json:"numOfGoMod"`
}

// ParseSingleFile parses one Go file and returns the encoded JSON document.
//
// A syntax error in the file is reported in errs (file-level) with nil JSON;
// err is reserved for request-level/panic failures.
func ParseSingleFile(path string) (jsonBytes []byte, errs []ParseError, err error) {
	defer recoverBoundary("ParseSingleFile", path, &jsonBytes, &errs, &err)

	fset := token.NewFileSet()
	f, perr := parser.ParseFile(fset, path, nil, parser.DeclarationErrors)
	if perr != nil {
		return nil, []ParseError{{File: path, Message: perr.Error()}}, nil
	}
	pkg := &ast.Package{
		Name:    "__single__",
		Scope:   nil,
		Imports: nil,
		Files:   make(map[string]*ast.File),
	}
	pkg.Files[path] = f
	packages := map[string]*ast.Package{"__single__": pkg}

	packageInfo, buildErrs := buildPackage("__single_module__", packages, fset)
	out := &Output{
		PackageInfo: packageInfo,
		ModuleName:  "__single_module__",
	}
	b, eerr := encodeOutput(out)
	if eerr != nil {
		return nil, buildErrs, eerr
	}
	return b, buildErrs, nil
}

// ParseProject parses every package under rootDir and returns the encoded JSON
// document. Individual unparseable files do not abort the scan: they are
// reported in errs and the remaining files are still emitted. A missing go.mod
// is reported as a file-level warning and the historical
// "__unknown_module__" fallback is kept.
func ParseProject(rootDir string) (jsonBytes []byte, errs []ParseError, err error) {
	defer recoverBoundary("ParseProject", rootDir, &jsonBytes, &errs, &err)

	fset := token.NewFileSet()

	goModPaths, gerr := findAllGoMod(rootDir)
	moduleName := "__unknown_module__"
	if gerr != nil {
		errs = append(errs, ParseError{File: rootDir, Message: gerr.Error()})
	} else {
		name, rerr := readModuleName(goModPaths[0])
		if rerr != nil {
			errs = append(errs, ParseError{File: goModPaths[0], Message: rerr.Error()})
		}
		moduleName = name
	}

	packages, pkgErrs, perr := preparePackage(rootDir, fset)
	errs = append(errs, pkgErrs...)
	if perr != nil {
		return nil, errs, fmt.Errorf("prepare packages under %s: %w", rootDir, perr)
	}
	if len(packages) == 0 {
		return nil, errs, fmt.Errorf("no packages found under %s", rootDir)
	}

	// 默认取找到的第一个 go.mod
	firstGoModPath := ""
	if goModPaths != nil {
		firstGoModPath = goModPaths[0]
	}
	packageInfo, buildErrs := buildPackage(moduleName, packages, fset)
	errs = append(errs, buildErrs...)

	out := &Output{
		PackageInfo: packageInfo,
		ModuleName:  moduleName,
		GoModPath:   firstGoModPath,
		NumOfGoMod:  len(goModPaths),
	}
	b, eerr := encodeOutput(out)
	if eerr != nil {
		return nil, errs, eerr
	}
	return b, errs, nil
}

// ParsePackage parses the .go files directly under dir, one file at a time so a
// bad file is recorded and does not discard the good ones. For a directory with
// several package names the returned package is chosen by map iteration order;
// this preserves the original (known, not-to-be-fixed) behavior O1.
func ParsePackage(dir string, fset *token.FileSet) (packageName string, files map[string]*ast.File, errs []ParseError, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", nil, nil, err
	}
	pkgs := make(map[string]map[string]*ast.File)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			errs = append(errs, ParseError{File: path, Message: perr.Error()})
			continue
		}
		name := f.Name.Name
		if pkgs[name] == nil {
			pkgs[name] = make(map[string]*ast.File)
		}
		pkgs[name][path] = f
	}
	for name, fs := range pkgs {
		return name, fs, errs, nil
	}
	return "", nil, errs, fmt.Errorf("no packages found in directory %s", dir)
}

func preparePackage(rootDir string, fset *token.FileSet) (map[string]*ast.Package, []ParseError, error) {
	packages := make(map[string]*ast.Package)
	var errs []ParseError

	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !info.IsDir() || !ContainsGoFiles(path) || strings.Contains(path, "/vendor") {
			return nil
		}
		if strings.HasPrefix(info.Name(), ".") {
			return filepath.SkipDir
		}

		packageName, files, perrs, perr := ParsePackage(path, fset)
		errs = append(errs, perrs...)
		if perr != nil {
			// 文件级：记录并继续处理其它目录，不再中止整次扫描。
			errs = append(errs, ParseError{File: path, Message: perr.Error()})
			return nil
		}

		relativePath, _ := filepath.Rel(rootDir, path)
		packagePath := filepath.Join("/", relativePath)
		if !strings.HasPrefix(packagePath, "/vendor") {
			packages[packagePath] = &ast.Package{
				Name:  packageName,
				Files: files,
			}
		}
		return nil
	})
	if err != nil {
		return nil, errs, err
	}
	return packages, errs, nil
}

// buildPackage runs the UAST builder; file-level errors collected by the
// builder are returned alongside the result.
func buildPackage(moduleName string, packages map[string]*ast.Package, fset *token.FileSet) (*uast.PackagePathInfo, []ParseError) {
	b := uast.NewUASTBuilder(moduleName, packages, fset)
	b.Build()
	res, err := b.GetResult()
	if err != nil {
		return nil, append(b.Errors(), uast.ParseError{Message: err.Error()})
	}
	return res, b.Errors()
}

func encodeOutput(out *Output) ([]byte, error) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// recoverBoundary converts a residual panic at the library boundary into a
// request-level error, so callers never observe a process crash.
func recoverBoundary(op, subject string, out *[]byte, errs *[]ParseError, err *error) {
	if r := recover(); r != nil {
		*out = nil
		*errs = nil
		*err = fmt.Errorf("uast: %s(%s): recovered panic: %v", op, subject, r)
	}
}

// findAllGoMod searches for go.mod files under dir (original behavior).
func findAllGoMod(dir string) ([]string, error) {
	if strings.Contains(dir, "/vendor") {
		return nil, fmt.Errorf("find vendor")
	}
	const goModFileName = "go.mod"

	var paths []string

	goModPath := filepath.Join(dir, goModFileName)
	if _, err := os.Stat(goModPath); err == nil {
		paths = append(paths, goModPath)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			subDir := filepath.Join(dir, entry.Name())
			subPaths, err := findAllGoMod(subDir)
			if err == nil && len(subPaths) > 0 {
				paths = append(paths, subPaths...)
			}
		}
	}
	if paths != nil {
		return paths, nil
	}
	return nil, fmt.Errorf("not found go.mod")
}

// readModuleName reads the module name from the go.mod file (original behavior).
func readModuleName(modFilePath string) (string, error) {
	content, err := os.ReadFile(modFilePath)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "module") {
			fields := strings.Fields(line)
			if len(fields) == 2 {
				return fields[1], nil
			}
		}
	}
	return "", fmt.Errorf("module directive not found in %s", modFilePath)
}

// ContainsGoFiles reports whether dir directly contains a .go file.
func ContainsGoFiles(dir string) bool {
	list, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, d := range list {
		if strings.HasSuffix(d.Name(), ".go") {
			return true
		}
	}
	return false
}
