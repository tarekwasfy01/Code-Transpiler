package backend

import (
	"fmt"
	"go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"go/types"
	"reflect"

	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

// NativeFrontend extracts structured facts without reconstructing source text.
// Analysis is deliberately separate from executable lowering: a type-checked
// source construct is not automatically supported by the dynamic runtime.
type NativeFrontend interface {
	Analyze(filename, source string) (*NativeAnalysis, error)
}

type NativeEvent struct {
	ID           int                `json:"id"`
	Kind         string             `json:"kind"`
	Name         string             `json:"name,omitempty"`
	Literal      string             `json:"literal,omitempty"`
	Type         SemanticType       `json:"type"`
	Source       SemanticSourceSpan `json:"source"`
	Binding      *int               `json:"binding,omitempty"`
	Declaration  bool               `json:"declaration,omitempty"`
	Operation    string             `json:"operation,omitempty"`
	ShortCircuit bool               `json:"short_circuit,omitempty"`
	Confidence   string             `json:"confidence"`
	Scope        int                `json:"scope"`
}

// NativeAnalysis is a frontend analysis artifact, NOT SemanticDocument v1.
// Syntax stores parent-to-child edges; Binding stores occurrences-to-symbols.
// The product Binding * Binding^T relates occurrences of the SAME symbol.
// It is not a dataflow or evaluation-order matrix.
type NativeAnalysis struct {
	Schema                string                   `json:"schema"`
	Language              string                   `json:"language"`
	Executable            bool                     `json:"executable"`
	Events                []NativeEvent            `json:"events"`
	Syntax                matrixir.SparseMatrix    `json:"syntax"`
	Binding               matrixir.SparseMatrix    `json:"binding"`
	SameBinding           matrixir.SparseMatrix    `json:"same_binding"`
	Requirements          []string                 `json:"requirements"`
	Scopes                []SemanticScope          `json:"scopes"`
	ScopeMembership       matrixir.SparseMatrix    `json:"scope_membership"`
	TypeCheckArchitecture string                   `json:"type_check_architecture"`
	TypeTable             []SemanticTypeDefinition `json:"type_table"`
	TypeGraph             matrixir.SparseMatrix    `json:"type_graph"`
	TypeRelations         *SemanticTypeRelations   `json:"type_relations"`
}

// GoNativeFrontend uses the official Go parser and type checker. Imports are
// rejected until an explicit, reproducible module resolver is supplied. Widths
// of platform-dependent int/uint/uintptr remain unknown, not host-derived.
type GoNativeFrontend struct{}

func (GoNativeFrontend) Analyze(filename, source string) (*NativeAnalysis, error) {
	fs := gotoken.NewFileSet()
	file, err := goparser.ParseFile(fs, filename, source, 0)
	if err != nil {
		return nil, err
	}
	if len(file.Imports) > 0 {
		return nil, fmt.Errorf("%s: native Go imports require a module resolver (unsupported)", filename)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	config := types.Config{Sizes: types.SizesFor("gc", "amd64")}
	pkg, err := config.Check(file.Name.Name, fs, []*ast.File{file}, info)
	if err != nil {
		return nil, fmt.Errorf("native Go type check: %w", err)
	}
	out := &NativeAnalysis{Schema: "code-transpiler.native-analysis.v1", Language: "go", TypeCheckArchitecture: "gc/amd64", Requirements: []string{"native.go.lowering"}}
	scopeIDs := map[*types.Scope]int{}
	var addScope func(*types.Scope, int)
	addScope = func(s *types.Scope, parent int) {
		id := len(out.Scopes)
		scopeIDs[s] = id
		out.Scopes = append(out.Scopes, SemanticScope{ID: id, Parent: parent, Kind: "lexical"})
		for i := 0; i < s.NumChildren(); i++ {
			addScope(s.Child(i), id)
		}
	}
	addScope(pkg.Scope(), -1)
	explicit := map[types.Object]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.ValueSpec:
			if x.Type != nil {
				for _, name := range x.Names {
					explicit[info.Defs[name]] = true
				}
			}
		case *ast.Field:
			for _, name := range x.Names {
				explicit[info.Defs[name]] = true
			}
		}
		return true
	})
	symbols := map[types.Object]int{}
	var parents []int
	var edges [][2]int
	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			parents = parents[:len(parents)-1]
			return false
		}
		id := len(out.Events)
		if len(parents) > 0 {
			edges = append(edges, [2]int{parents[len(parents)-1], id})
		}
		parents = append(parents, id)
		start, end := fs.Position(n.Pos()), fs.Position(n.End())
		event := NativeEvent{ID: id, Kind: "go." + reflect.TypeOf(n).Elem().Name(), Type: SemanticType{Kind: "unknown", TypeOrigin: "unknown"}, Confidence: "unknown", Source: SemanticSourceSpan{File: filename, StartOffset: start.Offset, EndOffset: end.Offset, StartLine: start.Line, StartColumn: start.Column, EndLine: end.Line, EndColumn: end.Column}}
		if scope := pkg.Scope().Innermost(n.Pos()); scope != nil {
			event.Scope = scopeIDs[scope]
		}
		if expr, ok := n.(ast.Expr); ok {
			if tv, exists := info.Types[expr]; exists {
				event.Type = nativeGoType(tv.Type, map[types.Type]bool{})
				event.Confidence = "exact"
			}
		}
		switch x := n.(type) {
		case *ast.Ident:
			event.Name = x.Name
			object := info.Defs[x]
			event.Declaration = object != nil
			if object == nil {
				object = info.Uses[x]
			}
			if object != nil {
				symbol, exists := symbols[object]
				if !exists {
					symbol = len(symbols)
					symbols[object] = symbol
				}
				event.Binding = &symbol
				event.Type = nativeGoType(object.Type(), map[types.Type]bool{})
				if explicit[object] {
					event.Type.TypeOrigin = "explicit"
				}
				event.Confidence = "exact"
				if event.Declaration {
					event.Operation = "binding.declare"
				} else {
					event.Operation = "binding.reference"
				}
			}
		case *ast.BasicLit:
			event.Literal = x.Value
		case *ast.BinaryExpr:
			prefix := "numeric"
			left := info.TypeOf(x.X)
			if left != nil {
				if b, ok := left.Underlying().(*types.Basic); ok {
					switch {
					case b.Info()&types.IsInteger != 0:
						prefix = "integer"
					case b.Info()&types.IsFloat != 0:
						prefix = "floating"
					case b.Info()&types.IsString != 0:
						prefix = "string"
					case b.Info()&types.IsBoolean != 0:
						prefix = "logical"
					}
				}
			}
			op := map[gotoken.Token]string{gotoken.ADD: "add", gotoken.SUB: "subtract", gotoken.MUL: "multiply", gotoken.QUO: "divide", gotoken.REM: "remainder", gotoken.LAND: "and", gotoken.LOR: "or", gotoken.EQL: "equal", gotoken.NEQ: "not_equal", gotoken.LSS: "less", gotoken.GTR: "greater", gotoken.LEQ: "less_equal", gotoken.GEQ: "greater_equal", gotoken.AND: "bit_and", gotoken.OR: "bit_or", gotoken.XOR: "bit_xor", gotoken.SHL: "shift_left", gotoken.SHR: "shift_right", gotoken.AND_NOT: "bit_clear"}[x.Op]
			if prefix == "string" && x.Op == gotoken.ADD {
				op = "concat"
			}
			event.Operation = prefix + "." + op
			event.ShortCircuit = x.Op == gotoken.LAND || x.Op == gotoken.LOR
		case *ast.CallExpr:
			event.Operation = "function.call"
			if tv, ok := info.Types[x.Fun]; ok && tv.IsType() {
				event.Operation = "conversion.explicit"
			}
		}
		out.Events = append(out.Events, event)
		return true
	})
	out.Syntax = matrixir.NewSparseMatrix(len(out.Events), len(out.Events))
	for _, edge := range edges {
		out.Syntax.Set(edge[0], edge[1], 1)
	}
	out.Binding = matrixir.NewSparseMatrix(len(out.Events), len(symbols))
	out.ScopeMembership = matrixir.NewSparseMatrix(len(out.Events), len(out.Scopes))
	for _, event := range out.Events {
		out.ScopeMembership.Set(event.ID, event.Scope, 1)
		if event.Binding != nil {
			out.Binding.Set(event.ID, *event.Binding, 1)
		}
	}
	out.SameBinding, err = out.Binding.Multiply(out.Binding.Transpose())
	if err != nil {
		return nil, err
	}
	// Reuse the language-neutral projection for every captured event type,
	// including composites not yet accepted by executable lowering.
	typeRoot := &SemanticStatement{}
	for _, event := range out.Events {
		typeRoot.Statements = append(typeRoot.Statements, SemanticStatement{Type: event.Type})
	}
	out.TypeTable, out.TypeGraph, err = deriveTypeTable(typeRoot)
	if err != nil {
		return nil, err
	}
	out.TypeRelations, err = deriveTypeRelations(typeRoot, out.TypeTable)
	if err != nil {
		return nil, err
	}
	for i := range out.TypeRelations.Occurrences {
		out.TypeRelations.Occurrences[i] = fmt.Sprintf("/events/%d/type", i)
	}
	return out, err
}

func nativeGoType(t types.Type, seen map[types.Type]bool) SemanticType {
	if t == nil {
		return SemanticType{Kind: "unknown", TypeOrigin: "unknown"}
	}
	result := SemanticType{Name: types.TypeString(t, nil), TypeOrigin: "inferred"}
	// Source-local nominal identity includes declaration position to distinguish
	// equally named local types. Instantiation spelling distinguishes Box[int]
	// from Box[string]. This is not a cross-build linkage identifier.
	var declaration *types.TypeName
	switch x := t.(type) {
	case *types.Named:
		declaration = x.Obj()
	case *types.Alias:
		declaration = x.Obj()
	case *types.TypeParam:
		declaration = x.Obj()
	}
	if declaration != nil {
		pkg := ""
		if declaration.Pkg() != nil {
			pkg = declaration.Pkg().Path()
		}
		result.Identity = fmt.Sprintf("go:%s:%d:%s:%s", pkg, declaration.Pos(), declaration.Name(), result.Name)
	}
	if seen[t] && declaration != nil {
		result.Kind = "named"
		result.Reference = true
		return result
	}
	seen[t] = true
	defer delete(seen, t)
	child := func(t types.Type) *SemanticType { v := nativeGoType(t, seen); return &v }
	switch x := t.(type) {
	case *types.Basic:
		switch {
		case x.Info()&types.IsInteger != 0:
			result.Kind = "integer"
			signed := x.Info()&types.IsUnsigned == 0
			result.Signed = &signed
			result.Bits = map[types.BasicKind]int{types.Int8: 8, types.Int16: 16, types.Int32: 32, types.Int64: 64, types.Uint8: 8, types.Uint16: 16, types.Uint32: 32, types.Uint64: 64}[x.Kind()]
			if x.Info()&types.IsUntyped != 0 {
				result.Kind = "arbitrary_integer"
			}
		case x.Info()&types.IsFloat != 0:
			result.Kind = "floating"
			result.Bits = map[types.BasicKind]int{types.Float32: 32, types.Float64: 64}[x.Kind()]
			result.IEEE754 = result.Bits != 0
		case x.Info()&types.IsComplex != 0:
			result.Kind = "complex"
		case x.Info()&types.IsBoolean != 0:
			result.Kind = "boolean"
		case x.Info()&types.IsString != 0:
			result.Kind = "string"
		default:
			result.Kind = "unknown"
		}
	case *types.Pointer:
		result.Kind = "pointer"
		result.Element = child(x.Elem())
	case *types.Slice:
		result.Kind = "slice"
		result.Element = child(x.Elem())
	case *types.Array:
		result.Kind = "array"
		result.Element = child(x.Elem())
		result.Length = int(x.Len())
	case *types.Map:
		result.Kind = "map"
		result.Key = child(x.Key())
		result.Value = child(x.Elem())
	case *types.Chan:
		result.Kind = "channel"
		result.Element = child(x.Elem())
		result.Constraints = []string{fmt.Sprintf("direction:%d", x.Dir())}
	case *types.Struct:
		result.Kind = "struct"
		for i := 0; i < x.NumFields(); i++ {
			f := x.Field(i)
			result.Fields = append(result.Fields, SemanticField{Name: f.Name(), Type: nativeGoType(f.Type(), seen)})
		}
	case *types.Signature:
		result.Kind = "function"
		for i := 0; i < x.TypeParams().Len(); i++ {
			result.TypeParameters = append(result.TypeParameters, nativeGoType(x.TypeParams().At(i), seen))
		}
		for i := 0; i < x.Params().Len(); i++ {
			result.Parameters = append(result.Parameters, nativeGoType(x.Params().At(i).Type(), seen))
		}
		result.Result = child(x.Results())
		if x.Variadic() {
			result.Constraints = append(result.Constraints, "variadic")
		}
	case *types.Tuple:
		result.Kind = "tuple"
		for i := 0; i < x.Len(); i++ {
			result.Parameters = append(result.Parameters, nativeGoType(x.At(i).Type(), seen))
		}
	case *types.Named:
		result.Kind = "named"
		result.Element = child(x.Underlying())
		for i := 0; i < x.TypeParams().Len(); i++ {
			result.TypeParameters = append(result.TypeParameters, nativeGoType(x.TypeParams().At(i), seen))
		}
		for i := 0; i < x.TypeArgs().Len(); i++ {
			result.TypeArguments = append(result.TypeArguments, nativeGoType(x.TypeArgs().At(i), seen))
		}
	case *types.Alias:
		result.Kind = "alias"
		result.Element = child(types.Unalias(x))
		for i := 0; i < x.TypeParams().Len(); i++ {
			result.TypeParameters = append(result.TypeParameters, nativeGoType(x.TypeParams().At(i), seen))
		}
		for i := 0; i < x.TypeArgs().Len(); i++ {
			result.TypeArguments = append(result.TypeArguments, nativeGoType(x.TypeArgs().At(i), seen))
		}
	case *types.Interface:
		result.Kind = "interface"
		for i := 0; i < x.NumExplicitMethods(); i++ {
			m := x.ExplicitMethod(i)
			result.Methods = append(result.Methods, SemanticField{Name: m.Name(), Type: nativeGoType(m.Type(), seen)})
		}
		for i := 0; i < x.NumEmbeddeds(); i++ {
			result.Embedded = append(result.Embedded, nativeGoType(x.EmbeddedType(i), seen))
		}
	case *types.TypeParam:
		result.Kind = "type_parameter"
		result.Constraint = child(x.Constraint())
	case *types.Union:
		result.Kind = "union"
		for i := 0; i < x.Len(); i++ {
			term := x.Term(i)
			result.Terms = append(result.Terms, SemanticTypeTerm{Type: nativeGoType(term.Type(), seen), Underlying: term.Tilde()})
		}
	default:
		result.Kind = "unknown"
	}
	return result
}
