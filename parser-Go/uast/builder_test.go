package uast

import (
	"testing"
)

func TestBuilderPackageDeclaration(t *testing.T) {
	res := buildSource(t, "pkg.go", "package foo\n")
	if ds := decls(t, res); len(ds) != 0 {
		t.Fatalf("empty file should have no declarations, got %d", len(ds))
	}
	pd, ok := fileBody(t, res)[0].(*PackageDeclaration)
	if !ok {
		t.Fatalf("body[0] is %T, want *PackageDeclaration", fileBody(t, res)[0])
	}
	if name := asIdent(t, pd.PackageName).Name; name != "foo" {
		t.Fatalf("package name = %q, want foo", name)
	}
}

// TestBuilderImports covers the four import forms and confirms each ImportSpec
// is emitted exactly once (the preprocess cache must not double-emit).
func TestBuilderImports(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		res := buildSource(t, "imp.go", "package p\n\nimport \"fmt\"\n")
		ds := decls(t, res)
		if len(ds) != 1 {
			t.Fatalf("expected 1 import declaration, got %d", len(ds))
		}
		vd := asVarDecl(t, ds[0])
		if !vd.Meta.IsDefaultImport {
			t.Fatal("default import should set _meta.isDefaultImport")
		}
		if name := asIdent(t, vd.Id).Name; name != "fmt" {
			t.Fatalf("import id = %q, want fmt", name)
		}
		if from := asImport(t, vd.Init).From; from == nil || from.Value != "fmt" {
			t.Fatalf("import path = %+v, want fmt", asImport(t, vd.Init).From)
		}
	})

	t.Run("named", func(t *testing.T) {
		res := buildSource(t, "imp.go", "package p\n\nimport f \"fmt\"\n")
		ds := decls(t, res)
		vd := asVarDecl(t, ds[0])
		if vd.Meta.IsDefaultImport {
			t.Fatal("named import must not set _meta.isDefaultImport")
		}
		if name := asIdent(t, vd.Id).Name; name != "f" {
			t.Fatalf("import id = %q, want f", name)
		}
	})

	t.Run("dot", func(t *testing.T) {
		res := buildSource(t, "imp.go", "package p\n\nimport . \"time\"\n")
		ds := decls(t, res)
		spread, ok := ds[0].(*SpreadElement)
		if !ok {
			t.Fatalf("dot import node is %T, want *SpreadElement", ds[0])
		}
		imp := asImport(t, spread.Argument)
		if imp.From == nil || imp.From.Value != "time" {
			t.Fatalf("dot import path = %+v, want time", imp.From)
		}
	})

	t.Run("blank", func(t *testing.T) {
		res := buildSource(t, "imp.go", "package p\n\nimport _ \"fmt\"\n")
		ds := decls(t, res)
		vd := asVarDecl(t, ds[0])
		if vd.Meta.IsDefaultImport {
			t.Fatal("blank import must not set _meta.isDefaultImport")
		}
		if name := asIdent(t, vd.Id).Name; name != "_" {
			t.Fatalf("import id = %q, want _", name)
		}
	})

	t.Run("no-duplication", func(t *testing.T) {
		src := "package p\n\nimport (\n\t\"fmt\"\n\tf2 \"fmt\"\n\t. \"time\"\n\t_ \"os\"\n)\n"
		res := buildSource(t, "imp.go", src)
		ds := decls(t, res)
		if len(ds) != 4 {
			t.Fatalf("expected one node per ImportSpec (4), got %d", len(ds))
		}
	})
}

func TestBuilderVarConst(t *testing.T) {
	t.Run("var-typed", func(t *testing.T) {
		res := buildSource(t, "v.go", "package p\n\nvar x int = 1\n")
		ds := decls(t, res)
		vd := asVarDecl(t, ds[0])
		if name := asIdent(t, vd.Id).Name; name != "x" {
			t.Fatalf("id = %q, want x", name)
		}
		if typ, ok := vd.VarType.(*Identifier); !ok || typ.Name != "int" {
			t.Fatalf("varType = %#v, want Identifier int", vd.VarType)
		}
		if lit := asLiteral(t, vd.Init); lit.Value != "1" {
			t.Fatalf("init = %q, want 1", lit.Value)
		}
	})

	t.Run("const-inferred", func(t *testing.T) {
		res := buildSource(t, "c.go", "package p\n\nconst C = 1\n")
		ds := decls(t, res)
		vd := asVarDecl(t, ds[0])
		if name := asIdent(t, vd.Id).Name; name != "C" {
			t.Fatalf("id = %q, want C", name)
		}
		if typ, ok := vd.VarType.(*Identifier); !ok || typ.Name != "int" {
			t.Fatalf("varType = %#v, want Identifier int (inferred from BasicLit kind)", vd.VarType)
		}
	})

	t.Run("multi-name-value", func(t *testing.T) {
		res := buildSource(t, "v.go", "package p\n\nvar a, b = 1, 2\n")
		ds := decls(t, res)
		seq := asSequence(t, ds[0])
		if len(seq.Expressions) != 2 {
			t.Fatalf("expected 2 variable declarations, got %d", len(seq.Expressions))
		}
	})

	t.Run("grouped", func(t *testing.T) {
		src := "package p\n\nvar (\n\tx int\n\ty string\n)\n"
		res := buildSource(t, "v.go", src)
		ds := decls(t, res)
		if len(ds) != 2 {
			t.Fatalf("expected 2 declarations from grouped var, got %d", len(ds))
		}
	})
}

// TestBuilderTypeDeclarations exercises the core type forms. The "func" case is
// a characterization: VisitTypeSpec has no branch for *FuncType, so the
// resulting ClassDefinition is empty (see the comment in the subtest).
func TestBuilderTypeDeclarations(t *testing.T) {
	t.Run("struct", func(t *testing.T) {
		res := buildSource(t, "t.go", "package p\n\ntype S struct {\n\tX int\n\tY string\n}\n")
		cd := asClassDef(t, decls(t, res)[0])
		if cd.Id.Name != "S" {
			t.Fatalf("class id = %q, want S", cd.Id.Name)
		}
		if len(cd.Body) != 2 {
			t.Fatalf("struct body len = %d, want 2", len(cd.Body))
		}
		if name := asIdent(t, asVarDecl(t, cd.Body[0]).Id).Name; name != "X" {
			t.Fatalf("first field = %q, want X", name)
		}
	})

	t.Run("interface", func(t *testing.T) {
		src := "package p\n\ntype I interface {\n\tM(x int) string\n}\n"
		res := buildSource(t, "t.go", src)
		cd := asClassDef(t, decls(t, res)[0])
		if !cd.Meta.IsInterface {
			t.Fatal("interface ClassDefinition should set _meta.isInterface")
		}
		if len(cd.Body) != 1 {
			t.Fatalf("interface body len = %d, want 1", len(cd.Body))
		}
		ft, ok := cd.Body[0].(*FuncType)
		if !ok {
			t.Fatalf("interface member is %T, want *FuncType", cd.Body[0])
		}
		if ft.Id.Name != "M" {
			t.Fatalf("method name = %q, want M", ft.Id.Name)
		}
	})

	t.Run("map", func(t *testing.T) {
		res := buildSource(t, "t.go", "package p\n\ntype M map[string]int\n")
		cd := asClassDef(t, decls(t, res)[0])
		if len(cd.Supers) != 1 {
			t.Fatalf("map supers = %d, want 1", len(cd.Supers))
		}
		if _, ok := cd.Supers[0].(*MapType); !ok {
			t.Fatalf("map super = %T, want *MapType", cd.Supers[0])
		}
	})

	t.Run("chan", func(t *testing.T) {
		res := buildSource(t, "t.go", "package p\n\ntype C chan int\n")
		cd := asClassDef(t, decls(t, res)[0])
		if _, ok := cd.Supers[0].(*ChanType); !ok {
			t.Fatalf("chan super = %T, want *ChanType", cd.Supers[0])
		}
	})

	t.Run("array", func(t *testing.T) {
		res := buildSource(t, "t.go", "package p\n\ntype A [3]int\n")
		cd := asClassDef(t, decls(t, res)[0])
		at, ok := cd.Supers[0].(*ArrayType)
		if !ok {
			t.Fatalf("array super = %T, want *ArrayType", cd.Supers[0])
		}
		if lit := asLiteral(t, at.Size); lit.Value != "3" {
			t.Fatalf("array size = %q, want 3", lit.Value)
		}
	})

	t.Run("slice", func(t *testing.T) {
		res := buildSource(t, "t.go", "package p\n\ntype S []int\n")
		cd := asClassDef(t, decls(t, res)[0])
		if _, ok := cd.Supers[0].(*ArrayType); !ok {
			t.Fatalf("slice super = %T, want *ArrayType", cd.Supers[0])
		}
	})

	t.Run("alias", func(t *testing.T) {
		for _, src := range []string{
			"package p\n\ntype MyString string\n",
			"package p\n\ntype B = string\n",
		} {
			res := buildSource(t, "t.go", src)
			cd := asClassDef(t, decls(t, res)[0])
			if len(cd.Supers) != 1 {
				t.Fatalf("alias supers = %d, want 1", len(cd.Supers))
			}
			if name := asIdent(t, cd.Supers[0]).Name; name != "string" {
				t.Fatalf("alias super = %q, want string", name)
			}
		}
	})

	t.Run("func-type-gap", func(t *testing.T) {
		// Characterization (current behavior, gap to fix later): a function type
		// declaration produces a ClassDefinition with no members because
		// VisitTypeSpec does not handle *FuncType.
		res := buildSource(t, "t.go", "package p\n\ntype F func(x int) bool\n")
		cd := asClassDef(t, decls(t, res)[0])
		if len(cd.Body) != 0 || len(cd.Supers) != 0 {
			t.Fatalf("current behavior changed: func type declaration now has body=%d supers=%d", len(cd.Body), len(cd.Supers))
		}
	})
}

func TestBuilderFuncDeclaration(t *testing.T) {
	t.Run("params-and-return", func(t *testing.T) {
		res := buildSource(t, "f.go", "package p\n\nfunc Add(a int, b int) int {\n\treturn a + b\n}\n")
		fd := findFunction(t, decls(t, res), "Add")
		if len(fd.Parameters) != 2 {
			t.Fatalf("params = %d, want 2", len(fd.Parameters))
		}
		if typ, ok := fd.ReturnType.(*Identifier); !ok || typ.Name != "int" {
			t.Fatalf("returnType = %#v, want Identifier int", fd.ReturnType)
		}
		ss := scopedBody(t, fd)
		if len(ss) != 1 {
			t.Fatalf("body len = %d, want 1", len(ss))
		}
		if _, ok := ss[0].(*ReturnStatement); !ok {
			t.Fatalf("body[0] = %T, want *ReturnStatement", ss[0])
		}
	})

	t.Run("variadic-param", func(t *testing.T) {
		res := buildSource(t, "f.go", "package p\n\nfunc V(xs ...int) {}\n")
		fd := findFunction(t, decls(t, res), "V")
		if len(fd.Parameters) != 1 {
			t.Fatalf("params = %d, want 1", len(fd.Parameters))
		}
		if fd.Parameters[0].Meta.ParameterKind != "vararg" {
			t.Fatalf("parameterKind = %q, want vararg", fd.Parameters[0].Meta.ParameterKind)
		}
	})

	t.Run("no-return", func(t *testing.T) {
		res := buildSource(t, "f.go", "package p\n\nfunc N() {}\n")
		fd := findFunction(t, decls(t, res), "N")
		if _, ok := fd.ReturnType.(*VoidType); !ok {
			t.Fatalf("returnType = %T, want *VoidType", fd.ReturnType)
		}
	})
}

func TestBuilderMethodReceiver(t *testing.T) {
	t.Run("value-receiver", func(t *testing.T) {
		src := "package p\n\ntype T struct {\n\tX int\n}\n\nfunc (t T) M() int {\n\treturn t.X\n}\n"
		res := buildSource(t, "m.go", src)
		ds := decls(t, res)
		if len(ds) != 1 {
			t.Fatalf("top-level decls = %d, want 1 (method must fold into the class)", len(ds))
		}
		cd := asClassDef(t, ds[0])
		var method *FunctionDefinition
		for _, n := range cd.Body {
			if fd, ok := n.(*FunctionDefinition); ok && fd.Id != nil && fd.Id.(*Identifier).Name == "M" {
				method = fd
			}
		}
		if method == nil {
			t.Fatal("method M not folded into ClassDefinition body")
		}
		if method.Meta.ReceiveCls != "T" {
			t.Fatalf("_meta.ReceiveCls = %q, want T", method.Meta.ReceiveCls)
		}
		body := scopedBody(t, method)
		recv := asVarDecl(t, body[0])
		if name := asIdent(t, recv.Id).Name; name != "t" {
			t.Fatalf("receiver assignment id = %q, want t", name)
		}
		if _, ok := recv.Init.(*ThisExpression); !ok {
			t.Fatalf("receiver init = %T, want *ThisExpression", recv.Init)
		}
	})

	t.Run("pointer-receiver", func(t *testing.T) {
		src := "package p\n\ntype T struct{}\n\nfunc (t *T) M() {}\n"
		res := buildSource(t, "m.go", src)
		cd := asClassDef(t, decls(t, res)[0])
		method := asFuncDef(t, cd.Body[len(cd.Body)-1])
		body := scopedBody(t, method)
		recv := asVarDecl(t, body[0])
		this, ok := recv.Init.(*ThisExpression)
		if !ok {
			t.Fatalf("receiver init = %T, want *ThisExpression", recv.Init)
		}
		if _, ok := this.Meta.Type.(*PointerType); !ok {
			t.Fatalf("this._meta.type = %T, want *PointerType", this.Meta.Type)
		}
	})

	t.Run("unknown-receiver-stays-top-level", func(t *testing.T) {
		// A method on an undeclared type cannot be folded into a class and is
		// emitted as a top-level FunctionDefinition.
		src := "package p\n\nfunc (x External) M() {}\n"
		res := buildSource(t, "m.go", src)
		ds := decls(t, res)
		fd := asFuncDef(t, ds[0])
		if fd.Meta.ReceiveCls != "External" {
			t.Fatalf("_meta.ReceiveCls = %q, want External", fd.Meta.ReceiveCls)
		}
	})
}

func TestBuilderNamedReturns(t *testing.T) {
	t.Run("single-named-bare-return", func(t *testing.T) {
		src := "package p\n\nfunc F() (a int) {\n\treturn\n}\n"
		fd := findFunction(t, decls(t, buildSource(t, "f.go", src)), "F")
		ss := scopedBody(t, fd)
		if len(ss) != 2 {
			t.Fatalf("body len = %d, want 2 (promoted decl + return)", len(ss))
		}
		decl := asVarDecl(t, ss[0])
		if name := asIdent(t, decl.Id).Name; name != "a" {
			t.Fatalf("promoted return var = %q, want a", name)
		}
		ret := asReturn(t, ss[1])
		if name := asIdent(t, ret.Argument).Name; name != "a" {
			t.Fatalf("bare return argument = %q, want a", name)
		}
	})

	t.Run("multi-named-bare-return", func(t *testing.T) {
		src := "package p\n\nfunc F() (a int, b string) {\n\treturn\n}\n"
		fd := findFunction(t, decls(t, buildSource(t, "f.go", src)), "F")
		ss := scopedBody(t, fd)
		if len(ss) != 3 {
			t.Fatalf("body len = %d, want 3 (2 promoted + return)", len(ss))
		}
		ret := asReturn(t, ss[2])
		tuple, ok := ret.Argument.(*TupleExpression)
		if !ok {
			t.Fatalf("bare return argument = %T, want *TupleExpression", ret.Argument)
		}
		if len(tuple.Elements) != 2 {
			t.Fatalf("tuple elements = %d, want 2", len(tuple.Elements))
		}
	})

	t.Run("explicit-return-not-rewritten", func(t *testing.T) {
		src := "package p\n\nfunc F() (a int) {\n\treturn 5\n}\n"
		fd := findFunction(t, decls(t, buildSource(t, "f.go", src)), "F")
		ss := scopedBody(t, fd)
		ret := asReturn(t, ss[len(ss)-1])
		if lit, ok := ret.Argument.(*Literal); !ok || lit.Value != "5" {
			t.Fatalf("explicit return argument = %#v, want Literal 5", ret.Argument)
		}
	})
}

func TestBuilderStructEmbedding(t *testing.T) {
	src := "package p\n\ntype Base struct{}\n\ntype S struct {\n\tBase\n}\n"
	res := buildSource(t, "s.go", src)
	cd := findClass(t, decls(t, res), "S")
	if len(cd.Body) != 2 {
		t.Fatalf("embedded field should emit SpreadElement + VariableDeclaration, got %d nodes", len(cd.Body))
	}
	spread, ok := cd.Body[0].(*SpreadElement)
	if !ok {
		t.Fatalf("body[0] = %T, want *SpreadElement", cd.Body[0])
	}
	if name := asIdent(t, spread.Argument).Name; name != "Base" {
		t.Fatalf("spread argument = %q, want Base", name)
	}
	vd := asVarDecl(t, cd.Body[1])
	if !vd.Cloned {
		t.Fatal("embedded field VariableDeclaration should be cloned")
	}
	if name := asIdent(t, vd.Id).Name; name != "Base" {
		t.Fatalf("embedded field id = %q, want Base", name)
	}
}
