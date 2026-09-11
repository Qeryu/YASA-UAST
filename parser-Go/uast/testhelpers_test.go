package uast

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// buildSource parses src as a single file called name, wraps it in a synthetic
// "__single__" package, builds the UAST and returns the result. This mirrors the
// CLI single-file path (main.go parseSingleFile) and the buidUAST helper in
// parser-Go/uast_test.go, but stays inside package uast so tests can use the
// unexported builder helpers directly.
func buildSource(t *testing.T, name, src string) *PackagePathInfo {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.DeclarationErrors)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	pkg := &ast.Package{
		Name:    "__single__",
		Scope:   nil,
		Imports: nil,
		Files:   make(map[string]*ast.File),
	}
	pkg.Files[name] = f
	packages := map[string]*ast.Package{"__single__": pkg}

	b := NewUASTBuilder("__single_module__", packages, fset)
	b.Build()
	res, err := b.GetResult()
	if err != nil {
		t.Fatalf("GetResult after Build: %v", err)
	}
	return res
}

// fileNode returns the single NodeInfo produced for the synthetic package.
func fileNode(t *testing.T, res *PackagePathInfo) *NodeInfo {
	t.Helper()
	if res == nil {
		t.Fatal("nil build result")
	}
	root := res.Subs["/"]
	if root == nil {
		t.Fatalf("build result has no root package path: %+v", res)
	}
	if len(root.Files) != 1 {
		t.Fatalf("expected exactly 1 file in root, got %d", len(root.Files))
	}
	for _, ni := range root.Files {
		return ni
	}
	return nil
}

// compileUnit returns the CompileUnit of the synthetic build.
func compileUnit(t *testing.T, res *PackagePathInfo) *CompileUnit {
	t.Helper()
	ni := fileNode(t, res)
	cu, ok := ni.Node.(*CompileUnit)
	if !ok {
		t.Fatalf("root node is %T, want *CompileUnit", ni.Node)
	}
	return cu
}

// fileBody returns the full compile-unit body (package declaration + decls).
func fileBody(t *testing.T, res *PackagePathInfo) []Instruction {
	t.Helper()
	return compileUnit(t, res).Body
}

// decls returns the compile-unit body with the leading PackageDeclaration removed.
func decls(t *testing.T, res *PackagePathInfo) []Instruction {
	t.Helper()
	b := fileBody(t, res)
	if len(b) == 0 {
		t.Fatal("compile unit body is empty")
	}
	if _, ok := b[0].(*PackageDeclaration); !ok {
		t.Fatalf("body[0] is %T, want *PackageDeclaration", b[0])
	}
	return b[1:]
}

// stmtsOf builds src, locates funcName and returns its body statements.
func stmtsOf(t *testing.T, src, funcName string) []Instruction {
	t.Helper()
	res := buildSource(t, "test.go", src)
	fd := findFunction(t, decls(t, res), funcName)
	return scopedBody(t, fd)
}

// exprStmt wraps expr in a single function and returns that expression statement.
func exprStmt(t *testing.T, expr string) Instruction {
	t.Helper()
	src := "package p\n\nfunc F() {\n" + expr + "\n}\n"
	ss := stmtsOf(t, src, "F")
	if len(ss) == 0 {
		t.Fatalf("expression %q produced no statements", expr)
	}
	return ss[0]
}

func asIdent(t *testing.T, n UNode) *Identifier {
	t.Helper()
	id, ok := n.(*Identifier)
	if !ok {
		t.Fatalf("node of type %T is not *Identifier", n)
	}
	return id
}

func asLiteral(t *testing.T, n UNode) *Literal {
	t.Helper()
	l, ok := n.(*Literal)
	if !ok {
		t.Fatalf("node of type %T is not *Literal", n)
	}
	return l
}

func asVarDecl(t *testing.T, n UNode) *VariableDeclaration {
	t.Helper()
	vd, ok := n.(*VariableDeclaration)
	if !ok {
		t.Fatalf("node of type %T is not *VariableDeclaration", n)
	}
	return vd
}

func asFuncDef(t *testing.T, n UNode) *FunctionDefinition {
	t.Helper()
	fd, ok := n.(*FunctionDefinition)
	if !ok {
		t.Fatalf("node of type %T is not *FunctionDefinition", n)
	}
	return fd
}

func asClassDef(t *testing.T, n UNode) *ClassDefinition {
	t.Helper()
	cd, ok := n.(*ClassDefinition)
	if !ok {
		t.Fatalf("node of type %T is not *ClassDefinition", n)
	}
	return cd
}

func asScoped(t *testing.T, n UNode) *ScopedStatement {
	t.Helper()
	ss, ok := n.(*ScopedStatement)
	if !ok {
		t.Fatalf("node of type %T is not *ScopedStatement", n)
	}
	return ss
}

func asCall(t *testing.T, n UNode) *CallExpression {
	t.Helper()
	ce, ok := n.(*CallExpression)
	if !ok {
		t.Fatalf("node of type %T is not *CallExpression", n)
	}
	return ce
}

func asImport(t *testing.T, n UNode) *ImportExpression {
	t.Helper()
	ie, ok := n.(*ImportExpression)
	if !ok {
		t.Fatalf("node of type %T is not *ImportExpression", n)
	}
	return ie
}

func asSequence(t *testing.T, n UNode) *Sequence {
	t.Helper()
	seq, ok := n.(*Sequence)
	if !ok {
		t.Fatalf("node of type %T is not *Sequence", n)
	}
	return seq
}

func asBinary(t *testing.T, n UNode) *BinaryExpression {
	t.Helper()
	be, ok := n.(*BinaryExpression)
	if !ok {
		t.Fatalf("node of type %T is not *BinaryExpression", n)
	}
	return be
}

func asUnary(t *testing.T, n UNode) *UnaryExpression {
	t.Helper()
	ue, ok := n.(*UnaryExpression)
	if !ok {
		t.Fatalf("node of type %T is not *UnaryExpression", n)
	}
	return ue
}

func asMemberAccess(t *testing.T, n UNode) *MemberAccess {
	t.Helper()
	ma, ok := n.(*MemberAccess)
	if !ok {
		t.Fatalf("node of type %T is not *MemberAccess", n)
	}
	return ma
}

func asReturn(t *testing.T, n UNode) *ReturnStatement {
	t.Helper()
	rs, ok := n.(*ReturnStatement)
	if !ok {
		t.Fatalf("node of type %T is not *ReturnStatement", n)
	}
	return rs
}

// findFunction returns the FunctionDefinition with the given name from list.
func findFunction(t *testing.T, list []Instruction, name string) *FunctionDefinition {
	t.Helper()
	for _, n := range list {
		if fd, ok := n.(*FunctionDefinition); ok {
			if id, ok := fd.Id.(*Identifier); ok && id.Name == name {
				return fd
			}
		}
	}
	t.Fatalf("function %q not found in %d declarations", name, len(list))
	return nil
}

// findClass returns the ClassDefinition with the given name from list.
func findClass(t *testing.T, list []Instruction, name string) *ClassDefinition {
	t.Helper()
	for _, n := range list {
		if cd, ok := n.(*ClassDefinition); ok && cd.Id != nil && cd.Id.Name == name {
			return cd
		}
	}
	t.Fatalf("class %q not found in %d declarations", name, len(list))
	return nil
}

// scopedBody unwraps the *ScopedStatement body of a function definition.
func scopedBody(t *testing.T, fd *FunctionDefinition) []Instruction {
	t.Helper()
	if fd.Body == nil {
		t.Fatalf("function %s has nil body", fd.Id)
	}
	ss, ok := fd.Body.(*ScopedStatement)
	if !ok {
		t.Fatalf("function body is %T, want *ScopedStatement", fd.Body)
	}
	return ss.Body
}
