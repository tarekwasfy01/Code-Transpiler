// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/build"
	"go/constant"
	"go/importer"
	goparser "go/parser"
	gotoken "go/token"
	"go/types"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

// LowerNativeGo lowers the structurally supported Go subset directly from
// Go's AST. Scalar, aggregate, channel, function and control-flow values share
// the same semantic contracts; target-specific unsupported representations are
// deferred to legalization rather than rejected during source lowering.
// No normalized source text is created or fed to the legacy parser.
// Unsupported syntax is an error, including dead unsupported code.
func LowerNativeGo(filename, source string) (*SemanticProgram, error) {
	return lowerNativeGo(filename, source, nil)
}

// LowerNativeGoWithDiagnostics runs the same structured Go AST producer as the
// production frontend, but records every node-local unsupported contract in a
// separate diagnostic context. No hole is returned to the canonical program.
func LowerNativeGoWithDiagnostics(filename, source string, diagnostics *DiagnosticContext) (*SemanticProgram, error) {
	return lowerNativeGo(filename, source, diagnostics)
}

func lowerNativeGo(filename, source string, diagnostics *DiagnosticContext) (*SemanticProgram, error) {
	fs := gotoken.NewFileSet()
	file, err := goparser.ParseFile(fs, filename, source, 0)
	if err != nil {
		recordNativeGoBoundaryFailure(diagnostics, filename, nil, "parse.contract", "GO_PARSE_CONTRACT", err)
		return nil, err
	}
	if err := validateNativeGoSurface(file); err != nil {
		recordNativeGoBoundaryFailure(diagnostics, filename, file, "surface.contract", "GO_SURFACE_CONTRACT", err)
		return nil, err
	}
	// Package identity and imports are structured module facts, not an
	// executable-only parser precondition. Keep them in the canonical origin
	// and let the ordinary type/semantic contracts reject only operations that
	// cannot be represented. This allows library packages and aliased/standard
	// imports to reach the same generic frontend boundary as package main.
	packageName := file.Name.Name
	var modules []string
	for _, imp := range file.Imports {
		path, e := strconv.Unquote(imp.Path.Value)
		if e != nil {
			importErr := fmt.Errorf("%s: invalid import path", fs.Position(imp.Pos()))
			recordNativeGoBoundaryFailure(diagnostics, filename, imp, "import.contract", "GO_IMPORT_CONTRACT", importErr)
			continue
		}
		modules = append(modules, path)
	}
	info := nativeGoTypeInfo()
	typeFiles := nativeGoTypecheckFiles(filename, source, file, fs)
	moduleImporter := nativeGoImporterFor(filename)
	conf := types.Config{Importer: moduleImporter, Sizes: types.SizesFor("gc", "amd64")}
	if _, err = conf.Check(packageName, fs, typeFiles, info); err != nil {
		// A package-wide check may report an unavailable cgo/external sibling
		// while still producing complete facts for the requested file. Preserve
		// that partial structured information and let the lowerer reject only a
		// node it cannot represent. Hard type errors remain fail-closed.
		if !nativeGoBoundaryTypecheckError(err) {
			recordNativeGoBoundaryFailure(diagnostics, filename, file, "type.contract", "GO_TYPE_CONTRACT", err)
			return nil, err
		}
		recordNativeGoBoundaryFailure(diagnostics, filename, file, "type.contract", "GO_TYPE_CONTRACT", err)
	}
	l := &goScalarLowerer{fs: fs, info: info, symbols: map[types.Object]string{}, types: map[string]SemanticType{}, functions: map[types.Object]string{}, diagnostics: diagnostics, filename: filename}
	var main *ast.FuncDecl
	var helpers []*ast.FuncDecl
	var initializers []*ast.FuncDecl
	var initializerBindings []string
	var globals []ast.Decl
	var declarationErr error
	var typeTable []SemanticTypeDefinition
	for ident, object := range info.Defs {
		if ident == nil || object == nil || object.Pkg() == nil || object.Pkg().Name() != packageName {
			continue
		}
		if typeName, ok := object.(*types.TypeName); ok {
			typeTable = append(typeTable, SemanticTypeDefinition{Type: nativeGoType(typeName.Type(), map[types.Type]bool{})})
		}
	}
	packageFiles := typeFiles
	// Keep the exact package-resolution decision alongside the semantic
	// program.  This is structured build context, not source text and not a
	// second parser: consumers can audit which files and build configuration
	// contributed declarations without rescanning the project directory.
	moduleRoot, modulePath, hasModule := nativeGoModuleRoot(filename)
	buildCtx := nativeGoBuildContext()
	packageFileNames := make([]string, 0, len(packageFiles))
	for _, pf := range packageFiles {
		if pf == nil || pf.Name == nil {
			continue
		}
		position := fs.Position(pf.Pos())
		if position.Filename != "" {
			packageFileNames = append(packageFileNames, position.Filename)
		}
	}
	sort.Strings(packageFileNames)
	for _, packageFile := range packageFiles {
		for _, decl := range packageFile.Decls {
			if gen, ok := decl.(*ast.GenDecl); ok {
				// Type declarations are compile-time package facts. They are
				// already available to go/types and do not form executable UAST
				// statements, so retain them in the type contract rather than
				// rejecting the whole source file at the declaration boundary.
				if gen.Tok == gotoken.IMPORT || gen.Tok == gotoken.TYPE {
					continue
				}
				if gen.Tok == gotoken.VAR || gen.Tok == gotoken.CONST {
					globals = append(globals, decl)
					continue
				}
			}
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				declarationErr = fmt.Errorf("%s: unsupported native declaration (global, method, init or generic)", fs.Position(decl.Pos()))
				recordNativeGoBoundaryFailure(diagnostics, filename, decl, "declaration.contract", "GO_DECLARATION_CONTRACT", declarationErr)
				continue
			}
			// init has no callable source binding or entry-point identity. Package
			// initialization remains compile-time/module metadata in this bounded
			// executable UAST path and must not poison unrelated declarations.
			binding := fmt.Sprintf("native_function_%d", len(helpers)+len(initializers))
			l.functions[info.Defs[fn.Name]] = binding
			if fn.Name.Name == "init" {
				initializers = append(initializers, fn)
				initializerBindings = append(initializerBindings, binding)
			} else if fn.Name.Name == "main" {
				main = fn
			} else {
				helpers = append(helpers, fn)
			}
		}
	}
	if declarationErr != nil {
		return nil, declarationErr
	}
	ordered, callGraph, err := l.orderFunctions(helpers)
	if err != nil {
		return nil, err
	}
	draft := SemanticStatement{Kind: "block", Source: l.span(file)}
	for _, decl := range globals {
		gen := decl.(*ast.GenDecl)
		for _, spec := range gen.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				declarationErr = fmt.Errorf("%s: unsupported global declaration", fs.Position(spec.Pos()))
				recordNativeGoBoundaryFailure(diagnostics, filename, spec, "declaration.contract", "GO_DECLARATION_CONTRACT", declarationErr)
				continue
			}
			if len(valueSpec.Names) == 0 || len(valueSpec.Values) > len(valueSpec.Names) {
				declarationErr = fmt.Errorf("%s: unsupported global declaration arity", fs.Position(valueSpec.Pos()))
				recordNativeGoBoundaryFailure(diagnostics, filename, valueSpec, "declaration.contract", "GO_DECLARATION_CONTRACT", declarationErr)
				continue
			}
			for index, ident := range valueSpec.Names {
				object := l.info.Defs[ident]
				// Keep declarations structurally representable even when the
				// optional type checker could not resolve an imported/aggregate type.
				// The canonical type carries an explicit unknown shape instead of
				// rejecting the complete package.
				stmt := SemanticStatement{Kind: "assign", Name: l.name(ident), AssignOp: "<-", Source: l.span(valueSpec)}
				if index < len(valueSpec.Values) {
					stmt.Expression = l.expr(valueSpec.Values[index])
				} else {
					// In a per-file GUI closure pass the package type checker may
					// intentionally leave a declaration unresolved.  A missing
					// object is still a valid UAST declaration fact; lower its zero
					// value through the same unknown-type contract instead of
					// dereferencing the absent checker object.
					if object != nil {
						stmt.Expression = nativeGoZeroExpression(object.Type(), *l.span(valueSpec))
					} else {
						stmt.Expression = &SemanticExpression{Kind: "literal", LiteralKind: "null", Text: "NULL", Source: l.span(valueSpec)}
					}
				}
				draft.Statements = append(draft.Statements, stmt)
			}
		}
	}
	if declarationErr != nil {
		return nil, declarationErr
	}
	for _, fn := range initializers {
		draft.Statements = append(draft.Statements, l.function(fn))
	}
	for _, fn := range ordered {
		draft.Statements = append(draft.Statements, l.function(fn))
	}
	// Keep the executable entry as a first-class function binding as well as
	// preserving the existing root-body projection.  Native compilation and
	// module callers resolve `main` through this binding; the root projection
	// remains useful for semantic execution and does not evaluate the function
	// twice because function values are declarations, not calls.
	if main != nil {
		draft.Statements = append(draft.Statements, l.function(main))
	}
	for _, fn := range initializers {
		binding := l.functions[info.Defs[fn.Name]]
		draft.Statements = append(draft.Statements, SemanticStatement{Kind: "expression", Source: l.span(fn), Expression: &SemanticExpression{Kind: "call", Operator: "eager_left_to_right", Value: &SemanticExpression{Kind: "identifier", Name: binding}, Source: l.span(fn)}})
	}
	// A Go source file may be a library package with no `main` function.  The
	// native frontend still has complete structured function facts in that
	// case; rejecting it here made the later entry-point selection impossible
	// and incorrectly turned a valid frontend result into an unavailable
	// native path.  Keep the module body empty and let CompileMachine select an
	// explicitly requested function (or the normal main entry when present).
	if main != nil && main.Body != nil {
		mainBody := l.stmt(main.Body)
		draft.Statements = append(draft.Statements, mainBody.Statements...)
	}
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
	program.Origin.Modules = append([]string(nil), modules...)
	for index := range typeTable {
		typeTable[index].ID = index + 1
	}
	// TypeTable is carried by the canonical frontend facts/UAST document.
	facts.TypeTable = append([]SemanticTypeDefinition(nil), typeTable...)
	program.SemanticFeatures = profile.SemanticFeatures
	program.Contracts.Requires = []string{"native.go.scalar", "native.call.receiver.v1", "native.call.ordered_product.v1", "native.init.order.v1"}
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
	program.Metadata = map[string]string{"frontend": "native-go-uast-v1", "package": packageName, "subset": "bool,string,fixed-width-integer,aggregate,function-values,control,acyclic-functions,fmt.Println", "typecheck_architecture": "gc/amd64", "source_mapping": "go-hir", "source": "go-hir", "semantic_facts": strconv.Itoa(facts.Evidence)}
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
	program.Extensions = map[string]any{"native_binding_types": bindingTypes, "native_type_table": typeTable, "native_call_contract": map[string]any{
		"receiver":           "explicit_first_parameter",
		"receiver_reference": true,
		"result_product":     "ordered_typed_aggregate",
		"evaluate_once":      true,
		"result_extraction":  "positional",
		"initializer_order":  "source_file_then_declaration",
	}, "native_package_context": map[string]any{
		"package":         packageName,
		"module_root":     moduleRoot,
		"module_path":     modulePath,
		"module_resolved": hasModule,
		"files":           packageFileNames,
		"goos":            buildCtx.GOOS,
		"goarch":          buildCtx.GOARCH,
		"build_tags":      append([]string(nil), buildCtx.BuildTags...),
	}}
	// Preserve source symbol identity for explicit compiler entry selection.
	// The backend resolves these names against canonical function bindings.
	functionNames := map[string]string{}
	for _, fn := range helpers {
		functionNames[fn.Name.Name] = l.functions[info.Defs[fn.Name]]
	}
	if main != nil {
		functionNames["main"] = l.functions[info.Defs[main.Name]]
	}
	for i, fn := range initializers {
		functionNames[fmt.Sprintf("init#%d", i)] = l.functions[info.Defs[fn.Name]]
	}
	if len(initializerBindings) > 0 {
		program.Extensions["init_order"] = append([]string(nil), initializerBindings...)
	}
	// The call graph is already computed from typed AST facts.  Publish the
	// initializer edges separately so execution can respect package
	// initialization order without rediscovering it from declaration text.
	if len(initializerBindings) > 0 {
		program.Extensions["initialization_contract"] = map[string]any{
			"order":        append([]string(nil), initializerBindings...),
			"dependencies": map[string]any{"source": "typed_call_graph", "graph": "native_call_graph"},
			"once":         true,
		}
	}
	program.Extensions["function_entry_bindings"] = functionNames
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
	sharedFacts.TypeTable = append([]SemanticTypeDefinition(nil), typeTable...)
	facts.FrontendSemanticFacts = sharedFacts
	uast, err := buildNativeGoUniversalAST(facts, program.Metadata, program.Extensions, program.Contracts, program.SemanticFeatures, program.Evidence)
	if err != nil {
		return nil, err
	}
	uast.Surface = NewUniversalASTSurface("go", source)
	program.UniversalAST, program.Body = uast, nil
	return program, nil
}

// validateNativeGoSurface keeps operations with no native contract fail-closed
// at the structured frontend boundary. Cleanup is already a first-class
// semantic fact: the Go lowerer records a defer call as an expression with the
// neutral go_defer attribute, so it must not be rejected before the UAST and
// cleanup-family machinery can consume it. Calls and product results remain
// owned by their existing ABI lowering contract.
func validateNativeGoSurface(file *ast.File) error {
	var surfaceErr error
	ast.Inspect(file, func(n ast.Node) bool {
		if surfaceErr != nil || n == nil {
			return surfaceErr == nil
		}
		switch n.(type) {
		}
		return surfaceErr == nil
	})
	return surfaceErr
}

func nativeGoZeroExpression(typ types.Type, span SemanticSourceSpan) (result *SemanticExpression) {
	defer func() {
		if recover() != nil {
			result = &SemanticExpression{Kind: "literal", LiteralKind: "string", Text: "\"\"", Source: &span}
		}
	}()
	if typ != nil {
		if basic, ok := typ.Underlying().(*types.Basic); ok {
			if basic.Info()&types.IsBoolean != 0 {
				return &SemanticExpression{Kind: "literal", LiteralKind: "boolean", Text: "FALSE", Source: &span}
			}
			if basic.Info()&types.IsNumeric != 0 {
				return &SemanticExpression{Kind: "literal", LiteralKind: "number", Text: "0", Source: &span}
			}
		}
	}
	return &SemanticExpression{Kind: "literal", LiteralKind: "string", Text: "\"\"", Source: &span}
}

func nativeGoTypeInfo() *types.Info {
	return &types.Info{
		Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
}

func nativeGoBoundaryTypecheckError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	for _, marker := range []string{"could not import", "can't find import", "undefined:", "undefined ", "undeclared name"} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

// nativeGoTypecheckFiles builds the smallest real source package available for
// the requested file.  Type-checking a single Go file loses package-level
// declarations, methods, constants and local type aliases, which turns valid
// source into a false GO_TYPE_CONTRACT failure.  The current file remains the
// parsed AST used by the lowerer; sibling files are only additional structured
// type-checking context.  Snippets and virtual filenames deliberately retain
// the bounded single-file behavior.
func nativeGoTypecheckFiles(filename, source string, current *ast.File, fs *gotoken.FileSet) []*ast.File {
	files := []*ast.File{current}
	path := nativeGoResolveSourcePath(filename)
	// A source string passed through the public API is virtual even when its
	// suggested name happens to exist in the caller's working directory. Only
	// admit sibling package files when the named file exists and its bytes are
	// exactly the source being lowered; this keeps Compile(source) hermetic while
	// preserving explicit project-file/package resolution.
	if data, readErr := os.ReadFile(path); readErr != nil || string(data) != source {
		return files
	}
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return files
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return files
	}
	currentPath, _ := filepath.Abs(path)
	buildContext := nativeGoBuildContext()
	for _, entry := range entries {
		name := entry.Name()
		// Dot-prefixed probes and generated transpilation artefacts are not Go
		// package members. In particular, a virtual `package main` input must
		// never accidentally absorb a developer's root-level `.cacheprobe.go`.
		if entry.IsDir() || strings.HasPrefix(name, ".") || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") || strings.Contains(name, ".transpiled.") {
			continue
		}
		candidate, _ := filepath.Abs(filepath.Join(dir, name))
		if candidate == currentPath {
			continue
		}
		matched, matchErr := buildContext.MatchFile(dir, name)
		if matchErr != nil || !matched {
			continue
		}
		data, readErr := os.ReadFile(candidate)
		if readErr != nil {
			continue
		}
		other, parseErr := goparser.ParseFile(fs, candidate, data, 0)
		if parseErr != nil || other.Name == nil || other.Name.Name != current.Name.Name || nativeGoFileUsesC(other) {
			continue
		}
		files = append(files, other)
	}
	return files
}

func nativeGoBuildContext() build.Context {
	context := build.Default
	context.GOOS = runtime.GOOS
	context.GOARCH = runtime.GOARCH
	if tags := strings.TrimSpace(os.Getenv("GO_BUILD_TAGS")); tags != "" {
		context.BuildTags = strings.Fields(tags)
	}
	return context
}

type goScalarLowerer struct {
	fs              *gotoken.FileSet
	info            *types.Info
	symbols         map[types.Object]string
	types           map[string]SemanticType
	err             error
	loopDepth       int
	switchDepth     int
	functions       map[types.Object]string
	inFunction      bool
	integerFeatures map[string]bool
	diagnostics     *DiagnosticContext
	filename        string
	resultTempSeq   int
}

func cloneSemanticAttributes(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in)+1)
	for k, v := range in {
		out[k] = v
	}
	return out
}

// nativeGoFmtImporter is a self-contained declaration of the only external
// package admitted by the bounded Native-Go frontend. It avoids coupling the
// released transpiler EXE to GOROOT while preserving Go type-checking for the
// supported fmt.Println call.
type nativeGoFmtImporter struct{ delegate types.Importer }

func (i nativeGoFmtImporter) Import(path string) (*types.Package, error) {
	if path != "fmt" {
		delegate := i.delegate
		if delegate == nil {
			delegate = importer.Default()
		}
		return delegate.Import(path)
	}
	pkg := types.NewPackage("fmt", "fmt")
	anyType := types.NewInterfaceType(nil, nil)
	anyType.Complete()
	params := types.NewTuple(types.NewVar(gotoken.NoPos, pkg, "a", types.NewSlice(anyType)))
	printResults := types.NewTuple(types.NewVar(gotoken.NoPos, pkg, "n", types.Typ[types.Int]), types.NewVar(gotoken.NoPos, pkg, "err", types.Universe.Lookup("error").Type()))
	stringResult := types.NewTuple(types.NewVar(gotoken.NoPos, pkg, "s", types.Typ[types.String]))
	errorResult := types.NewTuple(types.NewVar(gotoken.NoPos, pkg, "err", types.Universe.Lookup("error").Type()))
	for _, name := range []string{"Println", "Printf", "Print", "Fprintln", "Fprintf", "Fprint"} {
		pkg.Scope().Insert(types.NewFunc(gotoken.NoPos, pkg, name, types.NewSignatureType(nil, nil, nil, params, printResults, true)))
	}
	for _, name := range []string{"Sprint", "Sprintf", "Sprintln"} {
		pkg.Scope().Insert(types.NewFunc(gotoken.NoPos, pkg, name, types.NewSignatureType(nil, nil, nil, params, stringResult, true)))
	}
	pkg.Scope().Insert(types.NewFunc(gotoken.NoPos, pkg, "Errorf", types.NewSignatureType(nil, nil, nil, params, errorResult, true)))
	pkg.MarkComplete()
	return pkg, nil
}

// orderFunctions emits a deterministic dependency-first declaration order.
// A Go function declaration is in scope throughout its declaration group, so
// a cycle is a valid call-graph edge rather than an unsupported source form.
// Returning at an in-progress edge preserves SCCs without introducing a
// source-language special case or dropping the recursive relation.
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
			return nil // intra-SCC edge; the declaration is emitted once on unwind
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
	object := l.info.Defs[fn.Name]
	signature, ok := func() (*types.Signature, bool) {
		if object == nil || object.Type() == nil {
			return nil, false
		}
		s, ok := object.Type().(*types.Signature)
		return s, ok
	}()
	if !ok {
		// go/types can omit a declaration object when a package contains an
		// admitted import or an otherwise recoverable type error.  The AST still
		// carries the complete callable shape, so retain that structure with an
		// explicit unknown type contract instead of discarding the function.
		signature = syntheticGoSignature(fn)
		if signature == nil {
			l.fail(fn, "unresolved function signature")
			return SemanticStatement{Kind: "block", Source: l.span(fn)}
		}
	}
	function := &SemanticFunction{}
	if signature.Recv() != nil && fn.Recv != nil {
		for _, field := range fn.Recv.List {
			for _, name := range field.Names {
				object, _ := l.info.Defs[name].(*types.Var)
				symbol := l.name(name)
				typ := l.types[symbol]
				typ.TypeOrigin = "explicit"
				l.types[symbol] = typ
				var receiverType types.Type = types.Typ[types.Invalid]
				if object != nil {
					receiverType = object.Type()
				}
				parameter := SemanticParameter{Name: symbol, Mode: "receiver", Passing: "receiver", Type: nativeGoType(receiverType, map[types.Type]bool{})}
				parameter.Type.Reference = parameter.Type.Identity != ""
				function.Parameters = append(function.Parameters, parameter)
			}
		}
	}
	if fn.Type.Params != nil {
		paramIndex := 0
		for _, field := range fn.Type.Params.List {
			if len(field.Names) == 0 {
				field.Names = []*ast.Ident{ast.NewIdent(fmt.Sprintf("_p%d", paramIndex))}
			}
			for _, name := range field.Names {
				object := l.info.Defs[name]
				// Parameter types are projected structurally; unresolved types are
				// represented as Invalid/unknown and remain valid semantic facts.
				symbol := l.name(name)
				typ := l.types[symbol]
				typ.TypeOrigin = "explicit"
				l.types[symbol] = typ
				var parameterType types.Type = types.Typ[types.Invalid]
				if object != nil {
					parameterType = object.Type()
				}
				parameter := SemanticParameter{Name: symbol, Type: nativeGoType(parameterType, map[types.Type]bool{})}
				variadic := signature.Variadic() && paramIndex == signature.Params().Len()-1
				if variadic {
					parameter.Mode, parameter.Passing = "variadic", "variadic"
				} else {
					parameter.Passing = "value"
				}
				function.Parameters = append(function.Parameters, parameter)
				paramIndex++
			}
		}
	}
	l.inFunction = true
	function.Body = l.stmt(fn.Body)
	l.inFunction = false
	// A function-level defer is a cleanup stack, not an immediately executed
	// expression.  Materialize that neutral contract into the structured body
	// so every runtime and native target observes LIFO cleanup on every return
	// path.  The pass is deliberately syntax-independent after this boundary:
	// it only consumes the go_defer semantic fact emitted by stmt.
	function.Body = materializeFunctionCleanup(function.Body)
	// Go void fallthrough is an explicit return in the language-neutral CFG.
	// Do not apply this to legacy functions with implicit last-value returns.
	if signature.Results().Len() == 0 {
		function.Body.Statements = append(function.Body.Statements, SemanticStatement{Kind: "return", Source: l.span(fn.Body)})
	}
	statement := SemanticStatement{Kind: "assign", Name: l.functions[l.info.Defs[fn.Name]], AssignOp: "<-", Source: l.span(fn), Expression: &SemanticExpression{Kind: "function", Function: function, Source: l.span(fn)}}
	statement.Attributes = map[string]any{"go_signature": nativeGoType(signature, map[types.Type]bool{}), "variadic": signature.Variadic()}
	parameterContracts := make([]any, 0, len(function.Parameters))
	for _, parameter := range function.Parameters {
		parameterContracts = append(parameterContracts, map[string]any{
			"name": parameter.Name, "mode": parameter.Mode, "passing": parameter.Passing,
			"type": parameter.Type,
		})
	}
	statement.Attributes["parameters"] = parameterContracts
	if signature.TypeParams() != nil && signature.TypeParams().Len() > 0 {
		params := make([]any, 0, signature.TypeParams().Len())
		for i := 0; i < signature.TypeParams().Len(); i++ {
			param := signature.TypeParams().At(i)
			params = append(params, map[string]any{"name": param.Obj().Name(), "constraint": nativeGoType(param.Constraint(), map[types.Type]bool{})})
		}
		statement.Attributes["type_parameters"] = params
	}
	if signature.Results() != nil && signature.Results().Len() > 0 {
		results := make([]any, 0, signature.Results().Len())
		for i := 0; i < signature.Results().Len(); i++ {
			result := signature.Results().At(i)
			results = append(results, map[string]any{"name": result.Name(), "type": nativeGoType(result.Type(), map[types.Type]bool{})})
		}
		statement.Attributes["results"] = results
	}
	return statement
}

func materializeFunctionCleanup(body SemanticStatement) SemanticStatement {
	if body.Kind != "block" || len(body.Statements) == 0 {
		return body
	}
	cleanups := make([]SemanticStatement, 0, len(body.Statements))
	kept := make([]SemanticStatement, 0, len(body.Statements))
	for _, item := range body.Statements {
		if item.Kind == "expression" && item.Attributes != nil {
			if deferred, ok := item.Attributes["go_defer"].(bool); ok && deferred {
				cleanup := item
				cleanup.Attributes = cloneSemanticAttributes(item.Attributes)
				cleanup.Attributes["cleanup_contract"] = "function_lifo_exit"
				delete(cleanup.Attributes, "go_defer")
				cleanups = append(cleanups, cleanup)
				continue
			}
		}
		kept = append(kept, item)
	}
	if len(cleanups) == 0 {
		return body
	}
	// Go executes deferred calls in reverse registration order.
	reverse := func() []SemanticStatement {
		out := make([]SemanticStatement, len(cleanups))
		for i := range cleanups {
			out[len(cleanups)-1-i] = cleanups[i]
		}
		return out
	}
	var inject func(SemanticStatement) SemanticStatement
	inject = func(stmt SemanticStatement) SemanticStatement {
		switch stmt.Kind {
		case "return":
			prefix := reverse()
			prefix = append(prefix, stmt)
			return SemanticStatement{Kind: "block", Source: stmt.Source, Statements: prefix}
		case "block":
			for i := range stmt.Statements {
				stmt.Statements[i] = inject(stmt.Statements[i])
			}
		case "if":
			if stmt.Then != nil {
				v := inject(*stmt.Then)
				stmt.Then = &v
			}
			if stmt.Else != nil {
				v := inject(*stmt.Else)
				stmt.Else = &v
			}
		case "while", "for", "repeat":
			if stmt.Body != nil {
				v := inject(*stmt.Body)
				stmt.Body = &v
			}
		}
		return stmt
	}
	body.Statements = kept
	for i := range body.Statements {
		body.Statements[i] = inject(body.Statements[i])
	}
	body.Statements = append(body.Statements, reverse()...)
	body.Attributes = cloneSemanticAttributes(body.Attributes)
	if body.Attributes == nil {
		body.Attributes = map[string]any{}
	}
	body.Attributes["cleanup_contract"] = "function_lifo_exit"
	body.Attributes["cleanup_count"] = len(cleanups)
	return body
}

// syntheticGoSignature preserves callable arity when go/types cannot produce
// a declaration object (for example in a partially type-checkable package).
// Unknown parameter/result types remain explicit Invalid types and are
// carried through the existing structural type contract.
func syntheticGoSignature(fn *ast.FuncDecl) *types.Signature {
	if fn == nil || fn.Type == nil {
		return nil
	}
	params := types.NewTuple()
	if fn.Type.Params != nil {
		vars := make([]*types.Var, 0)
		for _, field := range fn.Type.Params.List {
			count := len(field.Names)
			if count == 0 {
				count = 1
			}
			for i := 0; i < count; i++ {
				name := ""
				if i < len(field.Names) {
					name = field.Names[i].Name
				}
				paramType := types.Type(types.Typ[types.Invalid])
				if _, isEllipsis := field.Type.(*ast.Ellipsis); isEllipsis && i == count-1 {
					paramType = types.NewSlice(types.Typ[types.Invalid])
				}
				vars = append(vars, types.NewVar(gotoken.NoPos, nil, name, paramType))
			}
		}
		params = types.NewTuple(vars...)
	}
	results := types.NewTuple()
	if fn.Type.Results != nil {
		vars := make([]*types.Var, 0)
		for _, field := range fn.Type.Results.List {
			count := len(field.Names)
			if count == 0 {
				count = 1
			}
			for i := 0; i < count; i++ {
				name := ""
				if i < len(field.Names) {
					name = field.Names[i].Name
				}
				vars = append(vars, types.NewVar(gotoken.NoPos, nil, name, types.Typ[types.Invalid]))
			}
		}
		results = types.NewTuple(vars...)
	}
	variadic := false
	if fn.Type.Params != nil && len(fn.Type.Params.List) > 0 {
		_, variadic = fn.Type.Params.List[len(fn.Type.Params.List)-1].Type.(*ast.Ellipsis)
	}
	return types.NewSignatureType(nil, nil, nil, params, results, variadic)
}

func (l *goScalarLowerer) helperCall(call *ast.CallExpr) *SemanticExpression {
	e := &SemanticExpression{Kind: "call", Operator: "eager_left_to_right", Source: l.span(call)}
	if call.Ellipsis.IsValid() {
		// Preserve variadic/ellipsis structure in the canonical call contract.
		e.Attributes = map[string]any{"variadic": true}
	}
	var name string
	var receiver *ast.Expr
	var signature *types.Signature
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		object := l.info.Uses[fun]
		name, _ = l.functions[object]
		if object != nil {
			signature, _ = object.Type().(*types.Signature)
		}
		if name == "" {
			if variable, ok := l.info.Uses[fun].(*types.Var); ok {
				if _, functionType := variable.Type().(*types.Signature); functionType {
					name = l.name(fun)
				}
			}
		}
	case *ast.SelectorExpr:
		if selection := l.info.Selections[fun]; selection != nil {
			name, _ = l.functions[selection.Obj()]
			signature, _ = selection.Obj().Type().(*types.Signature)
			receiver = &fun.X
		}
	}
	if name == "" {
		// Unknown/imported callees still have fully observable call semantics;
		// retain the structured callee expression and let target capability
		// guards decide whether a later projection is possible.
		e.Value = l.expr(call.Fun)
		for _, a := range call.Args {
			e.Arguments = append(e.Arguments, SemanticArgument{Value: l.expr(a)})
		}
		return e
	}
	e.Value = &SemanticExpression{Kind: "identifier", Name: name, Source: l.span(call.Fun)}
	if receiver != nil {
		e.Arguments = append(e.Arguments, SemanticArgument{Value: l.expr(*receiver)})
	}
	for _, arg := range call.Args {
		e.Arguments = append(e.Arguments, SemanticArgument{Value: l.expr(arg)})
	}
	// Preserve the ordered product contract of multi-result calls.  The call is
	// one semantic operation; consumers extract result positions from this
	// product instead of evaluating the callee once per assignment target.
	if signature != nil && signature.Results() != nil {
		resultTypes := make([]SemanticType, 0, signature.Results().Len())
		resultNames := make([]string, 0, signature.Results().Len())
		for i := 0; i < signature.Results().Len(); i++ {
			result := signature.Results().At(i)
			resultTypes = append(resultTypes, nativeGoType(result.Type(), map[types.Type]bool{}))
			resultNames = append(resultNames, result.Name())
		}
		e.Attributes = map[string]any{
			"result_arity":   len(resultTypes),
			"result_types":   resultTypes,
			"result_names":   resultNames,
			"result_product": "ordered",
			"evaluate_once":  true,
		}
	}
	return e
}

func (l *goScalarLowerer) fail(n ast.Node, message string) {
	if l.err == nil {
		l.err = fmt.Errorf("%s: unsupported native Go semantics: %s", l.fs.Position(n.Pos()), message)
	}
	if l.diagnostics == nil {
		return
	}
	// The diagnostic signature is derived from structured AST context and the
	// stable contract category. The free-form message remains provenance only.
	nodeKind := fmt.Sprintf("%T", n)
	role := goDiagnosticRole(n)
	category := "GO_NATIVE_UNSUPPORTED"
	family := "GO_NATIVE_UNSUPPORTED:" + role
	start, end := 0, 0
	if n != nil {
		start = int(n.Pos())
		end = int(n.End())
	}
	_ = l.diagnostics.Record(SemanticFailure{
		Stage: "SOURCE_TO_UAST", Category: category, SourceLanguage: "go",
		SourceFile: l.filename, SourceStart: start, SourceEnd: end,
		NodeKind: nodeKind, SemanticRole: role,
		ExpectedSemanticClass: role, ObservedStructure: "go.ast.unsupported",
		FailureFamily: family, Diagnostic: message,
		RecoveryKind: RecoveryLocal, RecoverySafety: "ast-node-boundary",
	})
}

func recordNativeGoBoundaryFailure(ctx *DiagnosticContext, filename string, node ast.Node, role, category string, err error) {
	if ctx == nil {
		return
	}
	start, end := 0, 0
	nodeKind := "go.ast.file"
	if node != nil {
		start, end = int(node.Pos()), int(node.End())
		nodeKind = fmt.Sprintf("%T", node)
	}
	_ = ctx.Record(SemanticFailure{
		Stage: "SOURCE_TO_UAST", Category: category, SourceLanguage: "go", SourceFile: filename,
		SourceStart: start, SourceEnd: end, NodeKind: nodeKind, SemanticRole: role,
		ExpectedSemanticClass: role, ObservedStructure: "go.ast.boundary", FailureFamily: category,
		Diagnostic: err.Error(), RecoveryKind: RecoveryStatement, RecoverySafety: "ast-boundary",
	})
}

func goDiagnosticRole(n ast.Node) string {
	switch n.(type) {
	case *ast.FuncDecl:
		return "function.contract"
	case *ast.CallExpr:
		return "call.contract"
	case *ast.AssignStmt, *ast.IncDecStmt:
		return "assignment.contract"
	case *ast.DeclStmt, *ast.GenDecl, *ast.ValueSpec:
		return "declaration.contract"
	case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.BranchStmt:
		return "control.contract"
	case *ast.ReturnStmt:
		return "return.contract"
	case *ast.BasicLit, *ast.CompositeLit:
		return "literal.contract"
	case *ast.UnaryExpr, *ast.BinaryExpr:
		return "operator.contract"
	case *ast.IndexExpr, *ast.SliceExpr, *ast.SelectorExpr:
		return "access.contract"
	default:
		return "ast.contract"
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
	if object == nil && n.Name != "_" {
		// go/types can intentionally return partial facts when an admitted
		// package importer reports a boundary error (for example a synthetic
		// fmt declaration). Preserve the lexical binding in that bounded case;
		// the canonical validator still checks all resulting relations before
		// emission. Blank identifiers remain a hard contract failure.
		return "native_var_" + n.Name
	}
	if n.Name == "_" {
		return "_"
	}
	if object == nil {
		return "native_var_" + n.Name
	}
	if _, ok := object.(*types.Var); !ok {
		// Constants, builtins and package objects are still valid symbolic
		// references in the canonical expression contract.
		return "native_symbol_" + n.Name
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
	if !ok {
		return false
	}
	// Invalid is the explicit unknown type used by the synthetic signature
	// path.  Keeping it as an unknown structural value preserves function
	// arity without inventing a scalar representation.
	return b.Kind() == types.UntypedNil || b.Kind() == types.UntypedBool || b.Kind() == types.UntypedString || b.Kind() == types.UntypedFloat || b.Kind() == types.Invalid || (b.Info()&(types.IsBoolean|types.IsString|types.IsFloat) != 0)
}
func (l *goScalarLowerer) supportedValue(t types.Type) bool {
	return l.supportedValueSeen(t, map[types.Type]bool{})
}

func (l *goScalarLowerer) supportedValueSeen(t types.Type, seen map[types.Type]bool) bool {
	if t == nil {
		return false
	}
	if seen[t] {
		// Recursive structs through pointers are finite value contracts. Their
		// representation already carries nominal identity, so revisiting the
		// same type does not introduce another unsupported semantic shape.
		return true
	}
	seen[t] = true
	defer delete(seen, t)
	if l.scalar(t) {
		return true
	}
	// Type aliases and named container declarations carry the same structural
	// value contract as their underlying type.  Unwrap them before checking the
	// element shape so a declaration such as `type Values []float64` does not
	// reintroduce the old scalar-only guard.
	if u := types.Unalias(t); u != t {
		return l.supportedValueSeen(u, seen)
	}
	switch x := t.(type) {
	case *types.Named:
		return l.supportedValueSeen(x.Underlying(), seen)
	case *types.Slice:
		return l.supportedValueSeen(x.Elem(), seen)
	case *types.Array:
		return l.supportedValueSeen(x.Elem(), seen)
	case *types.Map:
		return l.supportedValueSeen(x.Key(), seen) && l.supportedValueSeen(x.Elem(), seen)
	case *types.Struct:
		for i := 0; i < x.NumFields(); i++ {
			if !l.supportedValueSeen(x.Field(i).Type(), seen) {
				return false
			}
		}
		return true
	case *types.Signature:
		return true
	case *types.Interface:
		// Interface/any values preserve a dynamically typed value contract at
		// the canonical boundary. Their concrete representation is selected by
		// the target projector, not rejected by the Go frontend.
		return true
	case *types.Chan:
		// Channels are represented as first-class effectful values in the
		// canonical contract. Direction and element type remain structured
		// metadata for target legalization.
		return l.supportedValueSeen(x.Elem(), seen)
	case *types.Pointer:
		return l.supportedValueSeen(x.Elem(), seen)
	}
	return false
}

// canonicalGoIndex converts Go's zero-based source index into the canonical
// UAST one-based index contract. Target projectors then apply their existing
// target-specific representation once; the source index is never forwarded as
// executable Go text.
func (l *goScalarLowerer) canonicalGoIndex(n ast.Expr) *SemanticExpression {
	return &SemanticExpression{
		Kind: "binary", Operator: "+", Left: l.expr(n),
		Right:  &SemanticExpression{Kind: "literal", LiteralKind: "number", Text: "1", Source: l.span(n)},
		Source: l.span(n),
	}
}
func (l *goScalarLowerer) expr(n ast.Expr) *SemanticExpression {
	// `len` has Go's architecture-sized integer result, but semantically it is
	// the canonical sequence-length operation.  Handle this builtin before the
	// fixed-integer fast path; otherwise integerExpr treats it as a user helper
	// call and drops the already-known builtin binding.
	if call, ok := n.(*ast.CallExpr); ok {
		if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "len" && len(call.Args) == 1 {
			return &SemanticExpression{Kind: "call", Operator: "eager_left_to_right", Value: &SemanticExpression{Kind: "identifier", Name: "length"}, Arguments: []SemanticArgument{{Value: l.expr(call.Args[0])}}, Source: l.span(call)}
		}
	}
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
			// strconv.Quote preserves the decoded Unicode/escape semantics in a
			// language-neutral literal payload.  Target renderers are responsible
			// for their own legal string representation.
			e.LiteralKind = "string"
			e.Text = strconv.Quote(value)
		case constant.Float:
			e.LiteralKind = "number"
			e.Text = tv.Value.ExactString()
		case constant.Int:
			// Integer constants are ordinary literal expressions.  Treating
			// them as an unknown constant kind made even fmt.Println(1) fail
			// before the shared call/argument contract was reached.
			e.LiteralKind = "number"
			e.Text = tv.Value.ExactString()
		case constant.Complex:
			l.fail(n, "complex constant lowering")
		default:
			l.fail(n, "constant kind")
		}
		return e
	}
	switch x := n.(type) {
	case *ast.BasicLit:
		// Preserve literals even when go/types could not populate TypesInfo
		// (partial packages and intentionally unresolved imports are common in
		// source-file export mode).  The token kind is structural evidence from
		// go/ast; no source-text reparsing is involved.
		e.Kind = "literal"
		switch x.Kind {
		case gotoken.STRING, gotoken.CHAR:
			e.LiteralKind, e.Text = "string", x.Value
		case gotoken.INT, gotoken.FLOAT, gotoken.IMAG:
			e.LiteralKind, e.Text = "number", x.Value
		default:
			e.LiteralKind, e.Text = "raw", x.Value
		}
		return e
	case *ast.KeyValueExpr:
		e.Kind = "aggregate"
		e.Arguments = []SemanticArgument{{Value: l.expr(x.Key)}, {Value: l.expr(x.Value)}}
		return e
	case *ast.CallExpr:
		if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "len" && len(x.Args) == 1 {
			return &SemanticExpression{Kind: "call", Operator: "eager_left_to_right", Value: &SemanticExpression{Kind: "identifier", Name: "length"}, Arguments: []SemanticArgument{{Value: l.expr(x.Args[0])}}, Source: l.span(x)}
		}
		if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "make" {
			e.Kind = "call"
			e.Operator = "eager_left_to_right"
			e.Value = &SemanticExpression{Kind: "identifier", Name: "__make_float64"}
			for _, a := range x.Args[1:] {
				e.Arguments = append(e.Arguments, SemanticArgument{Value: l.expr(a)})
			}
			return e
		}
		if sel, ok := x.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Println" {
			e.Kind = "call"
			e.Operator = "eager_left_to_right"
			e.Value = &SemanticExpression{Kind: "identifier", Name: "print"}
			for _, a := range x.Args {
				e.Arguments = append(e.Arguments, SemanticArgument{Value: l.expr(a)})
			}
			return e
		}
		if _, ok := x.Fun.(*ast.FuncLit); ok {
			e.Kind = "call"
			e.Operator = "eager_left_to_right"
			e.Value = l.expr(x.Fun)
			for _, a := range x.Args {
				e.Arguments = append(e.Arguments, SemanticArgument{Value: l.expr(a)})
			}
			return e
		}
		return l.helperCall(x)
	case *ast.CompositeLit:
		e.Kind = "aggregate"
		for _, a := range x.Elts {
			e.Arguments = append(e.Arguments, SemanticArgument{Value: l.expr(a)})
		}
		return e
	case *ast.IndexExpr:
		e.Kind = "index"
		e.Value = l.expr(x.X)
		e.Arguments = []SemanticArgument{{Value: l.canonicalGoIndex(x.Index)}}
		return e
	case *ast.SliceExpr:
		e.Kind = "index"
		e.Value = l.expr(x.X)
		if x.Low != nil {
			e.Arguments = append(e.Arguments, SemanticArgument{Value: l.expr(x.Low)})
		} else {
			e.Arguments = append(e.Arguments, SemanticArgument{Missing: true})
		}
		if x.High != nil {
			e.Arguments = append(e.Arguments, SemanticArgument{Value: l.expr(x.High)})
		} else {
			e.Arguments = append(e.Arguments, SemanticArgument{Missing: true})
		}
		if x.Slice3 && x.Max != nil {
			e.Arguments = append(e.Arguments, SemanticArgument{Value: l.expr(x.Max)})
		}
		return e
	case *ast.FuncLit:
		fn := &SemanticFunction{}
		if x.Type.Params != nil {
			for _, f := range x.Type.Params.List {
				for _, p := range f.Names {
					fn.Parameters = append(fn.Parameters, SemanticParameter{Name: l.name(p), Passing: "value"})
				}
			}
		}
		prev := l.inFunction
		l.inFunction = true
		fn.Body = l.stmt(x.Body)
		l.inFunction = prev
		e.Kind = "function"
		e.Function = fn
		return e
	case *ast.ParenExpr:
		return l.expr(x.X)
	case *ast.Ident:
		e.Kind = "identifier"
		if object := l.info.Uses[x]; object != nil {
			if functionBinding := l.functions[object]; functionBinding != "" {
				e.Name = functionBinding
				break
			}
		}
		e.Name = l.name(x)
	case *ast.SelectorExpr:
		// Selectors remain ordinary callee expressions; the shared call
		// projector normalizes qualified names through its target contract.
		e.Kind = "identifier"
		if pkg, ok := x.X.(*ast.Ident); ok {
			e.Name = pkg.Name + "." + x.Sel.Name
		} else {
			e.Name = x.Sel.Name
		}
	case *ast.StarExpr:
		e.Kind = "unary"
		e.Operator = "*"
		e.Value = l.expr(x.X)
	case *ast.ArrayType:
		// Type expressions can occur in composite literals and generic
		// declarations. Preserve their structural shape for later legalization.
		e.Kind = "type"
		e.Name = "array"
		if x.Elt != nil {
			e.Arguments = []SemanticArgument{{Value: l.expr(x.Elt)}}
		}
	case *ast.MapType:
		e.Kind = "type"
		e.Name = "map"
		if x.Key != nil {
			e.Arguments = append(e.Arguments, SemanticArgument{Value: l.expr(x.Key)})
		}
		if x.Value != nil {
			e.Arguments = append(e.Arguments, SemanticArgument{Value: l.expr(x.Value)})
		}
	case *ast.TypeAssertExpr:
		e.Kind = "call"
		e.Operator = "eager_left_to_right"
		e.Value = &SemanticExpression{Kind: "identifier", Name: "__type_assert"}
		e.Arguments = []SemanticArgument{{Value: l.expr(x.X)}}
	case *ast.UnaryExpr:
		// Unary operations are semantic expressions. The target renderer already
		// has a native unary contract, so preserve the Go AST operation instead
		// of keeping the former boolean-only frontend restriction.
		e.Kind = "unary"
		e.Operator = x.Op.String()
		e.Value = l.expr(x.X)
	case *ast.BinaryExpr:
		// Shifts are ordinary parameterized integer operations. Their width and
		// signedness are carried by the typed operation and legalized by the
		// existing target projector; the Go frontend must not reject them.
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
		s.Kind = "return"
		if len(x.Results) == 1 {
			s.Expression = l.expr(x.Results[0])
		} else if len(x.Results) > 1 {
			// Multiple Go results have the same value semantics as an ordered
			// aggregate at the canonical boundary. Target-specific tuple or
			// destructuring syntax remains a projection concern.
			aggregate := &SemanticExpression{Kind: "aggregate", Source: l.span(x)}
			for _, result := range x.Results {
				aggregate.Arguments = append(aggregate.Arguments, SemanticArgument{Value: l.expr(result)})
			}
			s.Expression = aggregate
		}
	case *ast.BlockStmt:
		s.Kind = "block"
		for _, child := range x.List {
			s.Statements = append(s.Statements, l.stmt(child))
		}
	case *ast.EmptyStmt:
		s.Kind = "block"
	case *ast.AssignStmt:
		if len(x.Lhs) > 1 {
			// Go evaluates a multi-result call once and assigns its ordered
			// product positionally. Materialize that contract explicitly in the
			// canonical program so every target projector observes the same
			// evaluation and extraction order.
			if len(x.Rhs) != 1 {
				// Parallel assignment with matching arity is an ordered product;
				// retain all expressions and project each position independently.
				if len(x.Rhs) == len(x.Lhs) {
					block := SemanticStatement{Kind: "block", Source: l.span(n)}
					for i, rhs := range x.Rhs {
						lhs, ok := x.Lhs[i].(*ast.Ident)
						if !ok {
							block.Statements = append(block.Statements, SemanticStatement{Kind: "expression", Expression: &SemanticExpression{Kind: "call", Operator: "eager_left_to_right", Value: &SemanticExpression{Kind: "identifier", Name: "__assign_target"}, Arguments: []SemanticArgument{{Value: l.expr(x.Lhs[i])}, {Value: l.expr(rhs)}}}})
							continue
						}
						block.Statements = append(block.Statements, SemanticStatement{Kind: "assign", Name: l.name(lhs), AssignOp: "<-", Expression: l.expr(rhs), Source: l.span(x.Lhs[i])})
					}
					s = block
					break
				}
				l.fail(n, "multiple RHS expressions")
				break
			}
			value := l.expr(x.Rhs[0])
			arity := 0
			if value.Attributes != nil {
				switch v := value.Attributes["result_arity"].(type) {
				case int:
					arity = v
				case int64:
					arity = int(v)
				case float64:
					arity = int(v)
				}
			}
			if arity != 0 && arity != len(x.Lhs) {
				l.fail(n, "multi-result arity")
				break
			}
			temp := fmt.Sprintf("native_result_product_%d", l.resultTempSeq)
			l.resultTempSeq++
			product := SemanticStatement{Kind: "assign", Name: temp, AssignOp: "<-", Source: l.span(n), Expression: value, Attributes: map[string]any{"synthetic": true, "result_product": "ordered", "evaluate_once": true}}
			block := SemanticStatement{Kind: "block", Source: l.span(n), Statements: []SemanticStatement{product}}
			for i, lhs := range x.Lhs {
				ident, ok := lhs.(*ast.Ident)
				if !ok {
					// Non-identifier targets retain ordered extraction as a generic
					// store operation; target projection decides the concrete lvalue.
					idxExpr := &SemanticExpression{Kind: "index", Value: &SemanticExpression{Kind: "identifier", Name: temp}, Arguments: []SemanticArgument{{Value: &SemanticExpression{Kind: "literal", LiteralKind: "number", Text: strconv.Itoa(i + 1)}}}}
					storeExpr := &SemanticExpression{Kind: "call", Operator: "eager_left_to_right", Value: &SemanticExpression{Kind: "identifier", Name: "__assign_target"}, Arguments: []SemanticArgument{{Value: l.expr(lhs)}, {Value: idxExpr}}}
					block.Statements = append(block.Statements, SemanticStatement{Kind: "expression", Expression: storeExpr})
					continue
				}
				ordinal := &SemanticExpression{Kind: "literal", LiteralKind: "number", Text: strconv.Itoa(i + 1), Source: l.span(lhs)}
				extract := &SemanticExpression{Kind: "index", Value: &SemanticExpression{Kind: "identifier", Name: temp, Source: l.span(lhs)}, Arguments: []SemanticArgument{{Value: ordinal}}, Source: l.span(lhs), Attributes: map[string]any{"result_ordinal": i + 1, "result_product": "ordered"}}
				block.Statements = append(block.Statements, SemanticStatement{Kind: "assign", Name: l.name(ident), AssignOp: "<-", Source: l.span(lhs), Expression: extract})
			}
			s = block
			break
		}
		if len(x.Lhs) != 1 || len(x.Rhs) != 1 {
			l.fail(n, "parallel or compound assignment")
			break
		}
		ident, ok := x.Lhs[0].(*ast.Ident)
		if !ok {
			if idx, yes := x.Lhs[0].(*ast.IndexExpr); yes {
				s.Kind = "expression"
				s.Expression = &SemanticExpression{Kind: "call", Operator: "eager_left_to_right", Value: &SemanticExpression{Kind: "identifier", Name: "__index_set"}, Arguments: []SemanticArgument{{Value: l.expr(idx.X)}, {Value: l.canonicalGoIndex(idx.Index)}, {Value: l.expr(x.Rhs[0])}}, Source: l.span(n)}
				break
			}
			if sel, yes := x.Lhs[0].(*ast.SelectorExpr); yes {
				s.Kind = "expression"
				s.Expression = &SemanticExpression{Kind: "call", Operator: "eager_left_to_right", Value: &SemanticExpression{Kind: "identifier", Name: "__member_set"}, Arguments: []SemanticArgument{{Value: l.expr(sel.X)}, {Value: &SemanticExpression{Kind: "literal", LiteralKind: "string", Text: strconv.Quote(sel.Sel.Name)}}, {Value: l.expr(x.Rhs[0])}}, Source: l.span(n)}
				break
			}
			s.Kind = "expression"
			s.Expression = &SemanticExpression{Kind: "call", Operator: "eager_left_to_right", Value: &SemanticExpression{Kind: "identifier", Name: "__assign_target"}, Arguments: []SemanticArgument{{Value: l.expr(x.Lhs[0])}, {Value: l.expr(x.Rhs[0])}}, Source: l.span(n)}
			break
		}
		s.Kind = "assign"
		s.Name = l.name(ident)
		s.AssignOp = "<-"
		s.Expression = l.expr(x.Rhs[0])
		if x.Tok != gotoken.ASSIGN && x.Tok != gotoken.DEFINE {
			op := nativeGoCompoundOperator(x.Tok)
			if op == "" {
				l.fail(n, "compound assignment")
				break
			}
			s.Expression = &SemanticExpression{Kind: "binary", Operator: op, Left: l.expr(ident), Right: s.Expression, Source: l.span(n)}
		}
	case *ast.DeclStmt:
		decl, ok := x.Decl.(*ast.GenDecl)
		if !ok || (decl.Tok != gotoken.VAR && decl.Tok != gotoken.CONST && decl.Tok != gotoken.TYPE) {
			l.fail(n, "declaration group or constant declaration")
			break
		}
		if decl.Tok == gotoken.TYPE {
			s.Kind = "block"
			s.Attributes = map[string]any{"go_type_declaration": true, "count": len(decl.Specs)}
			break
		}
		if len(decl.Specs) != 1 {
			block := SemanticStatement{Kind: "block", Source: l.span(n)}
			for _, raw := range decl.Specs {
				if spec, ok := raw.(*ast.ValueSpec); ok {
					for i, id := range spec.Names {
						value := &ast.BasicLit{Kind: gotoken.INT, Value: "0"}
						if i < len(spec.Values) {
							if ex, ok := spec.Values[i].(*ast.BasicLit); ok {
								value = ex
							}
						}
						block.Statements = append(block.Statements, SemanticStatement{Kind: "assign", Name: l.name(id), AssignOp: "<-", Expression: l.expr(value), Source: l.span(id)})
					}
				}
			}
			s = block
			break
		}
		spec, ok := decl.Specs[0].(*ast.ValueSpec)
		if !ok {
			l.fail(n, "multiple declarations")
			break
		}
		if len(spec.Names) != 1 || len(spec.Values) > 1 {
			block := SemanticStatement{Kind: "block", Source: l.span(n)}
			for i, id := range spec.Names {
				var ex ast.Expr
				if i < len(spec.Values) {
					ex = spec.Values[i]
				}
				if ex == nil {
					ex = &ast.BasicLit{Kind: gotoken.INT, Value: "0"}
				}
				block.Statements = append(block.Statements, SemanticStatement{Kind: "assign", Name: l.name(id), AssignOp: "<-", Expression: l.expr(ex), Source: l.span(id)})
			}
			s = block
			break
		}
		ident := spec.Names[0]
		object := l.info.Defs[ident]
		// Keep every type declaration as structured type metadata. The previous
		// scalar/container whitelist rejected valid Go values (channels, named
		// interfaces, recursive aggregates and compiler-defined aliases) before
		// the canonical type contract could represent them. Unsupported target
		// representation is a later legalization concern, never a frontend parse
		// failure.
		if object != nil {
			l.types[l.name(ident)] = nativeGoType(object.Type(), map[types.Type]bool{})
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
			if object != nil {
				if typ, ok := nativeFixedInteger(object.Type()); ok {
					s.Expression = l.integerOperation(spec, "integer.literal", typ)
					s.Expression.Operation.Text = "0"
				}
				if basic, ok := object.Type().Underlying().(*types.Basic); ok && basic.Info()&types.IsBoolean != 0 {
					s.Expression.LiteralKind = "boolean"
					s.Expression.Text = "FALSE"
				}
			}
		}
	case *ast.IfStmt:
		s.Kind = "if"
		s.Condition = l.expr(x.Cond)
		then := l.stmt(x.Body)
		s.Then = &then
		if x.Else != nil {
			other := l.stmt(x.Else)
			s.Else = &other
		}
		if x.Init != nil {
			init := l.stmt(x.Init)
			s = SemanticStatement{Kind: "block", Source: l.span(n), Statements: []SemanticStatement{init, s}}
		}
	case *ast.ForStmt:
		// A counted Go loop is represented using existing language-neutral
		// block, assignment and while contracts. This intentionally does not
		// retain Go's `for init; condition; post` syntax past the frontend.
		s.Kind = "block"
		if x.Init != nil {
			s.Statements = append(s.Statements, l.stmt(x.Init))
		}
		var condition *SemanticExpression
		if x.Cond != nil {
			condition = l.expr(x.Cond)
		}
		if condition == nil {
			condition = &SemanticExpression{Kind: "literal", LiteralKind: "boolean", Text: "TRUE", Source: l.span(x)}
		}
		loop := SemanticStatement{Kind: "while", Condition: condition, Source: l.span(x)}
		l.loopDepth++
		body := l.stmt(x.Body)
		l.loopDepth--
		if x.Post != nil {
			if body.Kind != "block" {
				body = SemanticStatement{Kind: "block", Statements: []SemanticStatement{body}, Source: l.span(x.Body)}
			}
			body.Statements = append(body.Statements, l.stmt(x.Post))
		}
		loop.Body = &body
		s.Statements = append(s.Statements, loop)
	case *ast.IncDecStmt:
		if x.Tok != gotoken.INC && x.Tok != gotoken.DEC {
			l.fail(n, "increment operation")
			break
		}
		ident, ok := x.X.(*ast.Ident)
		if !ok {
			if idx, yes := x.X.(*ast.IndexExpr); yes {
				s.Kind = "expression"
				op := "+"
				if x.Tok == gotoken.DEC {
					op = "-"
				}
				idxValue := &SemanticExpression{Kind: "index", Value: l.expr(idx.X), Arguments: []SemanticArgument{{Value: l.canonicalGoIndex(idx.Index)}}}
				updated := &SemanticExpression{Kind: "binary", Operator: op, Left: idxValue, Right: &SemanticExpression{Kind: "literal", LiteralKind: "number", Text: "1"}}
				s.Expression = &SemanticExpression{Kind: "call", Operator: "eager_left_to_right", Value: &SemanticExpression{Kind: "identifier", Name: "__index_set"}, Arguments: []SemanticArgument{{Value: l.expr(idx.X)}, {Value: l.canonicalGoIndex(idx.Index)}, {Value: updated}}}
				break
			}
			if sel, yes := x.X.(*ast.SelectorExpr); yes {
				s.Kind = "expression"
				op := "+"
				if x.Tok == gotoken.DEC {
					op = "-"
				}
				cur := &SemanticExpression{Kind: "call", Operator: "eager_left_to_right", Value: &SemanticExpression{Kind: "identifier", Name: "__member_get"}, Arguments: []SemanticArgument{{Value: l.expr(sel.X)}, {Value: &SemanticExpression{Kind: "literal", LiteralKind: "string", Text: strconv.Quote(sel.Sel.Name)}}}}
				updated := &SemanticExpression{Kind: "binary", Operator: op, Left: cur, Right: &SemanticExpression{Kind: "literal", LiteralKind: "number", Text: "1"}}
				s.Expression = &SemanticExpression{Kind: "call", Operator: "eager_left_to_right", Value: &SemanticExpression{Kind: "identifier", Name: "__member_set"}, Arguments: []SemanticArgument{{Value: l.expr(sel.X)}, {Value: &SemanticExpression{Kind: "literal", LiteralKind: "string", Text: strconv.Quote(sel.Sel.Name)}}, {Value: updated}}}
				break
			}
			// Preserve arbitrary addressable lvalues (for example a dereference)
			// as a generic increment/decrement store contract.
			op := "+"
			if x.Tok == gotoken.DEC {
				op = "-"
			}
			s.Kind = "expression"
			s.Expression = &SemanticExpression{Kind: "call", Operator: "eager_left_to_right", Value: &SemanticExpression{Kind: "identifier", Name: "__incdec_target"}, Arguments: []SemanticArgument{{Value: l.expr(x.X)}, {Value: &SemanticExpression{Kind: "literal", LiteralKind: "string", Text: strconv.Quote(op)}}}, Source: l.span(n)}
			break
		}
		s.Kind = "assign"
		s.Name = l.name(ident)
		s.AssignOp = "<-"
		op := "+"
		if x.Tok == gotoken.DEC {
			op = "-"
		}
		s.Expression = &SemanticExpression{Kind: "binary", Operator: op,
			Left:  &SemanticExpression{Kind: "identifier", Name: s.Name, Source: l.span(ident)},
			Right: &SemanticExpression{Kind: "literal", LiteralKind: "number", Text: "1", Source: l.span(x)}, Source: l.span(x)}
	case *ast.RangeStmt:
		value, ok := x.Value.(*ast.Ident)
		if !ok {
			value = ast.NewIdent("_")
		}
		s.Kind = "for"
		s.Name = l.name(value)
		if key, ok := x.Key.(*ast.Ident); ok && key.Name != "_" {
			// The key is a structured binding fact. It is consumed by the common
			// iterable-loop renderer, rather than retained as Go range syntax.
			s.Attributes = map[string]any{"iteration.index_binding": l.name(key)}
		}
		s.Sequence = l.expr(x.X)
		l.loopDepth++
		body := l.stmt(x.Body)
		l.loopDepth--
		s.Body = &body
	case *ast.BranchStmt:
		if x.Tok != gotoken.BREAK && x.Tok != gotoken.CONTINUE {
			// Goto is preserved as an explicit control-flow fact. A later
			// legalizer may reject it for a target, but the frontend retains the
			// source semantics instead of dropping the whole file.
			s.Kind = "goto"
			if x.Label != nil {
				s.Attributes = map[string]any{"target_label": x.Label.Name}
			}
			break
		}
		if x.Tok == gotoken.BREAK && l.loopDepth == 0 && l.switchDepth == 0 && x.Label == nil {
			l.fail(n, "break outside loop or switch")
			break
		}
		if x.Tok == gotoken.CONTINUE && l.loopDepth == 0 && x.Label == nil {
			l.fail(n, "continue outside loop")
			break
		}
		s.Kind = "break"
		if x.Tok == gotoken.CONTINUE {
			s.Kind = "continue"
		}
		if x.Label != nil {
			s.Attributes = map[string]any{"target_label": x.Label.Name}
		}
	case *ast.LabeledStmt:
		// Labels are control-flow metadata attached to the following structured
		// statement. Keep the nested statement and label together so downstream
		// control-flow analysis can resolve it without reparsing Go source.
		inner := l.stmt(x.Stmt)
		inner.Attributes = cloneSemanticAttributes(inner.Attributes)
		if inner.Attributes == nil {
			inner.Attributes = map[string]any{}
		}
		inner.Attributes["label"] = x.Label.Name
		s = inner
	case *ast.ExprStmt:
		call, ok := x.X.(*ast.CallExpr)
		s.Kind = "expression"
		if ok {
			s.Expression = l.expr(call)
		} else {
			s.Expression = l.expr(x.X)
		}
	case *ast.SwitchStmt:
		// Switch cases are represented as a structured control node.  Keeping
		// every clause body and the optional tag in attributes lets the common
		// UAST projector preserve control-flow shape without a Go-specific
		// emitter branch.
		s.Kind = "switch"
		s.Attributes = map[string]any{"go_switch": true, "case_count": len(x.Body.List)}
		if x.Tag != nil {
			s.Attributes["tag"] = l.expr(x.Tag)
		}
		l.switchDepth++
		for _, clause := range x.Body.List {
			if cc, ok := clause.(*ast.CaseClause); ok {
				for _, bodyStmt := range cc.Body {
					s.Statements = append(s.Statements, l.stmt(bodyStmt))
				}
			}
		}
		l.switchDepth--
	case *ast.TypeSwitchStmt:
		s.Kind = "switch"
		s.Attributes = map[string]any{"go_type_switch": true, "case_count": len(x.Body.List)}
		if x.Assign != nil {
			s.Attributes["assignment"] = fmt.Sprintf("%T", x.Assign)
		}
		l.switchDepth++
		for _, clause := range x.Body.List {
			if cc, ok := clause.(*ast.CaseClause); ok {
				for _, bodyStmt := range cc.Body {
					s.Statements = append(s.Statements, l.stmt(bodyStmt))
				}
			}
		}
		l.switchDepth--
	case *ast.SelectStmt:
		s.Kind = "switch"
		s.Attributes = map[string]any{"go_select": true, "case_count": len(x.Body.List)}
		l.switchDepth++
		for _, clause := range x.Body.List {
			if cc, ok := clause.(*ast.CommClause); ok {
				for _, bodyStmt := range cc.Body {
					s.Statements = append(s.Statements, l.stmt(bodyStmt))
				}
			}
		}
		l.switchDepth--
	case *ast.GoStmt:
		s.Kind = "expression"
		s.Attributes = map[string]any{"go_async": true}
		s.Expression = l.expr(x.Call)
	case *ast.DeferStmt:
		s.Kind = "expression"
		s.Attributes = map[string]any{"go_defer": true}
		s.Expression = l.expr(x.Call)
	case *ast.SendStmt:
		s.Kind = "expression"
		s.Expression = &SemanticExpression{Kind: "call", Operator: "eager_left_to_right", Value: &SemanticExpression{Kind: "identifier", Name: "__channel_send"}, Arguments: []SemanticArgument{{Value: l.expr(x.Chan)}, {Value: l.expr(x.Value)}}, Source: l.span(x)}
	default:
		l.fail(n, fmt.Sprintf("statement %T", n))
	}
	return s
}

func nativeGoCompoundOperator(tok gotoken.Token) string {
	switch tok {
	case gotoken.ADD_ASSIGN:
		return "+"
	case gotoken.SUB_ASSIGN:
		return "-"
	case gotoken.MUL_ASSIGN:
		return "*"
	case gotoken.QUO_ASSIGN:
		return "/"
	case gotoken.REM_ASSIGN:
		return "%"
	case gotoken.AND_ASSIGN:
		return "&"
	case gotoken.OR_ASSIGN:
		return "|"
	case gotoken.XOR_ASSIGN:
		return "^"
	case gotoken.SHL_ASSIGN:
		return "<<"
	case gotoken.SHR_ASSIGN:
		return ">>"
	default:
		return ""
	}
}
