// Package api is the importable library entry point for the Go UAST parser.
//
// Error semantics (D11):
//   - file-level failures are recorded as ParseError and the build continues;
//     each entry carries a Severity (warning/error) and a Kind;
//   - request-level failures (unreadable rootDir, no packages at all, ...) are
//     returned as a plain error and no JSON is produced;
//   - the library never calls os.Exit and never panics: the exported entry
//     points install a recover() boundary that converts a residual panic into a
//     request-level error.
//
// P3 long-lived core: parseSource takes the source bytes in memory (a
// synchronous wasm callback cannot perform Go file I/O, D8). ParseSingleFile is
// read-file + delegate.
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
	"sort"
	"strings"

	"uast4go/uast"
)

// ParseError is a file-level (recoverable) entry. It is an alias of
// uast.ParseError so callers of the library do not need to import uast.
type ParseError = uast.ParseError

// Severity and Kind are aliases of the uast definitions.
type (
	Severity = uast.Severity
	Kind     = uast.Kind
)

const (
	SeverityWarning = uast.SeverityWarning
	SeverityError   = uast.SeverityError

	KindParseError      = uast.KindParseError
	KindReadError       = uast.KindReadError
	KindUnsupportedNode = uast.KindUnsupportedNode
	KindNoGoMod         = uast.KindNoGoMod
	KindNoPackages      = uast.KindNoPackages
)

// Output is the top-level JSON document. Its shape and field order are frozen
// for CLI compatibility.
type Output struct {
	PackageInfo *uast.PackagePathInfo `json:"packageInfo"`
	ModuleName  string                `json:"moduleName"`
	GoModPath   string                `json:"goModPath"`
	NumOfGoMod  int                   `json:"numOfGoMod"`
}

// HasErrors reports whether errs contains at least one error-severity entry.
// Warning-severity entries are informational and do not block product output.
func HasErrors(errs []ParseError) bool {
	for _, e := range errs {
		if e.Severity == SeverityError {
			return true
		}
	}
	return false
}

// parseSource builds the UAST for one in-memory Go source file. name is used
// verbatim as the filename in loc/sourcefile, so passing the same name as the
// CLI's -rootDir value yields byte-identical JSON.
//
// A syntax error is reported in errs (error/parse_error) with nil JSON; err is
// reserved for request-level/panic failures.
func parseSource(name string, src []byte) (jsonBytes []byte, errs []ParseError, err error) {
	defer recoverBoundary("parseSource", name, &jsonBytes, &errs, &err)

	fset := token.NewFileSet()
	f, perr := parser.ParseFile(fset, name, src, parser.DeclarationErrors)
	if perr != nil {
		return nil, []ParseError{{
			File:     name,
			Message:  perr.Error(),
			Severity: SeverityError,
			Kind:     KindParseError,
		}}, nil
	}
	pkg := &ast.Package{
		Name:    "__single__",
		Scope:   nil,
		Imports: nil,
		Files:   make(map[string]*ast.File),
	}
	pkg.Files[name] = f
	packages := map[string]*ast.Package{"__single__": pkg}

	packageInfo, buildErrs, berr := buildPackage("__single_module__", packages, fset)
	if berr != nil {
		return nil, buildErrs, berr
	}
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

// ParseSource is the exported in-memory single-file entry point used by the
// wasm/npm resident parser (P3). name is used verbatim as loc.sourcefile.
func ParseSource(name string, src []byte) (jsonBytes []byte, errs []ParseError, err error) {
	return parseSource(name, src)
}

// ParseSingleFile reads path and delegates to parseSource. The read is the only
// filesystem access; a read failure is reported as a file-level
// error/read_error (not a request-level error), matching the single-file
// parse-failure exit semantics (exit 1, no product).
func ParseSingleFile(path string) (jsonBytes []byte, errs []ParseError, err error) {
	defer recoverBoundary("ParseSingleFile", path, &jsonBytes, &errs, &err)

	src, rerr := os.ReadFile(path)
	if rerr != nil {
		return nil, []ParseError{{
			File:     path,
			Message:  rerr.Error(),
			Severity: SeverityError,
			Kind:     KindReadError,
		}}, nil
	}
	return parseSource(path, src)
}

// ParseProject parses every package under rootDir and returns the encoded JSON
// document. Individual unparseable files do not abort the scan: they are
// reported in errs (error/parse_error) and the remaining files are still
// emitted. A missing go.mod is reported as a warning (warning/no_gomod) and the
// historical "__unknown_module__" fallback is kept.
func ParseProject(rootDir string) (jsonBytes []byte, errs []ParseError, err error) {
	defer recoverBoundary("ParseProject", rootDir, &jsonBytes, &errs, &err)

	fset := token.NewFileSet()

	goModPaths, gerr := findAllGoMod(rootDir)
	moduleName := "__unknown_module__"
	if gerr != nil {
		errs = append(errs, ParseError{
			File:     rootDir,
			Message:  gerr.Error(),
			Severity: SeverityWarning,
			Kind:     KindNoGoMod,
		})
	} else {
		name, rerr := readModuleName(goModPaths[0])
		if rerr != nil {
			errs = append(errs, ParseError{
				File:     goModPaths[0],
				Message:  rerr.Error(),
				Severity: SeverityWarning,
				Kind:     KindNoGoMod,
			})
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
	packageInfo, buildErrs, berr := buildPackage(moduleName, packages, fset)
	errs = append(errs, buildErrs...)
	if berr != nil {
		return nil, errs, berr
	}

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

// SourceFile is an in-memory source file for ParseSources.
type SourceFile struct {
	Name    string
	Content []byte
}

// ParseSources is the exported in-memory project-mode entry point (P3). It
// mirrors the CLI -rootDir behavior for the same logical tree:
//   - the virtual root is the common ancestor directory of the file names;
//   - package paths are "/"+relative-dir (matching preparePackage);
//   - the module name comes from the shallowest provided go.mod;
//   - a missing go.mod is a warning/no_gomod and keeps __unknown_module__.
//
// A bad file is a file-level error/parse_error and does not discard the good
// files. Output is identical to the CLI for equivalent input after tmpN
// normalization (cross-file order inside a package follows Go map iteration).
func ParseSources(files []SourceFile) (jsonBytes []byte, errs []ParseError, err error) {
	defer recoverBoundary("ParseSources", "<memory>", &jsonBytes, &errs, &err)

	if len(files) == 0 {
		return nil, nil, fmt.Errorf("no source files provided")
	}
	root := commonRoot(files)
	fset := token.NewFileSet()

	var goModPaths []string
	goModContent := make(map[string][]byte)
	for _, f := range files {
		if filepath.Base(f.Name) == "go.mod" {
			goModPaths = append(goModPaths, f.Name)
			goModContent[f.Name] = f.Content
		}
	}
	sort.Slice(goModPaths, func(i, j int) bool {
		di, dj := pathDepth(goModPaths[i]), pathDepth(goModPaths[j])
		if di != dj {
			return di < dj
		}
		return goModPaths[i] < goModPaths[j]
	})

	moduleName := "__unknown_module__"
	if len(goModPaths) == 0 {
		errs = append(errs, ParseError{
			File:     root,
			Message:  "not found go.mod",
			Severity: SeverityWarning,
			Kind:     KindNoGoMod,
		})
	} else {
		name, rerr := moduleNameFromContent(goModContent[goModPaths[0]], goModPaths[0])
		if rerr != nil {
			errs = append(errs, ParseError{
				File:     goModPaths[0],
				Message:  rerr.Error(),
				Severity: SeverityWarning,
				Kind:     KindNoGoMod,
			})
		}
		moduleName = name
	}

	packages, pkgErrs := preparePackagesFromSources(root, files, fset)
	errs = append(errs, pkgErrs...)
	if len(packages) == 0 {
		return nil, errs, fmt.Errorf("no packages found under %s", root)
	}

	firstGoModPath := ""
	if len(goModPaths) > 0 {
		firstGoModPath = goModPaths[0]
	}
	packageInfo, buildErrs, berr := buildPackage(moduleName, packages, fset)
	errs = append(errs, buildErrs...)
	if berr != nil {
		return nil, errs, berr
	}

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
			errs = append(errs, ParseError{
				File:     path,
				Message:  perr.Error(),
				Severity: SeverityError,
				Kind:     KindParseError,
			})
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
		if !info.IsDir() || !containsGoFiles(path) || strings.Contains(path, "/vendor") {
			return nil
		}
		if strings.HasPrefix(info.Name(), ".") {
			return filepath.SkipDir
		}

		packageName, files, perrs, perr := ParsePackage(path, fset)
		errs = append(errs, perrs...)
		if perr != nil {
			// 文件级：记录并继续处理其它目录，不再中止整次扫描。
			errs = append(errs, ParseError{
				File:     path,
				Message:  perr.Error(),
				Severity: SeverityWarning,
				Kind:     KindNoPackages,
			})
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

// buildPackage runs the UAST builder. A GetResult failure is a request-level
// error (never a packageInfo:null product with exit 0).
func buildPackage(moduleName string, packages map[string]*ast.Package, fset *token.FileSet) (*uast.PackagePathInfo, []ParseError, error) {
	b := uast.NewUASTBuilder(moduleName, packages, fset)
	b.Build()
	res, err := b.GetResult()
	if err != nil {
		return nil, b.Errors(), fmt.Errorf("build package %s: %w", moduleName, err)
	}
	return res, b.Errors(), nil
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
	return moduleNameFromContent(content, modFilePath)
}

// moduleNameFromContent is the filesystem-free half of readModuleName, used by
// the in-memory ParseSources path.
func moduleNameFromContent(content []byte, source string) (string, error) {
	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, "module") {
			fields := strings.Fields(line)
			if len(fields) == 2 {
				return fields[1], nil
			}
		}
	}
	return "", fmt.Errorf("module directive not found in %s", source)
}

// pathDepth counts path separators, used to pick the shallowest go.mod.
func pathDepth(p string) int {
	return strings.Count(filepath.ToSlash(filepath.Clean(p)), "/")
}

// commonRoot returns the common ancestor directory of the file names, which
// plays the role of the CLI's -rootDir.
func commonRoot(files []SourceFile) string {
	root := filepath.Dir(files[0].Name)
	for _, f := range files[1:] {
		root = commonDir(root, filepath.Dir(f.Name))
	}
	return root
}

func commonDir(a, b string) string {
	if a == b {
		return a
	}
	if rel, err := filepath.Rel(a, b); err == nil && !isParentRef(rel) {
		return a
	}
	if rel, err := filepath.Rel(b, a); err == nil && !isParentRef(rel) {
		return b
	}
	for {
		parent := filepath.Dir(a)
		if parent == a {
			return parent
		}
		a = parent
		if rel, err := filepath.Rel(a, b); err == nil && !isParentRef(rel) {
			return a
		}
	}
}

func isParentRef(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// preparePackagesFromSources mirrors preparePackage, but over in-memory files:
// group by directory, parse each .go file individually (a bad file is recorded,
// not fatal), then pick one package name per directory via map iteration (O1
// preserved). Vendor and dot directories are skipped like the CLI does.
func preparePackagesFromSources(root string, files []SourceFile, fset *token.FileSet) (map[string]*ast.Package, []ParseError) {
	packages := make(map[string]*ast.Package)
	var errs []ParseError

	byDir := make(map[string][]SourceFile)
	for _, f := range files {
		dir := filepath.Dir(f.Name)
		if strings.Contains(filepath.ToSlash(dir), "/vendor") {
			continue
		}
		if base := filepath.Base(dir); strings.HasPrefix(base, ".") && base != "." && base != ".." {
			continue
		}
		byDir[dir] = append(byDir[dir], f)
	}

	for dir, dirFiles := range byDir {
		hasGo := false
		for _, f := range dirFiles {
			if strings.HasSuffix(f.Name, ".go") {
				hasGo = true
				break
			}
		}
		if !hasGo {
			continue
		}

		pkgs := make(map[string]map[string]*ast.File)
		for _, f := range dirFiles {
			if !strings.HasSuffix(f.Name, ".go") {
				continue
			}
			file, perr := parser.ParseFile(fset, f.Name, f.Content, 0)
			if perr != nil {
				errs = append(errs, ParseError{
					File:     f.Name,
					Message:  perr.Error(),
					Severity: SeverityError,
					Kind:     KindParseError,
				})
				continue
			}
			name := file.Name.Name
			if pkgs[name] == nil {
				pkgs[name] = make(map[string]*ast.File)
			}
			pkgs[name][f.Name] = file
		}

		for name, fileMap := range pkgs {
			rel, _ := filepath.Rel(root, dir)
			packagePath := filepath.Join("/", rel)
			packages[packagePath] = &ast.Package{
				Name:  name,
				Files: fileMap,
			}
			break
		}
	}
	return packages, errs
}

// containsGoFiles reports whether dir directly contains a .go file.
func containsGoFiles(dir string) bool {
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
