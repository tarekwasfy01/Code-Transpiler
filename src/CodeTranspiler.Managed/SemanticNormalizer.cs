namespace CodeTranspiler.Managed;

internal static class SemanticNormalizer
{
    public static void Normalize(SemanticProgram program)
    {
        if (program.SourceIndexBase == 0) return;
        for (var i = 0; i < program.Declarations.Count; i++)
        {
            if (program.Declarations[i] is SemanticFunction fn)
                program.Declarations[i] = fn with { Body = NormalizeBlock(fn.Body, program.SourceIndexBase) };
            else if (program.Declarations[i] is SemanticTypeDeclaration type)
                program.Declarations[i] = type with { Methods = type.Methods.Select(m => m with { Body = NormalizeBlock(m.Body, program.SourceIndexBase) }).ToArray() };
        }
        for (var i = 0; i < program.TopLevelStatements.Count; i++)
            program.TopLevelStatements[i] = NormalizeStatement(program.TopLevelStatements[i], program.SourceIndexBase);
        program.CanonicalIndexBase = 0;
    }

    private static SemanticBlock NormalizeBlock(SemanticBlock block, int sourceBase) => block with
    {
        Statements = block.Statements.Select(x => NormalizeStatement(x, sourceBase)).ToArray()
    };

    private static SemanticStatement NormalizeStatement(SemanticStatement statement, int sourceBase) => statement switch
    {
        SemanticVariableDeclaration v => v with { Initializer = v.Initializer is null ? null : NormalizeExpression(v.Initializer, sourceBase) },
        SemanticExpressionStatement e => e with { Expression = NormalizeExpression(e.Expression, sourceBase) },
        SemanticReturn r => r with { Expression = r.Expression is null ? null : NormalizeExpression(r.Expression, sourceBase) },
        SemanticThrow t => t with { Expression = NormalizeExpression(t.Expression, sourceBase) },
        SemanticAssignment a => a with { Target = NormalizeExpression(a.Target, sourceBase), Value = NormalizeExpression(a.Value, sourceBase) },
        SemanticIf i => i with { Condition = NormalizeExpression(i.Condition, sourceBase), Then = NormalizeBlock(i.Then, sourceBase), Else = i.Else is null ? null : NormalizeBlock(i.Else, sourceBase) },
        SemanticWhile w => w with { Condition = NormalizeExpression(w.Condition, sourceBase), Body = NormalizeBlock(w.Body, sourceBase) },
        SemanticFor f => f with { Init = f.Init is null ? null : NormalizeStatement(f.Init, sourceBase), Condition = f.Condition is null ? null : NormalizeExpression(f.Condition, sourceBase), Step = f.Step is null ? null : NormalizeStatement(f.Step, sourceBase), Body = NormalizeBlock(f.Body, sourceBase) },
        SemanticForEach e => e with { Iterable = NormalizeExpression(e.Iterable, sourceBase), Body = NormalizeBlock(e.Body, sourceBase) },
        SemanticTry t => t with { Body = NormalizeBlock(t.Body, sourceBase), Catch = t.Catch is null ? null : NormalizeBlock(t.Catch, sourceBase), Finally = t.Finally is null ? null : NormalizeBlock(t.Finally, sourceBase) },
        SemanticBlock b => NormalizeBlock(b, sourceBase),
        _ => statement
    };

    private static SemanticExpression NormalizeExpression(SemanticExpression expression, int sourceBase)
    {
        SemanticExpression Recurse(SemanticExpression e) => NormalizeExpression(e, sourceBase);
        return expression switch
        {
            SemanticIndex i => i with { Target = Recurse(i.Target), Index = ToCanonical(Recurse(i.Index), sourceBase) },
            SemanticSlice s => s with { Target = Recurse(s.Target), Start = s.Start is null ? null : ToCanonical(Recurse(s.Start), sourceBase), End = s.End is null ? null : ToCanonicalEnd(Recurse(s.End), sourceBase), Step = s.Step is null ? null : Recurse(s.Step) },
            SemanticUnary u => u with { Operand = Recurse(u.Operand) },
            SemanticBinary b => b with { Left = Recurse(b.Left), Right = Recurse(b.Right) },
            SemanticConditional c => c with { Condition = Recurse(c.Condition), WhenTrue = Recurse(c.WhenTrue), WhenFalse = Recurse(c.WhenFalse) },
            SemanticCall c => c with { Callee = Recurse(c.Callee), Arguments = c.Arguments.Select(Recurse).ToArray(), NamedArguments = c.NamedArguments?.ToDictionary(x => x.Key, x => Recurse(x.Value), StringComparer.Ordinal) },
            SemanticMember m => m with { Target = Recurse(m.Target) },
            SemanticArray a => a with { Items = a.Items.Select(Recurse).ToArray() },
            SemanticMap m => m with { Items = m.Items.Select(x => (Recurse(x.Key), Recurse(x.Value))).ToArray() },
            SemanticLambda l => l with { Body = Recurse(l.Body) },
            _ => expression
        };
    }

    private static SemanticExpression ToCanonical(SemanticExpression index, int sourceBase)
        => sourceBase == 0 ? index : new SemanticBinary(index, "-", new SemanticLiteral((long)sourceBase, "int64", index.SourceLine), index.SourceLine);

    private static SemanticExpression ToCanonicalEnd(SemanticExpression end, int sourceBase)
        => sourceBase == 0 ? end : new SemanticBinary(end, "-", new SemanticLiteral((long)sourceBase, "int64", end.SourceLine), end.SourceLine);
}
