package uast

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestVisitorStatements(t *testing.T) {
	t.Run("block-scope", func(t *testing.T) {
		src := "package p\n\nfunc F() {\n\t{\n\t\tx := 1\n\t}\n}\n"
		ss := stmtsOf(t, src, "F")
		block := asScoped(t, ss[0])
		if len(block.Body) != 1 {
			t.Fatalf("nested block body len = %d, want 1", len(block.Body))
		}
	})

	t.Run("assignment-define", func(t *testing.T) {
		ss := stmtsOf(t, "package p\n\nfunc F() {\n\tx := 1\n}\n", "F")
		vd := asVarDecl(t, ss[0])
		if name := asIdent(t, vd.Id).Name; name != "x" {
			t.Fatalf("id = %q, want x", name)
		}
	})

	t.Run("assignment-equals", func(t *testing.T) {
		ss := stmtsOf(t, "package p\n\nfunc F() {\n\tx = 1\n}\n", "F")
		a := asAssign(t, ss[0])
		if a.Operator != "=" {
			t.Fatalf("operator = %q, want =", a.Operator)
		}
	})

	t.Run("parallel-define", func(t *testing.T) {
		// VisitBlockStmt flattens a top-level Sequence, so the two
		// VariableDeclarations surface directly in the function body.
		ss := stmtsOf(t, "package p\n\nfunc F() {\n\tc, d := 1, 2\n}\n", "F")
		if len(ss) != 2 {
			t.Fatalf("parallel define produced %d statements, want 2", len(ss))
		}
		for i, s := range ss {
			if _, ok := s.(*VariableDeclaration); !ok {
				t.Fatalf("stmt[%d] = %T, want *VariableDeclaration", i, s)
			}
		}
	})

	t.Run("multi-return-define", func(t *testing.T) {
		src := "package p\n\nfunc F() {\n\ta, b := g()\n}\n"
		ss := stmtsOf(t, src, "F")
		vd := asVarDecl(t, ss[0])
		if _, ok := vd.Id.(*TupleExpression); !ok {
			t.Fatalf("id = %T, want *TupleExpression for multi-return define", vd.Id)
		}
		if _, ok := vd.Init.(*CallExpression); !ok {
			t.Fatalf("init = %T, want *CallExpression", vd.Init)
		}
	})

	t.Run("inc", func(t *testing.T) {
		ss := stmtsOf(t, "package p\n\nfunc F() {\n\ti++\n}\n", "F")
		u := asUnary(t, ss[0])
		if u.Operator != "++" || !u.IsSuffix {
			t.Fatalf("inc = %q suffix=%v, want ++ suffix=true", u.Operator, u.IsSuffix)
		}
	})

	t.Run("if", func(t *testing.T) {
		ss := stmtsOf(t, "package p\n\nfunc F() {\n\tif x > 0 {\n\t}\n}\n", "F")
		ifst, ok := ss[0].(*IfStatement)
		if !ok {
			t.Fatalf("stmt = %T, want *IfStatement", ss[0])
		}
		if _, ok := ifst.Test.(*BinaryExpression); !ok {
			t.Fatalf("test = %T, want *BinaryExpression", ifst.Test)
		}
	})

	t.Run("if-with-init", func(t *testing.T) {
		src := "package p\n\nfunc F() {\n\tif y := 1; y > 0 {\n\t}\n}\n"
		ss := stmtsOf(t, src, "F")
		outer := asScoped(t, ss[0])
		if len(outer.Body) != 2 {
			t.Fatalf("if-with-init body = %d, want [init, if]", len(outer.Body))
		}
		if _, ok := outer.Body[1].(*IfStatement); !ok {
			t.Fatalf("body[1] = %T, want *IfStatement", outer.Body[1])
		}
	})

	t.Run("for", func(t *testing.T) {
		src := "package p\n\nfunc F() {\n\tfor i := 0; i < 3; i++ {\n\t}\n}\n"
		ss := stmtsOf(t, src, "F")
		fs, ok := ss[0].(*ForStatement)
		if !ok {
			t.Fatalf("stmt = %T, want *ForStatement", ss[0])
		}
		if fs.Init == nil || fs.Test == nil || fs.Update == nil {
			t.Fatalf("for parts: init=%v test=%v update=%v", fs.Init, fs.Test, fs.Update)
		}
	})

	t.Run("range", func(t *testing.T) {
		src := "package p\n\nfunc F() {\n\tfor k, v := range m {\n\t}\n}\n"
		ss := stmtsOf(t, src, "F")
		rs, ok := ss[0].(*RangeStatement)
		if !ok {
			t.Fatalf("stmt = %T, want *RangeStatement", ss[0])
		}
		if name := asIdent(t, rs.Key).Name; name != "k" {
			t.Fatalf("key = %q, want k", name)
		}
		if name := asIdent(t, rs.Value).Name; name != "v" {
			t.Fatalf("value = %q, want v", name)
		}
	})

	t.Run("switch", func(t *testing.T) {
		src := "package p\n\nfunc F(x int) {\n\tswitch x {\n\tcase 1:\n\t}\n}\n"
		ss := stmtsOf(t, src, "F")
		sw, ok := ss[0].(*SwitchStatement)
		if !ok {
			t.Fatalf("stmt = %T, want *SwitchStatement", ss[0])
		}
		if len(sw.Cases) != 1 {
			t.Fatalf("cases = %d, want 1", len(sw.Cases))
		}
		if _, ok := sw.Discriminant.(*Sequence); !ok {
			t.Fatalf("discriminant = %T, want *Sequence", sw.Discriminant)
		}
	})

	t.Run("type-switch", func(t *testing.T) {
		src := "package p\n\nfunc F(i interface{}) {\n\tswitch v := i.(type) {\n\tcase int:\n\t\t_ = v\n\t}\n}\n"
		ss := stmtsOf(t, src, "F")
		if _, ok := ss[0].(*SwitchStatement); !ok {
			t.Fatalf("stmt = %T, want *SwitchStatement", ss[0])
		}
	})

	t.Run("select", func(t *testing.T) {
		src := "package p\n\nfunc F(c chan int) {\n\tselect {\n\tcase <-c:\n\t\tx = 1\n\t}\n}\n"
		ss := stmtsOf(t, src, "F")
		scoped := asScoped(t, ss[0])
		if len(scoped.Body) != 1 {
			t.Fatalf("select body = %d, want 1 case clause", len(scoped.Body))
		}
		if _, ok := scoped.Body[0].(*ScopedStatement); !ok {
			t.Fatalf("case node = %T, want *ScopedStatement", scoped.Body[0])
		}
	})

	t.Run("go", func(t *testing.T) {
		ss := stmtsOf(t, "package p\n\nfunc F() {\n\tgo g()\n}\n", "F")
		call := asCall(t, ss[0])
		if !call.Meta.Async {
			t.Fatal("go statement should set _meta.async")
		}
	})

	t.Run("defer", func(t *testing.T) {
		ss := stmtsOf(t, "package p\n\nfunc F() {\n\tdefer g()\n}\n", "F")
		call := asCall(t, ss[0])
		if !call.Meta.Defer {
			t.Fatal("defer statement should set _meta.defer")
		}
	})

	t.Run("return", func(t *testing.T) {
		ss := stmtsOf(t, "package p\n\nfunc F() int {\n\treturn 1\n}\n", "F")
		ret := asReturn(t, ss[0])
		if lit := asLiteral(t, ret.Argument); lit.Value != "1" {
			t.Fatalf("return = %q, want 1", lit.Value)
		}
	})

	t.Run("bare-return", func(t *testing.T) {
		ss := stmtsOf(t, "package p\n\nfunc F() {\n\treturn\n}\n", "F")
		ret := asReturn(t, ss[0])
		if _, ok := ret.Argument.(*Noop); !ok {
			t.Fatalf("bare return argument = %T, want *Noop", ret.Argument)
		}
	})

	t.Run("break-label", func(t *testing.T) {
		src := "package p\n\nfunc F() {\nL:\n\tfor {\n\t\tbreak L\n\t}\n}\n"
		ss := stmtsOf(t, src, "F")
		labeled := asScoped(t, ss[0])
		if name := asIdent(t, labeled.Id).Name; name != "L" {
			t.Fatalf("label = %q, want L", name)
		}
		loop, ok := labeled.Body[0].(*ForStatement)
		if !ok {
			t.Fatalf("labeled body[0] = %T, want *ForStatement", labeled.Body[0])
		}
		loopBody := asScoped(t, loop.Body)
		br, ok := loopBody.Body[0].(*BreakStatement)
		if !ok {
			t.Fatalf("loop body[0] = %T, want *BreakStatement", loopBody.Body[0])
		}
		if br.Label == nil || br.Label.Name != "L" {
			t.Fatalf("break label = %+v, want L", br.Label)
		}
	})

	t.Run("empty", func(t *testing.T) {
		ss := stmtsOf(t, "package p\n\nfunc F() {\n\t;\n}\n", "F")
		if _, ok := ss[0].(*Noop); !ok {
			t.Fatalf("empty statement = %T, want *Noop", ss[0])
		}
	})
}

func TestVisitorExpressions(t *testing.T) {
	t.Run("literal-int", func(t *testing.T) {
		lit := asLiteral(t, exprStmt(t, "42"))
		if lit.Value != "42" || lit.LiteralType != "INT" {
			t.Fatalf("literal = %q/%q, want 42/INT", lit.Value, lit.LiteralType)
		}
	})

	t.Run("literal-string", func(t *testing.T) {
		// Current behavior: string literals keep their source quotes.
		lit := asLiteral(t, exprStmt(t, `"hi"`))
		if lit.Value != `"hi"` || lit.LiteralType != "STRING" {
			t.Fatalf("literal = %q/%q, want %q/STRING", lit.Value, lit.LiteralType, `"hi"`)
		}
	})

	t.Run("literal-float", func(t *testing.T) {
		lit := asLiteral(t, exprStmt(t, "3.14"))
		if lit.Value != "3.14" || lit.LiteralType != "FLOAT" {
			t.Fatalf("literal = %q/%q, want 3.14/FLOAT", lit.Value, lit.LiteralType)
		}
	})

	t.Run("call", func(t *testing.T) {
		call := asCall(t, exprStmt(t, "f(1, 2)"))
		if len(call.Arguments) != 2 {
			t.Fatalf("args = %d, want 2", len(call.Arguments))
		}
		if name := asIdent(t, call.Callee).Name; name != "f" {
			t.Fatalf("callee = %q, want f", name)
		}
	})

	t.Run("call-new", func(t *testing.T) {
		n, ok := exprStmt(t, "new(T)").(*NewExpression)
		if !ok {
			t.Fatalf("expr = %T, want *NewExpression", exprStmt(t, "new(T)"))
		}
		if name := asIdent(t, n.Callee).Name; name != "T" {
			t.Fatalf("new callee = %q, want T", name)
		}
	})

	t.Run("call-variadic", func(t *testing.T) {
		call := asCall(t, exprStmt(t, "f(xs...)"))
		spread, ok := call.Arguments[len(call.Arguments)-1].(*SpreadElement)
		if !ok {
			t.Fatalf("last variadic arg = %T, want *SpreadElement", call.Arguments[len(call.Arguments)-1])
		}
		if name := asIdent(t, spread.Argument).Name; name != "xs" {
			t.Fatalf("spread argument = %q, want xs", name)
		}
	})

	t.Run("selector", func(t *testing.T) {
		ma := asMemberAccess(t, exprStmt(t, "a.B"))
		if ma.Computed {
			t.Fatal("selector should not be computed")
		}
		if name := asIdent(t, ma.Object).Name; name != "a" {
			t.Fatalf("object = %q, want a", name)
		}
		if name := asIdent(t, ma.Property).Name; name != "B" {
			t.Fatalf("property = %q, want B", name)
		}
	})

	t.Run("index", func(t *testing.T) {
		ma := asMemberAccess(t, exprStmt(t, "a[i]"))
		if !ma.Computed {
			t.Fatal("index expression should be computed")
		}
	})

	t.Run("slice", func(t *testing.T) {
		ma := asMemberAccess(t, exprStmt(t, "a[1:2]"))
		se, ok := ma.Property.(*SliceExpression)
		if !ok {
			t.Fatalf("slice property = %T, want *SliceExpression", ma.Property)
		}
		if lit := asLiteral(t, se.Start); lit.Value != "1" {
			t.Fatalf("slice start = %q, want 1", lit.Value)
		}
		if lit := asLiteral(t, se.End); lit.Value != "2" {
			t.Fatalf("slice end = %q, want 2", lit.Value)
		}
	})

	t.Run("unary-neg", func(t *testing.T) {
		u := asUnary(t, exprStmt(t, "-x"))
		if u.Operator != "-" || u.IsSuffix {
			t.Fatalf("unary = %q suffix=%v, want - prefix", u.Operator, u.IsSuffix)
		}
	})

	t.Run("deref", func(t *testing.T) {
		if _, ok := exprStmt(t, "*p").(*DereferenceExpression); !ok {
			t.Fatalf("expr = %T, want *DereferenceExpression", exprStmt(t, "*p"))
		}
	})

	t.Run("address-of", func(t *testing.T) {
		if _, ok := exprStmt(t, "&x").(*ReferenceExpression); !ok {
			t.Fatalf("expr = %T, want *ReferenceExpression", exprStmt(t, "&x"))
		}
	})

	t.Run("channel-receive", func(t *testing.T) {
		u := asUnary(t, exprStmt(t, "<-ch"))
		if u.Operator != "pop" {
			t.Fatalf("receive operator = %q, want pop", u.Operator)
		}
	})

	t.Run("binary", func(t *testing.T) {
		be := asBinary(t, exprStmt(t, "a + b"))
		if be.Operator != "+" {
			t.Fatalf("operator = %q, want +", be.Operator)
		}
	})

	t.Run("type-assert", func(t *testing.T) {
		be := asBinary(t, exprStmt(t, "x.(T)"))
		if be.Operator != "instanceof" {
			t.Fatalf("operator = %q, want instanceof", be.Operator)
		}
	})

	t.Run("composite-lit-tmp", func(t *testing.T) {
		src := "package p\n\ntype Person struct {\n\tName string\n\tAge int\n}\n\nfunc F() {\n\tp := Person{Name: \"a\"}\n}\n"
		ss := stmtsOf(t, src, "F")
		vd := asVarDecl(t, ss[0])
		seq := asSequence(t, vd.Init)
		if len(seq.Expressions) < 3 {
			t.Fatalf("composite literal sequence len = %d, want >=3", len(seq.Expressions))
		}
		tmp := asVarDecl(t, seq.Expressions[0])
		tmpName := asIdent(t, tmp.Id).Name
		if !strings.HasPrefix(tmpName, "tmp") {
			t.Fatalf("first composite node id = %q, want tmpN", tmpName)
		}
		if _, ok := tmp.Init.(*NewExpression); !ok {
			t.Fatalf("tmp init = %T, want *NewExpression", tmp.Init)
		}
		last := asIdent(t, seq.Expressions[len(seq.Expressions)-1])
		if last.Name != tmpName {
			t.Fatalf("last composite node = %q, want %q", last.Name, tmpName)
		}
	})

	t.Run("closure", func(t *testing.T) {
		src := "package p\n\nfunc F() {\n\tf := func(s string) {}\n}\n"
		ss := stmtsOf(t, src, "F")
		vd := asVarDecl(t, ss[0])
		if _, ok := vd.Init.(*FunctionDefinition); !ok {
			t.Fatalf("closure init = %T, want *FunctionDefinition", vd.Init)
		}
	})
}

func TestVisitorTypes(t *testing.T) {
	t.Run("pointer", func(t *testing.T) {
		// Pointers are resolved in type state; a struct field is the reliable
		// positive context (`type P *int` alone yields an empty ClassDefinition,
		// see TestVisitorKnownGaps/pointer-alias).
		src := "package p\n\ntype S struct {\n\tP *int\n}\n"
		cd := asClassDef(t, decls(t, buildSource(t, "t.go", src))[0])
		field := asVarDecl(t, cd.Body[0])
		pt, ok := field.VarType.(*PointerType)
		if !ok {
			t.Fatalf("field varType = %T, want *PointerType", field.VarType)
		}
		if name := asIdent(t, pt.Element).Name; name != "int" {
			t.Fatalf("pointer element = %q, want int", name)
		}
	})

	t.Run("array", func(t *testing.T) {
		cd := asClassDef(t, decls(t, buildSource(t, "t.go", "package p\n\ntype A [3]int\n"))[0])
		at := cd.Supers[0].(*ArrayType)
		if lit := asLiteral(t, at.Size); lit.Value != "3" {
			t.Fatalf("array size = %q, want 3", lit.Value)
		}
	})

	t.Run("slice", func(t *testing.T) {
		cd := asClassDef(t, decls(t, buildSource(t, "t.go", "package p\n\ntype S []int\n"))[0])
		at, ok := cd.Supers[0].(*ArrayType)
		if !ok {
			t.Fatalf("super = %T, want *ArrayType", cd.Supers[0])
		}
		if _, ok := at.Size.(*Noop); !ok {
			t.Fatalf("slice size = %T, want *Noop (no length)", at.Size)
		}
	})

	t.Run("struct", func(t *testing.T) {
		src := "package p\n\ntype T struct {\n\tX int\n}\n"
		cd := asClassDef(t, decls(t, buildSource(t, "t.go", src))[0])
		if len(cd.Body) != 1 {
			t.Fatalf("struct body = %d, want 1", len(cd.Body))
		}
	})

	t.Run("map", func(t *testing.T) {
		cd := asClassDef(t, decls(t, buildSource(t, "t.go", "package p\n\ntype M map[string]int\n"))[0])
		mt, ok := cd.Supers[0].(*MapType)
		if !ok {
			t.Fatalf("super = %T, want *MapType", cd.Supers[0])
		}
		if name := asIdent(t, mt.KeyType).Name; name != "string" {
			t.Fatalf("map key = %q, want string", name)
		}
	})

	t.Run("chan-both-send-recv", func(t *testing.T) {
		cd := asClassDef(t, decls(t, buildSource(t, "t.go", "package p\n\ntype C chan int\n"))[0])
		ct := cd.Supers[0].(*ChanType)
		if ct.Dir != "" {
			t.Fatalf("bidirectional chan dir = %q, want empty", ct.Dir)
		}
	})

	t.Run("chan-send", func(t *testing.T) {
		cd := asClassDef(t, decls(t, buildSource(t, "t.go", "package p\n\ntype C chan<- int\n"))[0])
		if ct := cd.Supers[0].(*ChanType); ct.Dir != "send" {
			t.Fatalf("send-only chan dir = %q, want send", ct.Dir)
		}
	})

	t.Run("chan-receive", func(t *testing.T) {
		cd := asClassDef(t, decls(t, buildSource(t, "t.go", "package p\n\ntype C <-chan int\n"))[0])
		if ct := cd.Supers[0].(*ChanType); ct.Dir != "receive" {
			t.Fatalf("receive-only chan dir = %q, want receive", ct.Dir)
		}
	})

	t.Run("interface", func(t *testing.T) {
		src := "package p\n\ntype I interface {\n\tM(x int) string\n}\n"
		cd := asClassDef(t, decls(t, buildSource(t, "t.go", src))[0])
		if !cd.Meta.IsInterface {
			t.Fatal("interface should set _meta.isInterface")
		}
		if _, ok := cd.Body[0].(*FuncType); !ok {
			t.Fatalf("member = %T, want *FuncType", cd.Body[0])
		}
	})

	t.Run("func", func(t *testing.T) {
		// Characterization (current behavior, gap to fix later): function types
		// are parsed by VisitFuncType but VisitTypeSpec drops them from the
		// enclosing ClassDefinition.
		cd := asClassDef(t, decls(t, buildSource(t, "t.go", "package p\n\ntype F func(x int) bool\n"))[0])
		if len(cd.Body) != 0 || len(cd.Supers) != 0 {
			t.Fatalf("current behavior changed: func type declaration now has body=%d supers=%d", len(cd.Body), len(cd.Supers))
		}
	})
}

func asAssign(t *testing.T, n UNode) *AssignmentExpression {
	t.Helper()
	a, ok := n.(*AssignmentExpression)
	if !ok {
		t.Fatalf("node of type %T is not *AssignmentExpression", n)
	}
	return a
}

// TestVisitorKnownGaps documents current behavior for known feature gaps so a
// future fix makes the characterization fail loudly and gets updated on purpose.
func TestVisitorKnownGaps(t *testing.T) {
	t.Run("iota-not-evaluated", func(t *testing.T) {
		// current behavior,待修缺口: iota is emitted as a plain identifier and the
		// implicit continuation value of B is nil.
		src := "package p\n\nconst (\n\tA = iota\n\tB\n)\n"
		ds := decls(t, buildSource(t, "c.go", src))
		if len(ds) != 2 {
			t.Fatalf("const decls = %d, want 2", len(ds))
		}
		a := asVarDecl(t, ds[0])
		if id, ok := a.Init.(*Identifier); !ok || id.Name != "iota" {
			t.Fatalf("A init = %#v, want Identifier iota", a.Init)
		}
		b := asVarDecl(t, ds[1])
		if b.Init != nil {
			t.Fatalf("B init = %#v, want nil (iota not carried over)", b.Init)
		}
	})

	t.Run("fallthrough-becomes-noop", func(t *testing.T) {
		// current behavior,待修缺口: token.FALLTHROUGH has no dedicated node.
		src := "package p\n\nfunc F() {\n\tswitch {\n\tcase true:\n\t\tfallthrough\n\t}\n}\n"
		ss := stmtsOf(t, src, "F")
		sw := ss[0].(*SwitchStatement)
		caseBody := asScoped(t, sw.Cases[0].Body)
		if _, ok := caseBody.Body[0].(*Noop); !ok {
			t.Fatalf("fallthrough = %T, want *Noop", caseBody.Body[0])
		}
	})

	t.Run("goto-becomes-noop", func(t *testing.T) {
		// current behavior,待修缺口: token.GOTO maps to Noop.
		src := "package p\n\nfunc F() {\n\tgoto L\nL:\n\treturn\n}\n"
		ss := stmtsOf(t, src, "F")
		if _, ok := ss[0].(*Noop); !ok {
			t.Fatalf("goto = %T, want *Noop", ss[0])
		}
	})

	t.Run("generics-type-params-dropped", func(t *testing.T) {
		// current behavior,待修缺口: FuncDecl.TypeParams and TypeSpec.TypeParams
		// are not represented in the output.
		src := "package p\n\nfunc F[T any](x T) T {\n\treturn x\n}\n"
		fd := findFunction(t, decls(t, buildSource(t, "g.go", src)), "F")
		if len(fd.Parameters) != 1 {
			t.Fatalf("params = %d, want 1 (TypeParams dropped)", len(fd.Parameters))
		}
	})

	t.Run("generic-struct-type-params-dropped", func(t *testing.T) {
		src := "package p\n\ntype A[T comparable] struct {\n\tV T\n}\n"
		cd := asClassDef(t, decls(t, buildSource(t, "g.go", src))[0])
		if len(cd.Body) != 1 {
			t.Fatalf("generic struct body = %d, want 1", len(cd.Body))
		}
	})

	t.Run("struct-tags-dropped", func(t *testing.T) {
		// current behavior,待修缺口: struct field tags are not emitted.
		src := "package p\n\ntype T struct {\n\tX int `json:\"x\"`\n}\n"
		cd := asClassDef(t, decls(t, buildSource(t, "s.go", src))[0])
		if len(cd.Body) != 1 {
			t.Fatalf("struct body = %d, want 1", len(cd.Body))
		}
		raw, err := json.Marshal(cd)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "json") {
			t.Fatalf("struct tag leaked into output: %s", raw)
		}
	})

	t.Run("comments-dropped", func(t *testing.T) {
		// current behavior,待修缺口: comments are never turned into nodes.
		src := "// leading\npackage p\n\n/* block */\nfunc F() {}\n"
		res := buildSource(t, "cm.go", src)
		raw, err := json.Marshal(res)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "\"Comment\"") || strings.Contains(string(raw), "leading") {
			t.Fatalf("comment leaked into output: %s", raw)
		}
	})

	t.Run("switch-tag-not-bound-to-cases", func(t *testing.T) {
		// current behavior,待修缺口: builder.go builds `tagid` but never assigns
		// it, so case values are not turned into `tag == value` comparisons.
		src := "package p\n\nfunc F(x int) {\n\tswitch x {\n\tcase 1:\n\t}\n}\n"
		ss := stmtsOf(t, src, "F")
		sw := ss[0].(*SwitchStatement)
		if _, ok := sw.Cases[0].Test.(*Literal); !ok {
			t.Fatalf("case test = %T, want plain *Literal (tagid unset)", sw.Cases[0].Test)
		}
	})

	t.Run("three-index-slice-drops-max", func(t *testing.T) {
		// current behavior,待修缺口: SliceExpr.Max is ignored.
		ma := asMemberAccess(t, exprStmt(t, "a[1:2:3]"))
		se := ma.Property.(*SliceExpression)
		if se.Step != nil {
			t.Fatalf("slice step = %#v, want nil (max index ignored)", se.Step)
		}
	})

	t.Run("pointer-alias-dropped", func(t *testing.T) {
		// current behavior,待修缺口: VisitTypeSpec calls u.visit (not
		// visitAsType) on the type, so *ast.StarExpr is treated as a
		// dereference expression and the alias loses its type.
		cd := asClassDef(t, decls(t, buildSource(t, "t.go", "package p\n\ntype P *int\n"))[0])
		if len(cd.Body) != 0 || len(cd.Supers) != 0 {
			t.Fatalf("current behavior changed: pointer alias now has body=%d supers=%d", len(cd.Body), len(cd.Supers))
		}
	})

	t.Run("interface-embedding-dropped", func(t *testing.T) {
		// current behavior,待修缺口: VisitInterfaceType skips fields without
		// Names, so an embedded interface disappears.
		src := "package p\n\ntype J interface {\n\tM()\n}\n\ntype I interface {\n\tJ\n}\n"
		cd := findClass(t, decls(t, buildSource(t, "i.go", src)), "I")
		if len(cd.Body) != 0 {
			t.Fatalf("embedded interface body = %d, want 0 (dropped)", len(cd.Body))
		}
		if !cd.Meta.IsInterface {
			t.Fatal("I should still be marked as interface")
		}
	})
}
