using System.Text.RegularExpressions;

namespace CodeTranspiler.Managed;

internal static class FragmentOrchestrator
{
    internal sealed record Candidate(string Route, SemanticProgram Program, string Code, IReadOnlyList<TranspileDiagnostic> Diagnostics, int Score, int Preserved, int Total);

    public static Candidate Transpile(string source, LanguageId sourceLanguage, LanguageId targetLanguage, TranspileOptions options, List<RouteAttempt> attempts)
    {
        var candidates = new List<Candidate>();

        var primaryDiagnostics = new List<TranspileDiagnostic>();
        var primaryProgram = FrontendParser.Parse(source, sourceLanguage, options, primaryDiagnostics);
        SemanticNormalizer.Normalize(primaryProgram);
        var primaryCode = CodeEmitter.Emit(primaryProgram, targetLanguage, options, primaryDiagnostics);
        AddCandidate("managed:semantic", primaryProgram, primaryCode, primaryDiagnostics, source, candidates, attempts);

        if (options.EnableFragmentFallback)
        {
            var fragmentDiagnostics = new List<TranspileDiagnostic>();
            var fragmentProgram = ParseByFragments(source, sourceLanguage, options, fragmentDiagnostics);
            SemanticNormalizer.Normalize(fragmentProgram);
            var fragmentCode = CodeEmitter.Emit(fragmentProgram, targetLanguage, options, fragmentDiagnostics);
            AddCandidate("managed:fragment-semantic", fragmentProgram, fragmentCode, fragmentDiagnostics, source, candidates, attempts);
        }

        if (options.EnableRouteRacing)
        {
            var lexicalDiagnostics = new List<TranspileDiagnostic>();
            var lexical = LexicalFallback.TryTranslate(source, sourceLanguage, targetLanguage, options, lexicalDiagnostics);
            if (lexical is not null)
            {
                var lexicalProgram = FrontendParser.Parse(source, sourceLanguage, options, new List<TranspileDiagnostic>());
                SemanticNormalizer.Normalize(lexicalProgram);
                AddCandidate("managed:lexical-rescue", lexicalProgram, lexical, lexicalDiagnostics, source, candidates, attempts);
            }
        }

        return candidates
            .OrderByDescending(x => x.Score)
            .ThenByDescending(x => x.Preserved)
            .ThenBy(x => x.Diagnostics.Count(d => d.Severity == DiagnosticSeverity.Warning))
            .First();
    }

    private static void AddCandidate(string route, SemanticProgram program, string code, List<TranspileDiagnostic> diagnostics, string source, List<Candidate> candidates, List<RouteAttempt> attempts)
    {
        var symbols = ExtractSymbols(source, program.SourceLanguage);
        foreach (var symbol in program.OriginalSymbols)
            if (!symbols.Contains(symbol, StringComparer.Ordinal)) symbols.Add(symbol);
        var preserved = symbols.Count(x => Regex.IsMatch(code, $@"\b{Regex.Escape(x)}\b"));
        var raw = diagnostics.Count(x => x.Code == "CT1001");
        var errors = diagnostics.Count(x => x.Severity == DiagnosticSeverity.Error);
        var warnings = diagnostics.Count(x => x.Severity == DiagnosticSeverity.Warning);
        var emptyPenalty = string.IsNullOrWhiteSpace(code) ? 10000 : 0;
        var score = preserved * 100 - (symbols.Count - preserved) * 80 - raw * 15 - warnings * 2 - errors * 500 - emptyPenalty;
        if (code.Length > 40) score += Math.Min(50, code.Length / 500);
        candidates.Add(new Candidate(route, program, code, diagnostics, score, preserved, symbols.Count));
        attempts.Add(new RouteAttempt(route, errors == 0 && !string.IsNullOrWhiteSpace(code), score, preserved, symbols.Count, errors == 0 ? null : string.Join("; ", diagnostics.Where(x => x.Severity == DiagnosticSeverity.Error).Select(x => x.Message))));
    }

    private static SemanticProgram ParseByFragments(string source, LanguageId language, TranspileOptions options, List<TranspileDiagnostic> diagnostics)
    {
        var merged = new SemanticProgram
        {
            SourceLanguage = language,
            SourceIndexBase = LanguageCatalog.Get(language).IndexBase,
            Evaluation = language == LanguageId.R ? "lazy_demand" : "eager_left_to_right"
        };
        merged.Metadata["frontend"] = "managed-fragment-v1";
        var fragments = SourceFragmenter.Split(source, language, options.MaxFragmentDepth);
        if (fragments.Count <= 1)
            return FrontendParser.Parse(source, language, options, diagnostics);

        foreach (var fragment in fragments)
        {
            var local = new List<TranspileDiagnostic>();
            var unit = FrontendParser.Parse(fragment.Text, language, options, local);
            Merge(merged, unit);
            foreach (var d in local)
                diagnostics.Add(d with { Fragment = fragment.Label, Route = "managed:fragment-semantic" });
        }
        return merged;
    }

    private static void Merge(SemanticProgram target, SemanticProgram source)
    {
        foreach (var import in source.Imports)
            if (!target.Imports.Contains(import)) target.Imports.Add(import);
        foreach (var declaration in source.Declarations)
        {
            if (target.Declarations.All(x => x.Name != declaration.Name || x.GetType() != declaration.GetType()))
                target.Declarations.Add(declaration);
        }
        target.TopLevelStatements.AddRange(source.TopLevelStatements);
        foreach (var symbol in source.OriginalSymbols)
            if (!target.OriginalSymbols.Contains(symbol, StringComparer.Ordinal)) target.OriginalSymbols.Add(symbol);
        foreach (var pair in source.Metadata) target.Metadata[pair.Key] = pair.Value;
    }

    private static List<string> ExtractSymbols(string source, LanguageId language)
    {
        var result = new List<string>();
        var patterns = new[]
        {
            @"\b(?:def|func|fn|function|proc|fun)\s+([A-Za-z_]\w*)",
            @"\b(?:class|struct|record|interface|enum|type)\s+([A-Za-z_]\w*)",
            @"(?m)^\s*([A-Za-z_.][\w.]*)\s*<-\s*function\b"
        };
        foreach (var pattern in patterns)
            foreach (Match m in Regex.Matches(source, pattern))
                if (!result.Contains(m.Groups[1].Value, StringComparer.Ordinal)) result.Add(m.Groups[1].Value);
        return result;
    }
}

internal sealed record SourceFragment(string Label, string Text, int StartLine);

internal static class SourceFragmenter
{
    public static List<SourceFragment> Split(string source, LanguageId language, int maxDepth)
    {
        var result = new List<SourceFragment>();
        if (string.IsNullOrWhiteSpace(source)) return result;
        if (LanguageCatalog.Get(language).SignificantIndentation)
            SplitIndented(source, language, result);
        else
            SplitBalanced(source, language, result);
        if (result.Count == 0) result.Add(new SourceFragment("module", source, 1));
        return result;
    }

    private static void SplitIndented(string source, LanguageId language, List<SourceFragment> output)
    {
        var lines = source.Replace("\r\n", "\n", StringComparison.Ordinal).Split('\n');
        var starts = new List<int>();
        var header = language == LanguageId.Python ? new Regex(@"^(def|class)\s+") : new Regex(@"^(proc|func|type|object)\s+");
        for (var i = 0; i < lines.Length; i++)
            if (lines[i].Length > 0 && !char.IsWhiteSpace(lines[i][0]) && header.IsMatch(lines[i])) starts.Add(i);
        if (starts.Count == 0) return;
        for (var i = 0; i < starts.Count; i++)
        {
            var start = starts[i]; var end = i + 1 < starts.Count ? starts[i + 1] : lines.Length;
            var text = string.Join('\n', lines[start..end]);
            var name = Regex.Match(lines[start], @"(?:def|class|proc|func|type|object)\s+([A-Za-z_]\w*)").Groups[1].Value;
            output.Add(new SourceFragment(name.Length == 0 ? $"fragment_{i}" : name, text, start + 1));
        }
    }

    private static void SplitBalanced(string source, LanguageId language, List<SourceFragment> output)
    {
        var header = language switch
        {
            LanguageId.Go => new Regex(@"(?m)^\s*(?:func|type)\s+(?:\([^)]*\)\s*)?([A-Za-z_]\w*)"),
            LanguageId.Rust => new Regex(@"(?m)^\s*(?:pub\s+)?(?:async\s+)?(?:fn|struct|enum|trait)\s+([A-Za-z_]\w*)"),
            LanguageId.Julia => new Regex(@"(?m)^\s*(?:function|struct|mutable\s+struct)\s+([A-Za-z_]\w*!?)"),
            LanguageId.R => new Regex(@"(?m)^\s*([A-Za-z_.][\w.]*)\s*<-\s*function\b"),
            _ => new Regex(@"(?m)^\s*(?:(?:public|private|protected|internal|static|final|inline|extern|pub|async)\s+)*(?:class|struct|record|interface|enum|fn|func|fun)?\s*[A-Za-z_][\w<>,.?\[\]:*&\s]*\s+([A-Za-z_]\w*)\s*\([^;{}]*\)\s*\{")
        };
        var matches = header.Matches(source).Cast<Match>().ToList();
        if (matches.Count == 0) return;
        for (var i = 0; i < matches.Count; i++)
        {
            var start = matches[i].Index;
            var end = i + 1 < matches.Count ? matches[i + 1].Index : source.Length;
            var label = matches[i].Groups[1].Value.TrimEnd('!');
            output.Add(new SourceFragment(label.Length == 0 ? $"fragment_{i}" : label, source[start..end], 1 + source[..start].Count(c => c == '\n')));
        }
    }
}

internal static class LexicalFallback
{
    public static string? TryTranslate(string source, LanguageId from, LanguageId to, TranspileOptions options, List<TranspileDiagnostic> diagnostics)
    {
        if (from == to) return source;
        var text = source;
        var changed = false;

        if (from == LanguageId.Python && to == LanguageId.CSharp)
        {
            text = Regex.Replace(text, @"\bTrue\b", "true");
            text = Regex.Replace(text, @"\bFalse\b", "false");
            text = Regex.Replace(text, @"\bNone\b", "null");
            text = Regex.Replace(text, @"\bprint\s*\(", "System.Console.WriteLine(");
            changed = text != source;
        }
        else if (from == LanguageId.Java && to == LanguageId.CSharp)
        {
            text = text.Replace("System.out.println", "System.Console.WriteLine", StringComparison.Ordinal)
                       .Replace("boolean ", "bool ", StringComparison.Ordinal)
                       .Replace("String ", "string ", StringComparison.Ordinal);
            changed = text != source;
        }
        else if (from == LanguageId.CSharp && to == LanguageId.Java)
        {
            text = text.Replace("System.Console.WriteLine", "System.out.println", StringComparison.Ordinal)
                       .Replace("bool ", "boolean ", StringComparison.Ordinal)
                       .Replace("string ", "String ", StringComparison.Ordinal);
            changed = text != source;
        }
        if (!changed) return null;
        diagnostics.Add(new TranspileDiagnostic(DiagnosticSeverity.Warning, "CT2001", "A conservative lexical rescue route was used; semantic route remains preferred.", Route: "managed:lexical-rescue"));
        return text;
    }
}
