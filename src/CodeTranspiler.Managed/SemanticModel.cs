using System.Text.Json.Serialization;

namespace CodeTranspiler.Managed;

/// <summary>Severity for parser/transpiler diagnostics.</summary>
public enum DiagnosticSeverity { Info, Warning, Error }

/// <summary>A structured diagnostic produced while lowering or emitting.</summary>
public sealed record TranspileDiagnostic(
    DiagnosticSeverity Severity,
    string Code,
    string Message,
    int? Line = null,
    int? Column = null,
    string? Fragment = null,
    string? Route = null);

/// <summary>Public options controlling fragment fallback and formatting.</summary>
public sealed class TranspileOptions
{
    public bool PreserveComments { get; init; } = true;
    public bool PreserveUnknownFragmentsAsComments { get; init; } = true;
    public bool EnableFragmentFallback { get; init; } = true;
    public bool EnableRouteRacing { get; init; } = true;
    public bool EmitHeader { get; init; } = true;
    public bool PreferExplicitTypes { get; init; } = true;
    public int MaxFragmentDepth { get; init; } = 6;
    public string? ModuleName { get; init; }
}

/// <summary>Result returned by the managed transpiler.</summary>
public sealed class TranspileResult
{
    public required LanguageId SourceLanguage { get; init; }
    public required LanguageId TargetLanguage { get; init; }
    public required string Code { get; init; }
    public required SemanticProgram Program { get; init; }
    public required IReadOnlyList<TranspileDiagnostic> Diagnostics { get; init; }
    public required IReadOnlyList<RouteAttempt> Attempts { get; init; }
    public bool Success => Diagnostics.All(x => x.Severity != DiagnosticSeverity.Error);
}

/// <summary>One candidate route evaluated by the fragment orchestrator.</summary>
public sealed record RouteAttempt(
    string Route,
    bool Success,
    int Score,
    int PreservedSymbols,
    int TotalSymbols,
    string? Diagnostic = null);

/// <summary>Language-neutral program used by all managed frontends/backends.</summary>
public sealed class SemanticProgram
{
    public LanguageId SourceLanguage { get; set; }
    public string Evaluation { get; set; } = "eager_left_to_right";
    public int SourceIndexBase { get; set; }
    public int CanonicalIndexBase { get; set; } = 0;
    public string ValueModel { get; set; } = "managed_dynamic_v1";
    public List<SemanticImport> Imports { get; } = new();
    public List<SemanticDeclaration> Declarations { get; } = new();
    public List<SemanticStatement> TopLevelStatements { get; } = new();
    public Dictionary<string, string> Metadata { get; } = new(StringComparer.Ordinal);
    public List<string> OriginalSymbols { get; } = new();

    public IEnumerable<SemanticFunction> Functions => Declarations.OfType<SemanticFunction>();
    public IEnumerable<SemanticTypeDeclaration> Types => Declarations.OfType<SemanticTypeDeclaration>();
}

public sealed record SemanticImport(string Module, string? Alias = null, IReadOnlyList<string>? Members = null);

public abstract record SemanticNode(int? SourceLine = null);
[JsonPolymorphic(TypeDiscriminatorPropertyName = "$kind")]
[JsonDerivedType(typeof(SemanticFunction), "function")]
[JsonDerivedType(typeof(SemanticTypeDeclaration), "type")]
public abstract record SemanticDeclaration(string Name, int? SourceLine = null) : SemanticNode(SourceLine);
[JsonPolymorphic(TypeDiscriminatorPropertyName = "$kind")]
[JsonDerivedType(typeof(SemanticVariableDeclaration), "variable")]
[JsonDerivedType(typeof(SemanticBlock), "block")]
[JsonDerivedType(typeof(SemanticExpressionStatement), "expression_statement")]
[JsonDerivedType(typeof(SemanticReturn), "return")]
[JsonDerivedType(typeof(SemanticBreak), "break")]
[JsonDerivedType(typeof(SemanticContinue), "continue")]
[JsonDerivedType(typeof(SemanticThrow), "throw")]
[JsonDerivedType(typeof(SemanticAssignment), "assignment")]
[JsonDerivedType(typeof(SemanticIf), "if")]
[JsonDerivedType(typeof(SemanticWhile), "while")]
[JsonDerivedType(typeof(SemanticFor), "for")]
[JsonDerivedType(typeof(SemanticForEach), "foreach")]
[JsonDerivedType(typeof(SemanticTry), "try")]
[JsonDerivedType(typeof(SemanticRawStatement), "raw_statement")]
[JsonDerivedType(typeof(SemanticComment), "comment")]
public abstract record SemanticStatement(int? SourceLine = null) : SemanticNode(SourceLine);
[JsonPolymorphic(TypeDiscriminatorPropertyName = "$kind")]
[JsonDerivedType(typeof(SemanticIdentifier), "identifier")]
[JsonDerivedType(typeof(SemanticLiteral), "literal")]
[JsonDerivedType(typeof(SemanticUnary), "unary")]
[JsonDerivedType(typeof(SemanticBinary), "binary")]
[JsonDerivedType(typeof(SemanticConditional), "conditional")]
[JsonDerivedType(typeof(SemanticCall), "call")]
[JsonDerivedType(typeof(SemanticMember), "member")]
[JsonDerivedType(typeof(SemanticIndex), "index")]
[JsonDerivedType(typeof(SemanticSlice), "slice")]
[JsonDerivedType(typeof(SemanticArray), "array")]
[JsonDerivedType(typeof(SemanticMap), "map")]
[JsonDerivedType(typeof(SemanticLambda), "lambda")]
[JsonDerivedType(typeof(SemanticRawExpression), "raw_expression")]
public abstract record SemanticExpression(int? SourceLine = null) : SemanticNode(SourceLine);

public sealed record SemanticParameter(string Name, SemanticTypeRef Type, SemanticExpression? DefaultValue = null, bool IsVariadic = false);

public sealed record SemanticFunction(
    string Name,
    IReadOnlyList<SemanticParameter> Parameters,
    SemanticTypeRef ReturnType,
    SemanticBlock Body,
    bool IsPublic = true,
    bool IsStatic = true,
    bool IsAsync = false,
    int? SourceLine = null) : SemanticDeclaration(Name, SourceLine);

public sealed record SemanticField(string Name, SemanticTypeRef Type, SemanticExpression? Initializer = null, bool IsMutable = true);

public enum SemanticTypeKind { Class, Struct, Interface, Enum, Record }

public sealed record SemanticTypeDeclaration(
    string Name,
    SemanticTypeKind Kind,
    IReadOnlyList<SemanticField> Fields,
    IReadOnlyList<SemanticFunction> Methods,
    IReadOnlyList<string>? BaseTypes = null,
    int? SourceLine = null) : SemanticDeclaration(Name, SourceLine);

public sealed record SemanticVariableDeclaration(
    string Name,
    SemanticTypeRef Type,
    SemanticExpression? Initializer,
    bool IsMutable = true,
    bool IsConstant = false,
    int? SourceLine = null) : SemanticStatement(SourceLine);

public sealed record SemanticBlock(IReadOnlyList<SemanticStatement> Statements, int? SourceLine = null) : SemanticStatement(SourceLine);
public sealed record SemanticExpressionStatement(SemanticExpression Expression, int? SourceLine = null) : SemanticStatement(SourceLine);
public sealed record SemanticReturn(SemanticExpression? Expression, int? SourceLine = null) : SemanticStatement(SourceLine);
public sealed record SemanticBreak(int? SourceLine = null) : SemanticStatement(SourceLine);
public sealed record SemanticContinue(int? SourceLine = null) : SemanticStatement(SourceLine);
public sealed record SemanticThrow(SemanticExpression Expression, int? SourceLine = null) : SemanticStatement(SourceLine);
public sealed record SemanticAssignment(SemanticExpression Target, string Operator, SemanticExpression Value, int? SourceLine = null) : SemanticStatement(SourceLine);
public sealed record SemanticIf(SemanticExpression Condition, SemanticBlock Then, SemanticBlock? Else = null, int? SourceLine = null) : SemanticStatement(SourceLine);
public sealed record SemanticWhile(SemanticExpression Condition, SemanticBlock Body, int? SourceLine = null) : SemanticStatement(SourceLine);
public sealed record SemanticFor(
    SemanticStatement? Init,
    SemanticExpression? Condition,
    SemanticStatement? Step,
    SemanticBlock Body,
    int? SourceLine = null) : SemanticStatement(SourceLine);
public sealed record SemanticForEach(string Variable, SemanticExpression Iterable, SemanticBlock Body, int? SourceLine = null) : SemanticStatement(SourceLine);
public sealed record SemanticTry(SemanticBlock Body, string? CatchVariable, SemanticBlock? Catch, SemanticBlock? Finally, int? SourceLine = null) : SemanticStatement(SourceLine);
public sealed record SemanticRawStatement(string Text, int? SourceLine = null) : SemanticStatement(SourceLine);
public sealed record SemanticComment(string Text, int? SourceLine = null) : SemanticStatement(SourceLine);

public sealed record SemanticIdentifier(string Name, int? SourceLine = null) : SemanticExpression(SourceLine);
public sealed record SemanticLiteral(object? Value, string LiteralKind = "auto", int? SourceLine = null) : SemanticExpression(SourceLine);
public sealed record SemanticUnary(string Operator, SemanticExpression Operand, bool Prefix = true, int? SourceLine = null) : SemanticExpression(SourceLine);
public sealed record SemanticBinary(SemanticExpression Left, string Operator, SemanticExpression Right, int? SourceLine = null) : SemanticExpression(SourceLine);
public sealed record SemanticConditional(SemanticExpression Condition, SemanticExpression WhenTrue, SemanticExpression WhenFalse, int? SourceLine = null) : SemanticExpression(SourceLine);
public sealed record SemanticCall(SemanticExpression Callee, IReadOnlyList<SemanticExpression> Arguments, IReadOnlyDictionary<string, SemanticExpression>? NamedArguments = null, int? SourceLine = null) : SemanticExpression(SourceLine);
public sealed record SemanticMember(SemanticExpression Target, string Member, int? SourceLine = null) : SemanticExpression(SourceLine);
public sealed record SemanticIndex(SemanticExpression Target, SemanticExpression Index, int? SourceLine = null) : SemanticExpression(SourceLine);
public sealed record SemanticSlice(SemanticExpression Target, SemanticExpression? Start, SemanticExpression? End, SemanticExpression? Step, int? SourceLine = null) : SemanticExpression(SourceLine);
public sealed record SemanticArray(IReadOnlyList<SemanticExpression> Items, int? SourceLine = null) : SemanticExpression(SourceLine);
public sealed record SemanticMap(IReadOnlyList<(SemanticExpression Key, SemanticExpression Value)> Items, int? SourceLine = null) : SemanticExpression(SourceLine);
public sealed record SemanticLambda(IReadOnlyList<string> Parameters, SemanticExpression Body, int? SourceLine = null) : SemanticExpression(SourceLine);
public sealed record SemanticRawExpression(string Text, int? SourceLine = null) : SemanticExpression(SourceLine);

/// <summary>Portable type reference used by the semantic IR.</summary>
public sealed record SemanticTypeRef(string Name, bool Nullable = false, IReadOnlyList<SemanticTypeRef>? GenericArguments = null)
{
    public static readonly SemanticTypeRef Unknown = new("unknown");
    public static readonly SemanticTypeRef Void = new("void");
    public static readonly SemanticTypeRef Bool = new("bool");
    public static readonly SemanticTypeRef Int = new("int");
    public static readonly SemanticTypeRef Int64 = new("int64");
    public static readonly SemanticTypeRef Float64 = new("float64");
    public static readonly SemanticTypeRef String = new("string");
    public static readonly SemanticTypeRef Object = new("object");

    public static SemanticTypeRef ListOf(SemanticTypeRef element) => new("list", false, new[] { element });
    public static SemanticTypeRef MapOf(SemanticTypeRef key, SemanticTypeRef value) => new("map", false, new[] { key, value });
}
