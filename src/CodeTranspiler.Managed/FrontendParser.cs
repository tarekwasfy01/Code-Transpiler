using System.Text.RegularExpressions;

namespace CodeTranspiler.Managed;

internal static class FrontendParser
{
    private sealed record FunctionMatch(string Name, string Parameters, string? ReturnType, string Body, int Line, bool IsAsync = false, bool IsPublic = true);
    private sealed record TypeMatch(string Name, SemanticTypeKind Kind, string Body, int Line, int EndLine, IReadOnlyList<string> BaseTypes);

    public static SemanticProgram Parse(string source, LanguageId language, TranspileOptions options, List<TranspileDiagnostic> diagnostics)
    {
        var spec = LanguageCatalog.Get(language);
        var program = new SemanticProgram
        {
            SourceLanguage = language,
            SourceIndexBase = spec.IndexBase,
            Evaluation = language == LanguageId.R ? "lazy_demand" : "eager_left_to_right"
        };
        program.Metadata["frontend"] = "managed-v1";
        program.Metadata["source_language"] = LanguageCatalog.CanonicalName(language);

        ParseImports(source, language, program);
        var types = ScanTypes(source, language, options, diagnostics);
        foreach (var type in types)
        {
            program.Declarations.Add(BuildType(type, language, options, diagnostics));
            if (!program.OriginalSymbols.Contains(type.Name, StringComparer.Ordinal)) program.OriginalSymbols.Add(type.Name);
        }

        var functions = ScanFunctions(source, language, diagnostics)
            .Where(fn => !types.Any(t => fn.Line >= t.Line && fn.Line <= t.EndLine))
            .ToList();
        foreach (var fn in functions)
        {
            var parameters = ParseParameters(fn.Parameters, language, diagnostics, fn.Line);
            var returnType = TypeMapper.Parse(fn.ReturnType, language);
            var body = StatementParser.ParseBlock(fn.Body, language, options, diagnostics, fn.Line);
            program.Declarations.Add(new SemanticFunction(fn.Name, parameters, returnType, body, fn.IsPublic, true, fn.IsAsync, fn.Line));
            if (!program.OriginalSymbols.Contains(fn.Name, StringComparer.Ordinal)) program.OriginalSymbols.Add(fn.Name);
        }

        if (functions.Count == 0 && types.Count == 0)
        {
            var body = StatementParser.ParseBlock(source, language, options, diagnostics, 1);
            program.TopLevelStatements.AddRange(body.Statements);
        }
        return program;
    }

    private static void ParseImports(string source, LanguageId language, SemanticProgram program)
    {
        foreach (var raw in source.Replace("\r\n", "\n", StringComparison.Ordinal).Split('\n'))
        {
            var line = raw.Trim();
            if (line.Length == 0) continue;
            Match m;
            switch (language)
            {
                case LanguageId.Python:
                    m = Regex.Match(line, @"^import\s+([\w.]+)(?:\s+as\s+(\w+))?");
                    if (m.Success) program.Imports.Add(new SemanticImport(m.Groups[1].Value, EmptyToNull(m.Groups[2].Value)));
                    m = Regex.Match(line, @"^from\s+([\w.]+)\s+import\s+(.+)$");
                    if (m.Success) program.Imports.Add(new SemanticImport(m.Groups[1].Value, null, m.Groups[2].Value.Split(',').Select(x => x.Trim()).Where(x => x.Length > 0).ToArray()));
                    break;
                case LanguageId.Go:
                    m = Regex.Match(line, "^import\\s+(?:[\\w.]+\\s+)?[\\\"]([^\\\"]+)[\\\"]");
                    if (m.Success) program.Imports.Add(new SemanticImport(m.Groups[1].Value));
                    break;
                case LanguageId.Rust:
                    m = Regex.Match(line, @"^use\s+([^;]+)");
                    if (m.Success) program.Imports.Add(new SemanticImport(m.Groups[1].Value.Trim()));
                    break;
                case LanguageId.C:
                case LanguageId.Cpp:
                    m = Regex.Match(line, "^#include\\s*[<\\\"]([^>\\\"]+)[>\\\"]");
                    if (m.Success) program.Imports.Add(new SemanticImport(m.Groups[1].Value));
                    break;
                case LanguageId.CSharp:
                    m = Regex.Match(line, @"^using\s+([^;]+)");
                    if (m.Success) program.Imports.Add(new SemanticImport(m.Groups[1].Value.Trim()));
                    break;
                case LanguageId.Java:
                case LanguageId.Kotlin:
                    m = Regex.Match(line, @"^import\s+([^;]+)");
                    if (m.Success) program.Imports.Add(new SemanticImport(m.Groups[1].Value.Trim()));
                    break;
                case LanguageId.Swift:
                    m = Regex.Match(line, @"^import\s+(\w+)");
                    if (m.Success) program.Imports.Add(new SemanticImport(m.Groups[1].Value));
                    break;
                case LanguageId.Julia:
                    m = Regex.Match(line, @"^(?:using|import)\s+(.+)$");
                    if (m.Success) program.Imports.Add(new SemanticImport(m.Groups[1].Value.Trim()));
                    break;
                case LanguageId.Nim:
                    m = Regex.Match(line, @"^import\s+(.+)$");
                    if (m.Success) program.Imports.Add(new SemanticImport(m.Groups[1].Value.Trim()));
                    break;
            }
        }
    }

    private static List<TypeMatch> ScanTypes(string source, LanguageId language, TranspileOptions options, List<TranspileDiagnostic> diagnostics)
    {
        var result = new List<TypeMatch>();
        if (language == LanguageId.Python)
        {
            var lines = source.Replace("\r\n", "\n", StringComparison.Ordinal).Split('\n');
            var rx = new Regex(@"^(?<indent>[ \t]*)class\s+(?<name>[A-Za-z_]\w*)\s*(?:\((?<bases>[^)]*)\))?\s*:\s*$");
            for (var i = 0; i < lines.Length; i++)
            {
                var m = rx.Match(lines[i]); if (!m.Success) continue;
                var baseIndent = CountIndent(m.Groups["indent"].Value); var body = new List<string>(); var j = i + 1;
                for (; j < lines.Length; j++)
                {
                    if (string.IsNullOrWhiteSpace(lines[j])) { body.Add(string.Empty); continue; }
                    if (CountIndent(lines[j]) <= baseIndent) break;
                    var line = lines[j]; var cut = 0; var width = 0;
                    while (cut < line.Length && width <= baseIndent && (line[cut] == ' ' || line[cut] == '\t')) { width += line[cut] == '\t' ? 4 : 1; cut++; }
                    body.Add(line[cut..]);
                }
                var bases = m.Groups["bases"].Success ? SplitTopLevel(m.Groups["bases"].Value, ',').Select(x => x.Trim()).Where(x => x.Length > 0).ToArray() : Array.Empty<string>();
                result.Add(new TypeMatch(m.Groups["name"].Value, SemanticTypeKind.Class, string.Join('\n', body), i + 1, Math.Max(i + 1, j), bases));
                i = Math.Max(i, j - 1);
            }
            return result;
        }

        if (language == LanguageId.Julia)
        {
            var lines = source.Replace("\r\n", "\n", StringComparison.Ordinal).Split('\n');
            var rx = new Regex(@"^\s*(?<mutable>mutable\s+)?struct\s+(?<name>[A-Za-z_]\w*)");
            for (var i = 0; i < lines.Length; i++)
            {
                var m = rx.Match(lines[i]); if (!m.Success) continue;
                var body = new List<string>(); var depth = 1; var j = i + 1;
                for (; j < lines.Length; j++)
                {
                    var t = lines[j].Trim();
                    if (Regex.IsMatch(t, @"^(struct|mutable\s+struct|function|if|for|while|try|begin|let)\b")) depth++;
                    if (t == "end") { depth--; if (depth == 0) break; }
                    body.Add(lines[j]);
                }
                result.Add(new TypeMatch(m.Groups["name"].Value, SemanticTypeKind.Struct, string.Join('\n', body), i + 1, Math.Max(i + 1, j + 1), Array.Empty<string>()));
                i = Math.Max(i, j);
            }
            return result;
        }

        Regex? header = language switch
        {
            LanguageId.Rust => new Regex(@"(?m)^\s*(?:pub\s+)?(?<kind>struct|enum|trait)\s+(?<name>[A-Za-z_]\w*)[^\{]*\{"),
            LanguageId.Go => new Regex(@"(?m)^\s*type\s+(?<name>[A-Za-z_]\w*)\s+(?<kind>struct|interface)\s*\{"),
            LanguageId.CSharp => new Regex(@"(?m)^\s*(?:(?:public|private|protected|internal|static|abstract|sealed|partial)\s+)*(?<kind>class|struct|interface|enum|record)\s+(?<name>[A-Za-z_]\w*)(?<tail>[^\{]*)\{"),
            LanguageId.Java => new Regex(@"(?m)^\s*(?:(?:public|private|protected|abstract|final|static)\s+)*(?<kind>class|interface|enum|record)\s+(?<name>[A-Za-z_]\w*)(?<tail>[^\{]*)\{"),
            LanguageId.Kotlin => new Regex(@"(?m)^\s*(?:(?:public|private|protected|internal|open|sealed|data|abstract)\s+)*(?<kind>class|interface|object|enum\s+class|data\s+class)\s+(?<name>[A-Za-z_]\w*)(?<tail>[^\{]*)\{"),
            LanguageId.Swift => new Regex(@"(?m)^\s*(?:(?:public|private|internal|fileprivate|open|final)\s+)*(?<kind>class|struct|protocol|enum)\s+(?<name>[A-Za-z_]\w*)(?<tail>[^\{]*)\{"),
            LanguageId.C or LanguageId.Cpp => new Regex(@"(?m)^\s*(?<kind>class|struct|enum)\s+(?<name>[A-Za-z_]\w*)(?<tail>[^\{]*)\{"),
            LanguageId.Zig => new Regex(@"(?m)^\s*(?:pub\s+)?const\s+(?<name>[A-Za-z_]\w*)\s*=\s*(?<kind>struct|enum|union)\s*\{"),
            _ => null
        };
        if (header is null) return result;
        foreach (Match m in header.Matches(source))
        {
            var open = source.IndexOf('{', m.Index + m.Length - 1); if (open < 0) continue;
            var close = FindMatchingBrace(source, open); if (close < 0) continue;
            var kindText = m.Groups["kind"].Value.ToLowerInvariant();
            var kind = kindText.Contains("interface") || kindText.Contains("protocol") || kindText.Contains("trait") ? SemanticTypeKind.Interface
                : kindText.Contains("enum") ? SemanticTypeKind.Enum
                : kindText.Contains("record") ? SemanticTypeKind.Record
                : kindText.Contains("struct") || kindText.Contains("union") ? SemanticTypeKind.Struct
                : SemanticTypeKind.Class;
            var tail = m.Groups["tail"].Success ? m.Groups["tail"].Value : string.Empty;
            var bases = ParseBaseTypes(tail, language);
            result.Add(new TypeMatch(m.Groups["name"].Value, kind, source[(open + 1)..close], LineAt(source, m.Index), LineAt(source, close), bases));
        }
        return result;
    }

    private static SemanticTypeDeclaration BuildType(TypeMatch type, LanguageId language, TranspileOptions options, List<TranspileDiagnostic> diagnostics)
    {
        var methods = ScanFunctions(type.Body, language, diagnostics).Select(fn => new SemanticFunction(
            fn.Name,
            ParseParameters(fn.Parameters, language, diagnostics, type.Line + fn.Line - 1),
            TypeMapper.Parse(fn.ReturnType, language),
            StatementParser.ParseBlock(fn.Body, language, options, diagnostics, type.Line + fn.Line - 1),
            fn.IsPublic, false, fn.IsAsync, type.Line + fn.Line - 1)).ToArray();
        var fields = ScanFields(type.Body, language, type.Line);
        return new SemanticTypeDeclaration(type.Name, type.Kind, fields, methods, type.BaseTypes, type.Line);
    }

    private static IReadOnlyList<SemanticField> ScanFields(string body, LanguageId language, int baseLine)
    {
        var fields = new List<SemanticField>();
        var lines = body.Replace("\r\n", "\n", StringComparison.Ordinal).Split('\n');
        for (var i = 0; i < lines.Length; i++)
        {
            var t = lines[i].Trim().TrimEnd(';', ','); if (t.Length == 0) continue;
            Match m = language switch
            {
                LanguageId.Python => Regex.Match(t, @"^(?<name>[A-Za-z_]\w*)\s*(?::\s*(?<type>[^=]+))?(?:\s*=\s*(?<value>.+))?$"),
                LanguageId.Rust => Regex.Match(t, @"^(?:pub\s+)?(?<name>[A-Za-z_]\w*)\s*:\s*(?<type>.+)$"),
                LanguageId.Go => Regex.Match(t, @"^(?<name>[A-Za-z_]\w*)\s+(?<type>[^=]+)$"),
                LanguageId.CSharp or LanguageId.Java or LanguageId.C or LanguageId.Cpp => Regex.Match(t, @"^(?:(?:public|private|protected|internal|static|readonly|const|final)\s+)*(?<type>[A-Za-z_][\w<>,.?\[\]:*& ]*)\s+(?<name>[A-Za-z_]\w*)(?:\s*=\s*(?<value>.+))?$"),
                LanguageId.Kotlin or LanguageId.Swift => Regex.Match(t, @"^(?:(?:public|private|protected|internal|static)\s+)*(?:var|val|let)\s+(?<name>[A-Za-z_]\w*)\s*(?::\s*(?<type>[^=]+))?(?:\s*=\s*(?<value>.+))?$"),
                LanguageId.Zig => Regex.Match(t, @"^(?<name>[A-Za-z_]\w*)\s*:\s*(?<type>[^=]+)(?:\s*=\s*(?<value>.+))?$"),
                LanguageId.Julia => Regex.Match(t, @"^(?<name>[A-Za-z_]\w*)\s*(?:::\s*(?<type>.+))?$"),
                _ => Match.Empty
            };
            if (!m.Success) continue;
            var name = m.Groups["name"].Value; if (name.Length == 0 || name is "return" or "if" or "for" or "while") continue;
            var typeText = m.Groups["type"].Success ? m.Groups["type"].Value.Trim() : null;
            var init = m.Groups["value"].Success ? ExpressionParser.ParseText(m.Groups["value"].Value.Trim(), language, baseLine + i) : null;
            if (!fields.Any(x => x.Name == name)) fields.Add(new SemanticField(name, TypeMapper.Parse(typeText, language), init, !t.Contains("const", StringComparison.Ordinal)));
        }
        return fields;
    }

    private static IReadOnlyList<string> ParseBaseTypes(string tail, LanguageId language)
    {
        if (string.IsNullOrWhiteSpace(tail)) return Array.Empty<string>();
        var text = tail.Trim();
        if (language is LanguageId.CSharp or LanguageId.Swift or LanguageId.Kotlin)
        {
            var colon = text.IndexOf(':'); if (colon >= 0) return SplitTopLevel(text[(colon + 1)..], ',').Select(x => x.Trim()).Where(x => x.Length > 0).ToArray();
        }
        if (language == LanguageId.Java)
        {
            var m = Regex.Match(text, @"(?:extends|implements)\s+(.+)$"); if (m.Success) return Regex.Split(m.Groups[1].Value, @"\s*,\s*|\s+implements\s+").Where(x => x.Length > 0).ToArray();
        }
        if (language == LanguageId.Cpp || language == LanguageId.C)
        {
            var colon = text.IndexOf(':'); if (colon >= 0) return SplitTopLevel(text[(colon + 1)..], ',').Select(x => Regex.Replace(x, @"\b(public|private|protected|virtual)\b", string.Empty).Trim()).Where(x => x.Length > 0).ToArray();
        }
        return Array.Empty<string>();
    }

    private static List<FunctionMatch> ScanFunctions(string source, LanguageId language, List<TranspileDiagnostic> diagnostics)
    {
        return language switch
        {
            LanguageId.Python => ScanIndentFunctions(source, new Regex(@"^(?<indent>[ \t]*)def\s+(?<name>[A-Za-z_]\w*)\s*\((?<params>[^)]*)\)\s*(?:->\s*(?<ret>[^:]+))?\s*:\s*$", RegexOptions.Multiline), language),
            LanguageId.Nim => ScanIndentFunctions(source, new Regex(@"^(?<indent>[ \t]*)(?:proc|func)\s+(?<name>[A-Za-z_]\w*)\*?\s*\((?<params>[^)]*)\)\s*(?::\s*(?<ret>[^=]+))?\s*=\s*$", RegexOptions.Multiline), language),
            LanguageId.Go => ScanBraceFunctions(source, new Regex(@"\bfunc\s+(?:\([^)]*\)\s*)?(?<name>[A-Za-z_]\w*)\s*\((?<params>[^)]*)\)\s*(?<ret>[^\{\n]*)\{", RegexOptions.Multiline)),
            LanguageId.Rust => ScanBraceFunctions(source, new Regex(@"(?m)^[ \t]*(?<pub>pub\s+)?(?<async>async\s+)?fn\s+(?<name>[A-Za-z_]\w*)\s*(?:<[^>{}]*>)?\s*\((?<params>[^)]*)\)\s*(?:->\s*(?<ret>[^\{]+))?\s*\{")),
            LanguageId.Zig => ScanBraceFunctions(source, new Regex(@"(?m)^[ \t]*(?<pub>pub\s+)?fn\s+(?<name>[A-Za-z_]\w*)\s*\((?<params>[^)]*)\)\s*(?<ret>[^\{\n]*)\{")),
            LanguageId.CSharp => ScanBraceFunctions(source, new Regex(@"(?m)^[ \t]*(?:(?:public|private|protected|internal|static|virtual|override|async|sealed|extern|unsafe|partial)\s+)*(?<ret>[A-Za-z_][\w<>,.?\[\] :]*)\s+(?<name>[A-Za-z_]\w*)\s*\((?<params>[^)]*)\)\s*\{")),
            LanguageId.Java => ScanBraceFunctions(source, new Regex(@"(?m)^[ \t]*(?:(?:public|private|protected|static|final|synchronized|native|abstract)\s+)*(?<ret>[A-Za-z_][\w<>,.?\[\] ]*)\s+(?<name>[A-Za-z_]\w*)\s*\((?<params>[^)]*)\)\s*(?:throws[^\{]+)?\{")),
            LanguageId.Kotlin => ScanBraceFunctions(source, new Regex(@"(?m)^[ \t]*(?:(?:public|private|protected|internal|suspend|inline|operator|override|open)\s+)*fun\s+(?<name>[A-Za-z_]\w*)\s*\((?<params>[^)]*)\)\s*(?::\s*(?<ret>[^\{=]+))?\s*\{")),
            LanguageId.Swift => ScanBraceFunctions(source, new Regex(@"(?m)^[ \t]*(?:(?:public|private|internal|fileprivate|open|static|class|mutating|async)\s+)*func\s+(?<name>[A-Za-z_]\w*)\s*\((?<params>[^)]*)\)\s*(?:async\s*)?(?:throws\s*)?(?:->\s*(?<ret>[^\{]+))?\s*\{")),
            LanguageId.C or LanguageId.Cpp => ScanBraceFunctions(source, new Regex(@"(?m)^[ \t]*(?:(?:static|inline|extern|constexpr|consteval|virtual|friend)\s+)*(?<ret>[A-Za-z_][\w:<>,*&\s\[\]]*?)\s+(?<name>[A-Za-z_]\w*)\s*\((?<params>[^;{}()]*)\)\s*(?:const\s*)?\{")),
            LanguageId.Julia => ScanEndFunctions(source, new Regex(@"(?m)^[ \t]*function\s+(?<name>[A-Za-z_]\w*!?)\s*\((?<params>[^)]*)\)\s*(?:::\s*(?<ret>[^\n]+))?\s*$")),
            LanguageId.R => ScanRFunctions(source),
            _ => new List<FunctionMatch>()
        };
    }

    private static List<FunctionMatch> ScanBraceFunctions(string source, Regex header)
    {
        var result = new List<FunctionMatch>();
        foreach (Match m in header.Matches(source))
        {
            var open = source.IndexOf('{', m.Index + m.Length - 1);
            if (open < 0) continue;
            var close = FindMatchingBrace(source, open);
            if (close < 0) continue;
            result.Add(new FunctionMatch(
                m.Groups["name"].Value,
                m.Groups["params"].Value,
                EmptyToNull(m.Groups["ret"].Value),
                source[(open + 1)..close],
                LineAt(source, m.Index),
                m.Groups["async"].Success,
                !m.Groups["pub"].Success || m.Groups["pub"].Value.Length > 0));
        }
        return Deduplicate(result);
    }

    private static List<FunctionMatch> ScanIndentFunctions(string source, Regex header, LanguageId language)
    {
        var lines = source.Replace("\r\n", "\n", StringComparison.Ordinal).Split('\n');
        var result = new List<FunctionMatch>();
        for (var i = 0; i < lines.Length; i++)
        {
            var m = header.Match(lines[i]);
            if (!m.Success) continue;
            var baseIndent = CountIndent(m.Groups["indent"].Value);
            var body = new List<string>();
            var j = i + 1;
            for (; j < lines.Length; j++)
            {
                if (string.IsNullOrWhiteSpace(lines[j])) { body.Add(string.Empty); continue; }
                var indent = CountIndent(lines[j]);
                if (indent <= baseIndent) break;
                var remove = Math.Min(lines[j].Length, baseIndent + 4);
                var line = lines[j];
                var cut = 0; var width = 0;
                while (cut < line.Length && width < baseIndent + 1 && (line[cut] == ' ' || line[cut] == '\t')) { width += line[cut] == '\t' ? 4 : 1; cut++; }
                body.Add(line[cut..]);
            }
            result.Add(new FunctionMatch(m.Groups["name"].Value, m.Groups["params"].Value, EmptyToNull(m.Groups["ret"].Value), string.Join('\n', body), i + 1));
            i = Math.Max(i, j - 1);
        }
        return result;
    }

    private static List<FunctionMatch> ScanEndFunctions(string source, Regex header)
    {
        var lines = source.Replace("\r\n", "\n", StringComparison.Ordinal).Split('\n');
        var result = new List<FunctionMatch>();
        for (var i = 0; i < lines.Length; i++)
        {
            var m = header.Match(lines[i]);
            if (!m.Success) continue;
            var depth = 1; var body = new List<string>(); var j = i + 1;
            for (; j < lines.Length; j++)
            {
                var t = lines[j].Trim();
                if (Regex.IsMatch(t, @"^(function|if|for|while|try|let|begin|struct|mutable\s+struct)\b")) depth++;
                if (t == "end") { depth--; if (depth == 0) break; }
                body.Add(lines[j]);
            }
            result.Add(new FunctionMatch(m.Groups["name"].Value.TrimEnd('!'), m.Groups["params"].Value, EmptyToNull(m.Groups["ret"].Value), string.Join('\n', body), i + 1));
            i = Math.Max(i, j);
        }
        return result;
    }

    private static List<FunctionMatch> ScanRFunctions(string source)
    {
        var regex = new Regex(@"(?m)^[ \t]*(?<name>[A-Za-z_.][\w.]*)\s*<-\s*function\s*\((?<params>[^)]*)\)\s*\{");
        return ScanBraceFunctions(source, regex);
    }

    private static List<SemanticParameter> ParseParameters(string text, LanguageId language, List<TranspileDiagnostic> diagnostics, int line)
    {
        var result = new List<SemanticParameter>();
        foreach (var part in SplitTopLevel(text, ','))
        {
            var p = part.Trim(); if (p.Length == 0 || p == "void") continue;
            var variadic = p.Contains("...", StringComparison.Ordinal) || p.StartsWith('*');
            p = p.Replace("...", string.Empty, StringComparison.Ordinal).Trim();
            string name; string? type = null; string? defaultText = null;
            var eq = FindTopLevel(p, '=');
            if (eq >= 0) { defaultText = p[(eq + 1)..].Trim(); p = p[..eq].Trim(); }

            switch (language)
            {
                case LanguageId.Python:
                    var colonPy = FindTopLevel(p, ':');
                    if (colonPy >= 0) { name = p[..colonPy].Trim().TrimStart('*'); type = p[(colonPy + 1)..].Trim(); }
                    else name = p.TrimStart('*');
                    break;
                case LanguageId.Go:
                    var piecesGo = p.Split((char[]?)null, StringSplitOptions.RemoveEmptyEntries);
                    if (piecesGo.Length >= 2) { name = piecesGo[0]; type = string.Join(' ', piecesGo.Skip(1)); }
                    else { name = piecesGo.FirstOrDefault() ?? "arg"; }
                    break;
                case LanguageId.Rust:
                case LanguageId.CSharp:
                case LanguageId.Kotlin:
                case LanguageId.Swift:
                case LanguageId.Zig:
                case LanguageId.Nim:
                    var colon = FindTopLevel(p, ':');
                    if (colon >= 0)
                    {
                        var left = p[..colon].Trim(); name = left.Split(' ', StringSplitOptions.RemoveEmptyEntries).LastOrDefault() ?? "arg";
                        type = p[(colon + 1)..].Trim();
                    }
                    else if (language == LanguageId.CSharp)
                    {
                        var pcs = p.Split(' ', StringSplitOptions.RemoveEmptyEntries); name = pcs.LastOrDefault() ?? "arg"; if (pcs.Length > 1) type = string.Join(' ', pcs[..^1]);
                    }
                    else name = p.Split(' ', StringSplitOptions.RemoveEmptyEntries).LastOrDefault() ?? "arg";
                    break;
                case LanguageId.C:
                case LanguageId.Cpp:
                case LanguageId.Java:
                    var pcs2 = p.Split(' ', StringSplitOptions.RemoveEmptyEntries); name = pcs2.LastOrDefault()?.TrimStart('*', '&') ?? "arg"; if (pcs2.Length > 1) type = string.Join(' ', pcs2[..^1]);
                    break;
                default:
                    var colonOther = FindTopLevel(p, ':');
                    if (colonOther >= 0) { name = p[..colonOther].Trim(); type = p[(colonOther + 1)..].Trim(); }
                    else name = p;
                    break;
            }
            var defaultValue = defaultText is null ? null : ExpressionParser.ParseText(defaultText, language, line);
            if (name.Length > 0) result.Add(new SemanticParameter(SanitizeName(name), TypeMapper.Parse(type, language), defaultValue, variadic));
        }
        return result;
    }

    internal static IEnumerable<string> SplitTopLevel(string text, char separator)
    {
        var start = 0; var round = 0; var square = 0; var curly = 0; var quote = '\0'; var escape = false;
        for (var i = 0; i < text.Length; i++)
        {
            var c = text[i];
            if (quote != '\0') { if (escape) escape = false; else if (c == '\\') escape = true; else if (c == quote) quote = '\0'; continue; }
            if (c is '\'' or '"') { quote = c; continue; }
            if (c == '(') round++; else if (c == ')') round--; else if (c == '[') square++; else if (c == ']') square--; else if (c == '{') curly++; else if (c == '}') curly--;
            else if (c == separator && round == 0 && square == 0 && curly == 0) { yield return text[start..i]; start = i + 1; }
        }
        yield return text[start..];
    }

    internal static int FindTopLevel(string text, char needle)
    {
        var index = 0;
        foreach (var part in SplitTopLevel(text, needle)) { if (index + part.Length < text.Length) return index + part.Length; index += part.Length + 1; }
        return -1;
    }

    private static int FindMatchingBrace(string source, int open)
    {
        var depth = 0; var quote = '\0'; var escape = false; var lineComment = false; var blockComment = false;
        for (var i = open; i < source.Length; i++)
        {
            var c = source[i]; var n = i + 1 < source.Length ? source[i + 1] : '\0';
            if (lineComment) { if (c == '\n') lineComment = false; continue; }
            if (blockComment) { if (c == '*' && n == '/') { blockComment = false; i++; } continue; }
            if (quote != '\0') { if (escape) escape = false; else if (c == '\\') escape = true; else if (c == quote) quote = '\0'; continue; }
            if (c is '\'' or '"' or '`') { quote = c; continue; }
            if (c == '/' && n == '/') { lineComment = true; i++; continue; }
            if (c == '/' && n == '*') { blockComment = true; i++; continue; }
            if (c == '{') depth++; else if (c == '}' && --depth == 0) return i;
        }
        return -1;
    }

    private static int LineAt(string text, int index) { var end = Math.Clamp(index, 0, text.Length); var count = 1; for (var i = 0; i < end; i++) if (text[i] == '\n') count++; return count; }
    private static int CountIndent(string text) => text.Sum(c => c == '\t' ? 4 : 1);
    private static string? EmptyToNull(string value) => string.IsNullOrWhiteSpace(value) ? null : value.Trim();
    private static string SanitizeName(string name) => name.Trim().TrimStart('&', '*').TrimEnd(',', ';');
    private static List<FunctionMatch> Deduplicate(List<FunctionMatch> source) => source.GroupBy(x => (x.Name, x.Line)).Select(x => x.First()).ToList();
}

internal static class TypeMapper
{
    public static SemanticTypeRef Parse(string? type, LanguageId language)
    {
        if (string.IsNullOrWhiteSpace(type)) return SemanticTypeRef.Unknown;
        var raw = type.Trim();
        raw = Regex.Replace(raw, @"\b(const|mut|ref|in|out|var|let|final|static)\b", string.Empty).Trim();
        raw = raw.Trim('&', '*', ' ');
        var lower = raw.ToLowerInvariant().Replace("system.", string.Empty, StringComparison.Ordinal);
        if (lower is "void" or "unit" or "()") return SemanticTypeRef.Void;
        if (lower is "bool" or "boolean") return SemanticTypeRef.Bool;
        if (lower is "int" or "i32" or "int32" or "integer" or "int32_t" or "isize") return SemanticTypeRef.Int;
        if (lower is "long" or "i64" or "int64" or "int64_t" or "usize" or "uint64") return SemanticTypeRef.Int64;
        if (lower is "float" or "double" or "f32" or "f64" or "float64" or "real") return SemanticTypeRef.Float64;
        if (lower is "string" or "str" or "&str" or "char*" or "cstring") return SemanticTypeRef.String;
        if (lower is "object" or "any" or "dynamic" or "anyref") return SemanticTypeRef.Object;
        if (lower.EndsWith("[]", StringComparison.Ordinal)) return SemanticTypeRef.ListOf(Parse(raw[..^2], language));
        var generic = Regex.Match(raw, @"^(?:List|Vec|Vector|Array|IList|MutableList|Sequence|seq)\s*[<{\[]\s*(.+?)\s*[>}\]]$");
        if (generic.Success) return SemanticTypeRef.ListOf(Parse(generic.Groups[1].Value, language));
        var optional = lower.EndsWith('?');
        if (optional) raw = raw[..^1];
        return new SemanticTypeRef(raw, optional);
    }
}
