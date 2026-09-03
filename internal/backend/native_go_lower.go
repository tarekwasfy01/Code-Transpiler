package backend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/constant"
	"go/importer"
	goparser "go/parser"
	gotoken "go/token"
	"go/types"
	"sort"
	"strconv"

	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

// LowerNativeGo lowers the structurally supported Go subset directly from
// Go's AST. Scalar, aggregate and function values share the same semantic
// expression contract; unsupported shapes are rejected by the type contract.
// No normalized source text is created or fed to the legacy parser.
// Unsupported syntax is an error, including dead unsupported code.
func LowerNativeGo(filename, source string) (*SemanticProgram, error) {
	fs := gotoken.NewFileSet()
	file, err := goparser.ParseFile(fs, filename, source, 0)
	if err != nil {
		return nil, err
	}
	if file.Name.Name != "main" {
		return nil, fmt.Errorf("%s: native executable frontend requires package main", filename)
	}
	for _, imp := range file.Imports {
		path, e := strconv.Unquote(imp.Path.Value)
		if e != nil || path != "fmt" || imp.Name != nil {
			return nil, fmt.Errorf("%s: only unaliased fmt import is supported", fs.Position(imp.Pos()))
		}
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	conf := types.Config{Importer: importer.Default(), Sizes: types.SizesFor("gc", "amd64")}
	if _, err = conf.Check("main", fs, []*ast.File{file}, info); err != nil {
		// A distributed OneFile must not require a locally installed Go SDK just
		// to analyse the supported fmt.Println subset. Retry with the bounded
		// frontend importer; all other imports were rejected above.
		info = &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
		conf.Importer = nativeGoFmtImporter{}
		if _, err = conf.Check("main", fs, []*ast.File{file}, info); err != nil {
			return nil, err
		}
	}
	l := &goScalarLowerer{fs: fs, info: info, symbols: map[types.Object]string{}, types: map[string]SemanticType{}, functions: map[types.Object]string{}}
	var main *ast.FuncDecl
	var helpers []*ast.FuncDecl
	for _, decl := range file.Decls {
		if imp, ok := decl.(*ast.GenDecl); ok && imp.Tok == gotoken.IMPORT {
			continue
		}
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name == "init" || fn.Body == nil || fn.Type.TypeParams != nil {
			return nil, fmt.Errorf("%s: unsupported native declaration (global, method, init or generic)", fs.Position(decl.Pos()))
		}
		if fn.Name.Name == "main" {
			main = fn
			continue
		}
		l.functions[info.Defs[fn.Name]] = fmt.Sprintf("native_function_%d", len(helpers))
		helpers = append(helpers, fn)
	}
	if main == nil || main.Body == nil {
		return nil, fmt.Errorf("%s: missing main body", filename)
	}
	ordered, callGraph, err := l.orderFunctions(helpers)
	if err != nil {
		return nil, err
	}
	draft := SemanticStatement{Kind: "block", Source: l.span(file)}
	for _, fn := range ordered {
		draft.Statements = append(draft.Statements, l.function(fn))
	}
	mainBody := l.stmt(main.Body)
	draft.Statements = append(draft.Statements, mainBody.Statements...)
	if l.err != nil {
		return nil, l.err
	}
	assignDocumentIDs(&draft)
	spans := &sourceSpanVisitor{spans: map[int]SemanticSourceSpan{}}
	profile := NewSemanticProgram(&BlockStmt{}, "eager_left_to_right")
	if err := profile.AttachSemanticFeatureProfile("go"); err != nil {
		return nil, err
	}
	wrapper := SemanticDocument{SchemaVersion: SemanticDocumentVersion, Schema: SemanticDocumentSchema, Evaluation: "eager_left_to_right", ValueModel: "tagged_dynamic_binary64", IndexBase: 1, Types: defaultSemanticTypeContract(), Origin: SemanticOrigin{SourceLanguage: "go", EntryPoint: "main"}, SemanticFeatures: profile.SemanticFeatures, Root: draft}
	if err = WalkSemanticDocument(&wrapper, spans); err != nil {
		return nil, err
	}
	spans.restore = true
	if err = WalkSemanticDocument(&wrapper, spans); err != nil {
		return nil, err
	}
	draft = wrapper.Root
	facts := extractNativeGoSemanticFacts(&draft)
	program := NewSemanticProgram(&BlockStmt{}, "eager_left_to_right")
	program.Origin.SourceLanguage = "go"
	program.SemanticFeatures = profile.SemanticFeatures
	program.Contracts.Requires = []string{"native.go.scalar"}
	if len(l.integerFeatures) > 0 {
		program.Contracts.Requires = append(program.Contracts.Requires, "integer.operations.v1")
		var features []string
		for feature := range l.integerFeatures {
			features = append(features, feature)
		}
		sort.Strings(features)
		program.Contracts.Requires = append(program.Contracts.Requires, features...)
	}
	if len(helpers) > 0 {
		program.Contracts.Requires = append(program.Contracts.Requires, "native.go.functions")
	}
	program.Metadata = map[string]string{"frontend": "native-go-uast-v1", "subset": "bool,string,fixed-width-integer,aggregate,function-values,control,acyclic-functions,fmt.Println", "typecheck_architecture": "gc/amd64", "source_mapping": "go-hir", "source": "go-hir", "semantic_facts": strconv.Itoa(facts.Evidence)}
	// Extension objects use the same canonical map representation before and
	// after JSON import; typed struct field order must not break determinism.
	typeJSON, err := json.Marshal(l.types)
	if err != nil {
		return nil, err
	}
	var bindingTypes map[string]any
	decoder := json.NewDecoder(bytes.NewReader(typeJSON))
	decoder.UseNumber()
	if err := decoder.Decode(&bindingTypes); err != nil {
		return nil, err
	}
	program.Extensions = map[string]any{"native_binding_types": bindingTypes}
	if len(helpers) > 0 {
		names := make([]string, len(helpers))
		for i, fn := range helpers {
			names[i] = l.functions[info.Defs[fn.Name]]
		}
		entries := make([][3]int, 0, callGraph.NonZeros())
		callGraph.Each(func(r, c int, v float64) { entries = append(entries, [3]int{r, c, int(v)}) })
		program.Extensions["native_call_graph"] = map[string]any{"rows": callGraph.Rows, "cols": callGraph.Cols, "storage": "coo", "entries": entries}
		program.Extensions["native_call_graph_axes"] = names
	}
	if facts.RawUAST == nil {
		return nil, fmt.Errorf("native Go facts have no raw UAST")
	}
	facts.RawUAST.Metadata, facts.RawUAST.Extensions, facts.RawUAST.Contracts = program.Metadata, program.Extensions, program.Contracts
	facts.RawUAST.SemanticFeatures = program.SemanticFeatures
	sharedFacts, err := frontendSemanticFactsFromUniversalAST(facts.RawUAST, nil)
	if err != nil {
		return nil, err
	}
	facts.FrontendSemanticFacts = sharedFacts
	uast, err := buildNativeGoUniversalAST(facts, program.Metadata, program.Extensions, program.Contracts, program.SemanticFeatures, program.Evidence)
	if err != nil {
		return nil, err
	}
	program.UniversalAST, program.Body = uast, nil
	return program, nil
}

type goScalarLowerer struct {
	fs              *gotoken.FileSet
	info            *types.Info
	symbols         map[types.Object]string
	types           map[string]SemanticType
	err             error
	loopDepth       int
	functions       map[types.Object]string
	inFunction      bool
	integerFeatures map[string]bool
}

// nativeGoFmtImporter is a self-contained declaration of the only external
// package admitted by the bounded Native-Go frontend. It avoids coupling the
// released transpiler EXE to GOROOT while preserving Go type-checking for the
// supported fmt.Println call.
type nativeGoFmtImporter struct{}

func (nativeGoFmtImporter) Import(path string) (*types.Package, error) {
	if path != "fmt" {
		return nil, fmt.Errorf("unsupported native import %q", path)
	}
	pkg := types.NewPackage("fmt", "fmt")
	anyType := types.NewInterfaceType(nil, nil)
	anyType.Complete()
	params := types.NewTuple(types.NewVar(gotoken.NoPos, pkg, "a", types.NewSlice(anyType)))
	results := types.NewTuple(types.NewVar(gotoken.NoPos, pkg, "n", types.Typ[types.Int]), types.NewVar(gotoken.NoPos, pkg, "err", types.Universe.Lookup("error").Type()))
	pkg.Scope().Insert(types.NewFunc(gotoken.NoPos, pkg, "Println", types.NewSignatureType(nil, nil, nil, params, results, true)))
	pkg.MarkComplete()
	return pkg, nil
}

// The call matrix orders declarations before their callers. Cycles are
// rejected until every enabled target supports recursive closure binding.
func (l *goScalarLowerer) orderFunctions(functions []*ast.FuncDecl) ([]*ast.FuncDecl, matrixir.SparseMatrix, error) {
	indices := map[types.Object]int{}
	for i, fn := range functions {
		indices[l.info.Defs[fn.Name]] = i
	}
	graph := matrixir.NewSparseMatrix(len(functions), len(functions))
	for i, fn := range functions {
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if id, ok := call.Fun.(*ast.Ident); ok {
					if j, ok := indices[l.info.Uses[id]]; ok {
						graph.Set(i, j, 1)
					}
				}
			}
			return true
		})
	}
	state := make([]int, len(functions))
	var ordered []*ast.FuncDecl
	var visit func(int) error
	visit = func(i int) error {
		if state[i] == 1 {
			return fmt.Errorf("%s: recursive native functions are unsupported", l.fs.Position(functions[i].Pos()))
		}
		if state[i] == 2 {
			return nil
		}
		state[i] = 1
		for j := range functions {
			if graph.At(i, j) != 0 {
				if err := visit(j); err != nil {
					return err
				}
			}
		}
		state[i] = 2
		ordered = append(ordered, functions[i])
		return nil
	}
	for i := range functions {
		if err := visit(i); err != nil {
			return nil, graph, err
		}
	}
	return ordered, graph, nil
}

func (l *goScalarLowerer) function(fn *ast.FuncDecl) SemanticStatement {
	signature := l.info.Defs[fn.Name].Type().(*types.Signature)
	if signature.Variadic() || signature.Results().Len() > 1 {
		l.fail(fn, "variadic or multiple-result function")
	}
	for i := 0; i < signature.Results().Len(); i++ {
		r := signature.Results().At(i)
		// Result shape is a structural contract.  The semantic function model
		// already carries recursive SemanticType values, so aggregates and
		// function values must not be rejected by the old scalar-only guard.
		if r.Name() != "" || !l.supportedValue(r.Type()) {
			l.fail(fn, "named or unsupported function result type")
		}
	}
	function := &SemanticFunction{}
	if fn.Type.Params != nil {
		for _, field := range fn.Type.Params.List {
			if len(field.Names) == 0 {
				l.fail(fn, "unnamed parameter")
			}
			for _, name := range field.Names {
				object := l.info.Defs[name]
				if object == nil || !l.supportedValue(object.Type()) {
					l.fail(name, "unsupported parameter type")
				}
				symbol := l.name(name)
				typ := l.types[symbol]
				typ.TypeOrigin = "explicit"
				l.types[symbol] = typ
				parameter := SemanticParameter{Name: symbol}
				if typ, ok := nativeFixedInteger(object.Type()); ok {
					parameter.Type = typ
					parameter.Passing = "value"
				}
				function.Parameters = append(function.Parameters, parameter)
			}
		}
	}
	l.inFunction = true
	function.Body = l.stmt(fn.Body)
	l.inFunction = false
	// Go void fallthrough is an explicit return in the language-neutral CFG.
	// Do not apply this to legacy functions with implicit last-value returns.
	if signature.Results().Len() == 0 {
		function.Body.Statements = append(function.Body.Statements, SemanticStatement{Kind: "return", Source: l.span(fn.Body)})
	}
	return SemanticStatement{Kind: "assign", Name: l.functions[l.info.Defs[fn.Name]], AssignOp: "<-", Source: l.span(fn), Expression: &SemanticExpression{Kind: "function", Function: function, Source: l.span(fn)}}
}

func (l *goScalarLowerer) helperCall(call *ast.CallExpr) *SemanticExpression {
	e := &SemanticExpression{Kind: "call", Operator: "eager_left_to_right", Source: l.span(call)}
	id, ok := call.Fun.(*ast.Ident)
	if !ok || call.Ellipsis.IsValid() {
		l.fail(call, "indirect or variadic function call")
		return e
	}
	name, ok := l.functions[l.info.Uses[id]]
	if !ok {
		l.fail(call, "unregistered native function call")
		return e
	}
	e.Value = &SemanticExpression{Kind: "identifier", Name: name, Source: l.span(id)}
	for _, arg := range call.Args {
		e.Arguments = append(e.Arguments, SemanticArgument{Value: l.expr(arg)})
	}
	return e
}

func (l *goScalarLowerer) fail(n ast.Node, message string) {
	if l.err == nil {
		l.err = fmt.Errorf("%s: unsupported native Go semantics: %s", l.fs.Position(n.Pos()), message)
	}
}
func (l *goScalarLowerer) span(n ast.Node) *SemanticSourceSpan {
	a, b := l.fs.Position(n.Pos()), l.fs.Position(n.End())
	return &SemanticSourceSpan{File: a.Filename, StartOffset: a.Offset, EndOffset: b.Offset, StartLine: a.Line, StartColumn: a.Column, EndLine: b.Line, EndColumn: b.Column}
}
func (l *goScalarLowerer) name(n *ast.Ident) string {
	object := l.info.Defs[n]
	if object == nil {
		object = l.info.Uses[n]
	}
	if object == nil || n.Name == "_" {
		l.fail(n, "blank or unresolved binding")
		return "invalid"
	}
	if _, ok := object.(*types.Var); !ok {
		l.fail(n, "non-variable binding")
		return "invalid"
	}
	if name, ok := l.symbols[object]; ok {
		return name
	}
	name := fmt.Sprintf("native_var_%d", len(l.symbols))
	l.symbols[object] = name
	typ := nativeGoType(object.Type(), map[types.Type]bool{})
	l.types[name] = typ
	return name
}
func (l *goScalarLowerer) scalar(t types.Type) bool {
	if _, ok := nativeFixedInteger(t); ok {
		return true
	}
	b, ok := t.(*types.Basic)
	return ok && (b.Info()&(types.IsBoolean|types.IsString|types.IsFloat) != 0)
}
func (l *goScalarLowerer) supportedValue(t types.Type) bool {
	if l.scalar(t) { return true }
	switch x := t.(type) { case *types.Slice: return l.scalar(x.Elem()); case *types.Array: return l.scalar(x.Elem()); case *types.Signature: return true }
	return false
}
func (l *goScalarLowerer) expr(n ast.Expr) *SemanticExpression {
	if typ, ok := nativeFixedInteger(l.info.TypeOf(n)); ok {
		return l.integerExpr(n, typ)
	}
	if binary, ok := n.(*ast.BinaryExpr); ok {
		if typ, ok := nativeFixedInteger(l.info.TypeOf(binary.X)); ok {
			op := map[string]string{"==": "integer.equal", "!=": "integer.not_equal", "<": "integer.less", "<=": "integer.less_equal", ">": "integer.greater", ">=": "integer.greater_equal"}[binary.Op.String()]
			if op != "" {
				return l.integerOperation(n, op, typ, l.expr(binary.X), l.expr(binary.Y))
			}
		}
	}
	e := &SemanticExpression{Source: l.span(n)}
	if tv, ok := l.info.Types[n]; ok && tv.Value != nil {
		e.Kind = "literal"
		switch tv.Value.Kind() {
		case constant.Bool:
			e.LiteralKind = "boolean"
			e.Text = "FALSE"
			if constant.BoolVal(tv.Value) {
				e.Text = "TRUE"
			}
		case constant.String:
			value := constant.StringVal(tv.Value)
			// Restrict the current cross-target string codec to printable ASCII.
			// This avoids claiming equivalence for backend escape/Unicode differences.
			for _, r := range value {
				if r < 32 || r > 126 || r == '"' || r == '\\' {
					l.fail(n, "string requires an unverified escape or Unicode lowering")
				}
			}
			e.LiteralKind = "string"
			e.Text = strconv.Quote(value)
		case constant.Float:
			e.LiteralKind = "number"
			e.Text = tv.Value.ExactString()
		default:
			l.fail(n, "constant kind")
		}
		return e
	}
	switch x := n.(type) {
	case *ast.CallExpr:
		if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "len" && len(x.Args)==1 { return &SemanticExpression{Kind:"call",Operator:"eager_left_to_right",Value:&SemanticExpression{Kind:"identifier",Name:"length"},Arguments:[]SemanticArgument{{Value:l.expr(x.Args[0])}},Source:l.span(x)} }
		if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "make" { e.Kind="call"; e.Operator="eager_left_to_right"; e.Value=&SemanticExpression{Kind:"identifier",Name:"__make_float64"}; for _,a:=range x.Args[1:] { e.Arguments=append(e.Arguments,SemanticArgument{Value:l.expr(a)}) }; return e }
		if sel, ok := x.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name=="Println" { e.Kind="call"; e.Operator="eager_left_to_right"; e.Value=&SemanticExpression{Kind:"identifier",Name:"print"}; for _,a:=range x.Args { e.Arguments=append(e.Arguments,SemanticArgument{Value:l.expr(a)}) }; return e }
		if _, ok := x.Fun.(*ast.FuncLit); ok { e.Kind="call"; e.Operator="eager_left_to_right"; e.Value=l.expr(x.Fun); for _,a:=range x.Args { e.Arguments=append(e.Arguments,SemanticArgument{Value:l.expr(a)}) }; return e }
		return l.helperCall(x)
	case *ast.CompositeLit:
		e.Kind="aggregate"; for _,a:=range x.Elts { e.Arguments=append(e.Arguments,SemanticArgument{Value:l.expr(a)}) }; return e
	case *ast.IndexExpr:
		e.Kind="index"; e.Value=l.expr(x.X); e.Arguments=[]SemanticArgument{{Value:l.expr(x.Index)}}; return e
	case *ast.SliceExpr:
		e.Kind="index"; e.Value=l.expr(x.X); if x.Low!=nil { e.Arguments=append(e.Arguments,SemanticArgument{Value:l.expr(x.Low)}) } else { e.Arguments=append(e.Arguments,SemanticArgument{Missing:true}) }; if x.High!=nil { e.Arguments=append(e.Arguments,SemanticArgument{Value:l.expr(x.High)}) } else { e.Arguments=append(e.Arguments,SemanticArgument{Missing:true}) }; if x.Slice3&&x.Max!=nil { e.Arguments=append(e.Arguments,SemanticArgument{Value:l.expr(x.Max)}) }; return e
	case *ast.FuncLit:
		fn:=&SemanticFunction{}; if x.Type.Params!=nil { for _,f:=range x.Type.Params.List { for _,p:=range f.Names { fn.Parameters=append(fn.Parameters,SemanticParameter{Name:l.name(p),Passing:"value"}) } } }; prev:=l.inFunction; l.inFunction=true; fn.Body=l.stmt(x.Body); l.inFunction=prev; e.Kind="function"; e.Function=fn; return e
	case *ast.ParenExpr:
		return l.expr(x.X)
	case *ast.Ident:
		e.Kind = "identifier"
		e.Name = l.name(x)
	case *ast.SelectorExpr:
		// Selectors remain ordinary callee expressions; the shared call
		// projector normalizes qualified names through its target contract.
		e.Kind = "identifier"
		if pkg, ok := x.X.(*ast.Ident); ok { e.Name = pkg.Name + "." + x.Sel.Name } else { e.Name = x.Sel.Name }
	case *ast.UnaryExpr:
		if x.Op != gotoken.NOT {
			l.fail(n, "unary operation")
			return e
		}
		e.Kind = "unary"
		e.Operator = "!"
		e.Value = l.expr(x.X)
	case *ast.BinaryExpr:
		if x.Op != gotoken.EQL && x.Op != gotoken.NEQ && x.Op != gotoken.LAND && x.Op != gotoken.LOR && x.Op != gotoken.ADD && x.Op != gotoken.SUB && x.Op != gotoken.MUL && x.Op != gotoken.QUO && x.Op != gotoken.REM {
			l.fail(n, "binary operation")
			return e
		}
		e.Kind = "binary"
		e.Operator = x.Op.String()
		e.Left = l.expr(x.X)
		e.Right = l.expr(x.Y)
	default:
		l.fail(n, fmt.Sprintf("expression %T", n))
	}
	return e
}

func (l *goScalarLowerer) stmt(n ast.Stmt) SemanticStatement {
	s := SemanticStatement{Source: l.span(n)}
	switch x := n.(type) {
	case *ast.ReturnStmt:
		if !l.inFunction || len(x.Results) > 1 {
			l.fail(n, "return outside helper or multiple results")
			break
		}
		s.Kind = "return"
		if len(x.Results) == 1 {
			s.Expression = l.expr(x.Results[0])
		}
	case *ast.BlockStmt:
		s.Kind = "block"
		for _, child := range x.List {
			s.Statements = append(s.Statements, l.stmt(child))
		}
	case *ast.EmptyStmt:
		s.Kind = "block"
	case *ast.AssignStmt:
		if len(x.Lhs) != 1 || len(x.Rhs) != 1 || (x.Tok != gotoken.ASSIGN && x.Tok != gotoken.DEFINE) {
			l.fail(n, "parallel or compound assignment")
			break
		}
		ident, ok := x.Lhs[0].(*ast.Ident)
		if !ok {
			if idx, yes := x.Lhs[0].(*ast.IndexExpr); yes {
				s.Kind = "expression"
				s.Expression = &SemanticExpression{Kind:"call", Operator:"eager_left_to_right", Value:&SemanticExpression{Kind:"identifier",Name:"__index_set"}, Arguments:[]SemanticArgument{{Value:l.expr(idx.X)},{Value:l.expr(idx.Index)},{Value:l.expr(x.Rhs[0])}}, Source:l.span(n)}
				break
			}
			l.fail(n, "nonlocal assignment")
			break
		}
		s.Kind = "assign"
		s.Name = l.name(ident)
		s.AssignOp = "<-"
		s.Expression = l.expr(x.Rhs[0])
	case *ast.DeclStmt:
		decl, ok := x.Decl.(*ast.GenDecl)
		if !ok || decl.Tok != gotoken.VAR || len(decl.Specs) != 1 {
			l.fail(n, "declaration group or constant declaration")
			break
		}
		spec := decl.Specs[0].(*ast.ValueSpec)
		if len(spec.Names) != 1 || len(spec.Values) > 1 {
			l.fail(n, "multiple declarations")
			break
		}
		ident := spec.Names[0]
		object := l.info.Defs[ident]
		if object == nil || !l.supportedValue(object.Type()) {
			l.fail(n, "non-scalar declared type")
			break
		}
		s.Kind = "assign"
		s.AssignOp = "<-"
		s.Name = l.name(ident)
		if spec.Type != nil {
			typ := l.types[s.Name]
			typ.TypeOrigin = "explicit"
			l.types[s.Name] = typ
		}
		if len(spec.Values) == 1 {
			s.Expression = l.expr(spec.Values[0])
		} else {
			s.Expression = &SemanticExpression{Kind: "literal", LiteralKind: "string", Text: "\"\"", Source: l.span(spec)}
			if typ, ok := nativeFixedInteger(object.Type()); ok {
				s.Expression = l.integerOperation(spec, "integer.literal", typ)
				s.Expression.Operation.Text = "0"
			}
			if object.Type().Underlying().(*types.Basic).Info()&types.IsBoolean != 0 {
				s.Expression.LiteralKind = "boolean"
				s.Expression.Text = "FALSE"
			}
		}
	case *ast.IfStmt:
		if x.Init != nil {
			l.fail(n, "if initializer")
			break
		}
		s.Kind = "if"
		s.Condition = l.expr(x.Cond)
		then := l.stmt(x.Body)
		s.Then = &then
		if x.Else != nil {
			other := l.stmt(x.Else)
			s.Else = &other
		}
	case *ast.ForStmt:
		if x.Init != nil || x.Post != nil || x.Cond == nil {
			l.fail(n, "only condition-only for loops are supported")
			break
		}
		s.Kind = "while"
		s.Condition = l.expr(x.Cond)
		l.loopDepth++
		body := l.stmt(x.Body)
		l.loopDepth--
		s.Body = &body
	case *ast.RangeStmt:
		value, ok := x.Value.(*ast.Ident)
		if !ok { l.fail(n, "range value binding"); break }
		s.Kind = "for"
		s.Name = l.name(value)
		s.Sequence = l.expr(x.X)
		l.loopDepth++
		body := l.stmt(x.Body)
		l.loopDepth--
		s.Body = &body
	case *ast.BranchStmt:
		if x.Label != nil || l.loopDepth == 0 || (x.Tok != gotoken.BREAK && x.Tok != gotoken.CONTINUE) {
			l.fail(n, "branch or label")
			break
		}
		s.Kind = "break"
		if x.Tok == gotoken.CONTINUE {
			s.Kind = "continue"
		}
	case *ast.ExprStmt:
		call, ok := x.X.(*ast.CallExpr)
		if !ok || call.Ellipsis.IsValid() {
			l.fail(n, "expression statement requires a non-variadic call")
			break
		}
		s.Kind = "expression"
		s.Expression = l.expr(call)
	default:
		l.fail(n, fmt.Sprintf("statement %T", n))
	}
	return s
}
