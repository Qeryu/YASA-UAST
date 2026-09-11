// Package api 是 Go UAST parser 的可导入库入口。
//
// 错误语义（D11）：
//   - 文件级失败记录为 ParseError 并继续构建，每项带 Severity（warning/error）
//     与 Kind；
//   - 请求级失败（rootDir 不可读、完全没有 package 等）以普通 error 返回，
//     不产出 JSON；
//   - 库绝不 os.Exit、绝不 panic：导出的入口都装了 recover() 边界，把残留
//     panic 转成请求级 error。
//
// P3 常驻核心：parseSource 接收内存字节（同步 wasm 回调无法做 Go 文件 IO，
// D8）。ParseSingleFile = 读文件 + 委派。
//
// 成功时返回的 JSON 与历史 CLI 输出逐字节一致（json.Encoder.Encode，含末尾换行）。
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

// ParseError 是一个文件级（可恢复）错误项。它是 uast.ParseError 的别名，
// 使库调用方无需 import uast。
type ParseError = uast.ParseError

// Severity 与 Kind 是 uast 定义的别名。
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

// Output 是顶层 JSON 文档。其结构与字段顺序为兼容 CLI 而冻结。
type Output struct {
	PackageInfo *uast.PackagePathInfo `json:"packageInfo"`
	ModuleName  string                `json:"moduleName"`
	GoModPath   string                `json:"goModPath"`
	NumOfGoMod  int                   `json:"numOfGoMod"`
}

// HasErrors 报告 errs 中是否至少有一个 error 级错误项。
// warning 级项仅供参考，不阻断产物输出。
func HasErrors(errs []ParseError) bool {
	for _, e := range errs {
		if e.Severity == SeverityError {
			return true
		}
	}
	return false
}

// parseSource 为单个内存 Go 源码文件构建 UAST。name 会原样用作
// loc/sourcefile 中的文件名，因此传入与 CLI `-rootDir` 相同的 name 可得到
// 逐字节一致的 JSON。
//
// 语法错误记录在 errs（error/parse_error）且 JSON 为 nil；err 仅用于
// 请求级/panic 失败。
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

// ParseSource 是 wasm/npm 常驻 parser 使用的导出内存单文件入口（P3）。
// name 会原样用作 loc.sourcefile。
func ParseSource(name string, src []byte) (jsonBytes []byte, errs []ParseError, err error) {
	return parseSource(name, src)
}

// ParseSingleFile 读取 path 并委派给 parseSource。读盘是唯一的文件系统访问；
// 读取失败记为文件级 error/read_error（不是请求级 error），与单文件解析失败的
// 退出语义一致（exit 1，无产物）。
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

// ParseProject 解析 rootDir 下的每个 package 并返回编码后的 JSON 文档。
// 单个无法解析的文件不会中止扫描：记入 errs（error/parse_error），其余文件照常
// 产出。缺少 go.mod 记为 warning（warning/no_gomod），并保留历史的
// "__unknown_module__" 兜底。
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

// SourceFile 是 ParseSources 使用的内存源码文件。
type SourceFile struct {
	Name    string
	Content []byte
}

// ParseSources 是导出的内存项目模式入口（P3）。它针对同一逻辑目录树复刻
// CLI `-rootDir` 的行为：
//   - 显式传入 root[0] 时以其为虚拟 root，否则取各文件名的共同祖先目录；
//   - package 路径为 "/"+相对目录（与 preparePackage 一致）；
//   - module 名取最浅层传入的 go.mod；
//   - 缺少 go.mod 记为 warning/no_gomod 并保留 __unknown_module__。
//
// 坏文件记为文件级 error/parse_error，不会丢弃好文件。对等价输入，tmpN 归一化后
// 输出与 CLI 一致（同一 package 内的跨文件顺序遵循 Go map 迭代）。
func ParseSources(files []SourceFile, rootArg ...string) (jsonBytes []byte, errs []ParseError, err error) {
	defer recoverBoundary("ParseSources", "<memory>", &jsonBytes, &errs, &err)

	if len(files) == 0 {
		return nil, nil, fmt.Errorf("no source files provided")
	}
	root := commonRoot(files)
	if len(rootArg) > 0 && rootArg[0] != "" {
		// 显式 root 优先（调用方知道 module root，例如 Engine 的项目目录）；
		// 为使 package 路径逐字节一致，它必须与 CLI 传给 -rootDir 的字符串相同。
		root = rootArg[0]
	}
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

// ParsePackage 逐个解析 dir 下的 .go 文件，使坏文件被记录而不丢弃好文件。
// 对含多个 package 名的目录，返回的 package 由 map 迭代顺序决定；这保留了原版
// （已知、本次不修）的 O1 行为。
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

// buildPackage 运行 UAST builder。GetResult 失败为请求级 error（绝不产出
// packageInfo:null 且 exit 0）。
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

// recoverBoundary 把库边界处的残留 panic 转成请求级 error，使调用方永远不会
// 遭遇进程崩溃。
func recoverBoundary(op, subject string, out *[]byte, errs *[]ParseError, err *error) {
	if r := recover(); r != nil {
		*out = nil
		*errs = nil
		*err = fmt.Errorf("uast: %s(%s): recovered panic: %v", op, subject, r)
	}
}

// findAllGoMod 在 dir 下搜索 go.mod 文件（保持原版行为）。
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

// readModuleName 从 go.mod 文件读取 module 名（保持原版行为）。
func readModuleName(modFilePath string) (string, error) {
	content, err := os.ReadFile(modFilePath)
	if err != nil {
		return "", err
	}
	return moduleNameFromContent(content, modFilePath)
}

// moduleNameFromContent 是 readModuleName 中去掉文件系统的部分，供内存态
// ParseSources 路径使用。
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

// pathDepth 统计路径分隔符数量，用于选取最浅层的 go.mod。
func pathDepth(p string) int {
	return strings.Count(filepath.ToSlash(filepath.Clean(p)), "/")
}

// commonRoot 返回各文件名的共同祖先目录，扮演 CLI `-rootDir` 的角色。
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

// preparePackagesFromSources 复刻 preparePackage，但基于内存文件。它精确复现
// CLI 的 filepath.Walk 剪枝语义：
//   - 目录 basename 以 "." 开头时，仅当该目录**直接包含 .go 文件**才连同整棵
//     子树一起剪掉（CLI 先判 ContainsGoFiles 再判 dot，因此没有直接 .go 文件的
//     dot 目录会被下钻，其子孙仍会解析）；
//   - 含 /vendor 的路径被排除；
//   - 每个 .go 文件单独解析（坏文件被记录，不致命）；
//   - 每个目录通过 map 迭代选出一个 package 名（保留 O1）。
func preparePackagesFromSources(root string, files []SourceFile, fset *token.FileSet) (map[string]*ast.Package, []ParseError) {
	packages := make(map[string]*ast.Package)
	var errs []ParseError

	dirHasGo := make(map[string]bool)
	for _, f := range files {
		if strings.HasSuffix(f.Name, ".go") {
			dirHasGo[filepath.Dir(f.Name)] = true
		}
	}

	byDir := make(map[string][]SourceFile)
	for _, f := range files {
		if !strings.HasSuffix(f.Name, ".go") {
			continue
		}
		dir := filepath.Dir(f.Name)
		if strings.Contains(filepath.ToSlash(dir), "/vendor") {
			continue
		}
		if prunedByDotDir(root, dir, dirHasGo) {
			continue
		}
		byDir[dir] = append(byDir[dir], f)
	}

	for dir, dirFiles := range byDir {
		pkgs := make(map[string]map[string]*ast.File)
		for _, f := range dirFiles {
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

// prunedByDotDir 报告 dir 是否位于 CLI 会用 filepath.SkipDir 剪掉的子树内。
// CLI 只对**直接包含 .go 文件**的 dot 目录返回 SkipDir；没有直接 .go 文件的
// dot 目录会被下钻（其子孙仍可能被解析）。
func prunedByDotDir(root, dir string, dirHasGo map[string]bool) bool {
	for cur := dir; ; {
		base := filepath.Base(cur)
		if base != "." && base != ".." && strings.HasPrefix(base, ".") && dirHasGo[cur] {
			return true
		}
		if cur == root {
			return false
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return false
		}
		cur = parent
	}
}

// containsGoFiles 报告 dir 是否直接包含 .go 文件。
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
