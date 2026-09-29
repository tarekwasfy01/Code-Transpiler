using System.Text.RegularExpressions;

namespace CodeTranspiler.Managed;

internal static class StatementParser
{
    public static SemanticBlock ParseBlock(string source, LanguageId language, TranspileOptions options, List<TranspileDiagnostic> diagnostics, int baseLine)
    {
        if (language is LanguageId.Python or LanguageId.Nim)
            return ParseIndented(source, language, options, diagnostics, baseLine);

        var list = new List<SemanticStatement>();
        foreach (var part in SplitStatements(source, baseLine))
        {
            var text = part.Text.Trim();
            if (text.Length == 0) continue;
            if (IsComment(text, language))
            {
                if (options.PreserveComments) list.Add(new SemanticComment(TrimComment(text), part.Line));
                continue;
            }
            var stmt = ParseStatement(text, language, options, diagnostics, part.Line);
            if (stmt is not null) list.Add(stmt);
        }
        return new SemanticBlock(list, baseLine);
    }

    private sealed record IndentedLine(string Text, int Indent, int Line);

    private static SemanticBlock ParseIndented(string source, LanguageId language, TranspileOptions options, List<TranspileDiagnostic> diagnostics, int baseLine)
    {
        var lines = new List<IndentedLine>();
        var raw = source.Replace("\r\n", "\n", StringComparison.Ordinal).Split('\n');
        for (var i = 0; i < raw.Length; i++)
        {
            if (string.IsNullOrWhiteSpace(raw[i])) continue;
            var cut = 0; var indent = 0;
            while (cut < raw[i].Length && (raw[i][cut] == ' ' || raw[i][cut] == '\t'))
            {
                indent += raw[i][cut] == '\t' ? 4 : 1; cut++;
            }
            lines.Add(new IndentedLine(raw[i][cut..].TrimEnd(), indent, baseLine + i));
        }
        if (lines.Count == 0) return new SemanticBlock(Array.Empty<SemanticStatement>(), baseLine);
        var pos = 0;
        return ParseIndentLevel(lines, ref pos, lines.Min(x => x.Indent), language, options, diagnostics, baseLine);
    }

    private static SemanticBlock ParseIndentLevel(IReadOnlyList<IndentedLine> lines, ref int pos, int indent, LanguageId language, TranspileOptions options, List<TranspileDiagnostic> diagnostics, int baseLine)
    {
        var result = new List<SemanticStatement>();
        while (pos < lines.Count)
        {
            var line = lines[pos];
            if (line.Indent < indent) break;
            if (line.Indent > indent) { pos++; continue; }
            var text = line.Text.Trim();

            if (IsComment(text, language))
            {
                if (options.PreserveComments) result.Add(new SemanticComment(TrimComment(text), line.Line));
                pos++; continue;
            }

            var mif = Regex.Match(text, @"^if\s+(?<c>.+)\s*:\s*$");
            if (mif.Success)
            {
                pos++;
                var thenBlock = ParseIndentLevel(lines, ref pos, NextIndent(lines, pos, indent), language, options, diagnostics, line.Line + 1);
                SemanticBlock? elseBlock = null;
                if (pos < lines.Count && lines[pos].Indent == indent)
                {
                    var melif = Regex.Match(lines[pos].Text, @"^elif\s+(?<c>.+)\s*:\s*$");
                    if (melif.Success)
                    {
                        var e = lines[pos++];
                        var eThen = ParseIndentLevel(lines, ref pos, NextIndent(lines, pos, indent), language, options, diagnostics, e.Line + 1);
                        elseBlock = new SemanticBlock(new SemanticStatement[] { new SemanticIf(ExpressionParser.ParseText(melif.Groups["c"].Value, language, e.Line), eThen, null, e.Line) }, e.Line);
                    }
                    else if (Regex.IsMatch(lines[pos].Text, @"^else\s*:\s*$"))
                    {
                        var e = lines[pos++];
                        elseBlock = ParseIndentLevel(lines, ref pos, NextIndent(lines, pos, indent), language, options, diagnostics, e.Line + 1);
                    }
                }
                result.Add(new SemanticIf(ExpressionParser.ParseText(mif.Groups["c"].Value, language, line.Line), thenBlock, elseBlock, line.Line));
                continue;
            }

            var mwhile = Regex.Match(text, @"^while\s+(?<c>.+)\s*:\s*$");
            if (mwhile.Success)
            {
                pos++;
                var body = ParseIndentLevel(lines, ref pos, NextIndent(lines, pos, indent), language, options, diagnostics, line.Line + 1);
                result.Add(new SemanticWhile(ExpressionParser.ParseText(mwhile.Groups["c"].Value, language, line.Line), body, line.Line));
                continue;
            }

            var mfor = Regex.Match(text, @"^for\s+(?<v>[A-Za-z_]\w*)\s+in\s+(?<i>.+)\s*:\s*$");
            if (mfor.Success)
            {
                pos++;
                var body = ParseIndentLevel(lines, ref pos, NextIndent(lines, pos, indent), language, options, diagnostics, line.Line + 1);
                result.Add(new SemanticForEach(mfor.Groups["v"].Value, ExpressionParser.ParseText(mfor.Groups["i"].Value, language, line.Line), body, line.Line));
                continue;
            }

            if (Regex.IsMatch(text, @"^try\s*:\s*$"))
            {
                pos++;
                var body = ParseIndentLevel(lines, ref pos, NextIndent(lines, pos, indent), language, options, diagnostics, line.Line + 1);
                string? catchVar = null; SemanticBlock? catchBlock = null; SemanticBlock? finallyBlock = null;
                if (pos < lines.Count && lines[pos].Indent == indent)
                {
                    var mex = Regex.Match(lines[pos].Text, @"^except(?:\s+[^:]+)?(?:\s+as\s+(?<v>[A-Za-z_]\w*))?\s*:\s*$");
                    if (mex.Success)
                    {
                        var e = lines[pos++];
                        catchVar = mex.Groups["v"].Success ? mex.Groups["v"].Value : "ex";
                        catchBlock = ParseIndentLevel(lines, ref pos, NextIndent(lines, pos, indent), language, options, diagnostics, e.Line + 1);
                    }
                }
                if (pos < lines.Count && lines[pos].Indent == indent && Regex.IsMatch(lines[pos].Text, @"^finally\s*:\s*$"))
                {
                    var f = lines[pos++];
                    finallyBlock = ParseIndentLevel(lines, ref pos, NextIndent(lines, pos, indent), language, options, diagnostics, f.Line + 1);
                }
                result.Add(new SemanticTry(body, catchVar, catchBlock, finallyBlock, line.Line));
                continue;
            }

            if (Regex.IsMatch(text, @"^(else|elif|except|finally)\b")) break;
            var parsed = ParseStatement(text, language, options, diagnostics, line.Line);
            if (parsed is not null) result.Add(parsed);
            pos++;
        }
        return new SemanticBlock(result, baseLine);
    }

    private static int NextIndent(IReadOnlyList<IndentedLine> lines, int pos, int parent)
        => pos < lines.Count && lines[pos].Indent > parent ? lines[pos].Indent : parent + 4;

    private static SemanticStatement? ParseStatement(string text, LanguageId language, TranspileOptions options, List<TranspileDiagnostic> diagnostics, int line)
    {
        text = text.Trim().TrimEnd(';').Trim();
        if (text.Length == 0 || text is "pass" or "noop" or "{") return null;
        if (text == "break") return new SemanticBreak(line);
        if (text == "continue") return new SemanticContinue(line);

        var mr = Regex.Match(text, @"^return(?:\s+(.+))?$", RegexOptions.Singleline);
        if (mr.Success) return new SemanticReturn(mr.Groups[1].Success ? ExpressionParser.ParseText(mr.Groups[1].Value, language, line) : null, line);

        var mt = Regex.Match(text, @"^(?:throw|raise|panic!?)\s*(?:\((.*)\)|(.*))$", RegexOptions.Singleline);
        if (mt.Success)
        {
            var v = mt.Groups[1].Success ? mt.Groups[1].Value : mt.Groups[2].Value;
            return new SemanticThrow(ExpressionParser.ParseText(v, language, line), line);
        }

        if (TryIf(text, language, options, diagnostics, line, out var si)) return si;
        if (TryWhile(text, language, options, diagnostics, line, out var sw)) return sw;
        if (TryForEach(text, language, options, diagnostics, line, out var se)) return se;
        if (TryCFor(text, language, options, diagnostics, line, out var sf)) return sf;
        if (TryVariable(text, language, line, out var sv)) return sv;
        if (TryAssignment(text, language, line, out var sa)) return sa;

        var expr = ExpressionParser.ParseText(text, language, line);
        if (expr is SemanticRawExpression && options.PreserveUnknownFragmentsAsComments)
        {
            diagnostics.Add(new TranspileDiagnostic(DiagnosticSeverity.Warning, "CT1001", "Statement preserved as a raw semantic fragment.", line, Fragment: text));
            return new SemanticRawStatement(text, line);
        }
        return new SemanticExpressionStatement(expr, line);
    }

    private static bool TryVariable(string text, LanguageId language, int line, out SemanticVariableDeclaration variable)
    {
        variable = default!;
        Match m = language switch
        {
            LanguageId.Python => Regex.Match(text, @"^(?<n>[A-Za-z_]\w*)\s*(?::\s*(?<t>[^=]+))?\s*=\s*(?<v>.+)$", RegexOptions.Singleline),
            LanguageId.Go => Regex.Match(text, @"^(?:(?<k>var|const)\s+)?(?<n>[A-Za-z_]\w*)\s*(?<t>[^:=]*?)\s*(?::=|=)\s*(?<v>.+)$", RegexOptions.Singleline),
            LanguageId.Rust => Regex.Match(text, @"^let\s+(?<m>mut\s+)?(?<n>[A-Za-z_]\w*)\s*(?::\s*(?<t>[^=]+))?\s*=\s*(?<v>.+)$", RegexOptions.Singleline),
            LanguageId.Kotlin or LanguageId.Swift or LanguageId.Zig => Regex.Match(text, @"^(?<k>val|var|let|const)\s+(?<n>[A-Za-z_]\w*)\s*(?::\s*(?<t>[^=]+))?\s*=\s*(?<v>.+)$", RegexOptions.Singleline),
            LanguageId.Julia or LanguageId.R or LanguageId.Nim => Regex.Match(text, @"^(?:(?<k>let|var|const)\s+)?(?<n>[A-Za-z_]\w*)\s*(?:::\s*(?<t>[^=]+))?\s*(?:<-|=)\s*(?<v>.+)$", RegexOptions.Singleline),
            LanguageId.CSharp or LanguageId.Java or LanguageId.C or LanguageId.Cpp => Regex.Match(text, @"^(?:(?<k>const|final)\s+)?(?<t>[A-Za-z_][\w<>,.?\[\]:*& ]*)\s+(?<n>[A-Za-z_]\w*)\s*=\s*(?<v>.+)$", RegexOptions.Singleline),
            _ => Match.Empty
        };
        if (!m.Success) return false;
        var mutable = !(m.Groups["k"].Success && m.Groups["k"].Value is "const" or "val" or "let" or "final");
        variable = new SemanticVariableDeclaration(
            m.Groups["n"].Value,
            TypeMapper.Parse(m.Groups["t"].Success ? m.Groups["t"].Value : null, language),
            ExpressionParser.ParseText(m.Groups["v"].Value, language, line),
            mutable, !mutable, line);
        return true;
    }

    private static bool TryAssignment(string text, LanguageId language, int line, out SemanticAssignment assignment)
    {
        assignment = default!;
        var m = Regex.Match(text, @"^(?<t>.+?)\s*(?<o><-|\+=|-=|\*=|/=|%=|:=|=)\s*(?<v>.+)$", RegexOptions.Singleline);
        if (!m.Success) return false;
        var target = m.Groups["t"].Value.Trim();
        if (target.Contains("==", StringComparison.Ordinal) || target.Contains("!=", StringComparison.Ordinal) || target.Contains("<=", StringComparison.Ordinal) || target.Contains(">=", StringComparison.Ordinal)) return false;
        var op = m.Groups["o"].Value is "<-" or ":=" ? "=" : m.Groups["o"].Value;
        assignment = new SemanticAssignment(ExpressionParser.ParseText(target, language, line), op, ExpressionParser.ParseText(m.Groups["v"].Value, language, line), line);
        return true;
    }

    private static bool TryIf(string text, LanguageId language, TranspileOptions options, List<TranspileDiagnostic> diagnostics, int line, out SemanticIf statement)
    {
        statement = default!;
        var m = Regex.Match(text, @"^if\s*\(?(?<c>.*?)\)?\s*\{(?<b>.*)\}\s*(?:else\s*\{(?<e>.*)\})?$", RegexOptions.Singleline);
        if (!m.Success) return false;
        statement = new SemanticIf(ExpressionParser.ParseText(m.Groups["c"].Value, language, line), ParseBlock(m.Groups["b"].Value, language, options, diagnostics, line), m.Groups["e"].Success ? ParseBlock(m.Groups["e"].Value, language, options, diagnostics, line) : null, line);
        return true;
    }

    private static bool TryWhile(string text, LanguageId language, TranspileOptions options, List<TranspileDiagnostic> diagnostics, int line, out SemanticWhile statement)
    {
        statement = default!;
        var m = Regex.Match(text, @"^while\s*\(?(?<c>.*?)\)?\s*\{(?<b>.*)\}$", RegexOptions.Singleline);
        if (!m.Success) return false;
        statement = new SemanticWhile(ExpressionParser.ParseText(m.Groups["c"].Value, language, line), ParseBlock(m.Groups["b"].Value, language, options, diagnostics, line), line);
        return true;
    }

    private static bool TryForEach(string text, LanguageId language, TranspileOptions options, List<TranspileDiagnostic> diagnostics, int line, out SemanticForEach statement)
    {
        statement = default!;
        var patterns = new[]
        {
            @"^for\s+(?<v>[A-Za-z_]\w*)\s+in\s+(?<i>.+?)\s*\{(?<b>.*)\}$",
            @"^for\s*\(?(?:(?:var|let|const|final|auto)\s+)?(?<v>[A-Za-z_]\w*)\s*:\s*(?<i>.+?)\)?\s*\{(?<b>.*)\}$",
            @"^for\s+(?<v>[A-Za-z_]\w*)\s*:=\s*range\s+(?<i>.+?)\s*\{(?<b>.*)\}$"
        };
        foreach (var p in patterns)
        {
            var m = Regex.Match(text, p, RegexOptions.Singleline);
            if (!m.Success) continue;
            statement = new SemanticForEach(m.Groups["v"].Value, ExpressionParser.ParseText(m.Groups["i"].Value, language, line), ParseBlock(m.Groups["b"].Value, language, options, diagnostics, line), line);
            return true;
        }
        return false;
    }

    private static bool TryCFor(string text, LanguageId language, TranspileOptions options, List<TranspileDiagnostic> diagnostics, int line, out SemanticFor statement)
    {
        statement = default!;
        var m = Regex.Match(text, @"^for\s*\((?<i>[^;]*);(?<c>[^;]*);(?<s>[^)]*)\)\s*\{(?<b>.*)\}$", RegexOptions.Singleline);
        if (!m.Success) return false;
        statement = new SemanticFor(
            ParseStatement(m.Groups["i"].Value, language, options, diagnostics, line),
            string.IsNullOrWhiteSpace(m.Groups["c"].Value) ? null : ExpressionParser.ParseText(m.Groups["c"].Value, language, line),
            ParseStatement(m.Groups["s"].Value, language, options, diagnostics, line),
            ParseBlock(m.Groups["b"].Value, language, options, diagnostics, line), line);
        return true;
    }

    private sealed record Logical(string Text, int Line);

    private static List<Logical> SplitStatements(string source, int baseLine)
    {
        var result = new List<Logical>();
        var sb = new System.Text.StringBuilder();
        var round = 0; var square = 0; var curly = 0; var quote = '\0'; var escape = false; var line = baseLine; var start = baseLine;
        for (var i = 0; i < source.Length; i++)
        {
            var c = source[i];
            if (quote != '\0')
            {
                sb.Append(c);
                if (escape) escape = false; else if (c == '\\') escape = true; else if (c == quote) quote = '\0';
                if (c == '\n') line++;
                continue;
            }
            if (c is '\'' or '"' or '`') { quote = c; sb.Append(c); continue; }
            if (c == '(') round++; else if (c == ')') round = Math.Max(0, round - 1);
            else if (c == '[') square++; else if (c == ']') square = Math.Max(0, square - 1);
            else if (c == '{') curly++; else if (c == '}') curly = Math.Max(0, curly - 1);
            sb.Append(c);
            if ((c == ';' || c == '\n') && round == 0 && square == 0 && curly == 0)
            {
                var t = sb.ToString().Trim().TrimEnd(';');
                if (t.Length > 0) result.Add(new Logical(t, start));
                sb.Clear(); start = line + (c == '\n' ? 1 : 0);
            }
            if (c == '\n') line++;
        }
        var tail = sb.ToString().Trim();
        if (tail.Length > 0) result.Add(new Logical(tail, start));
        return result;
    }

    private static bool IsComment(string text, LanguageId language)
        => text.StartsWith("//", StringComparison.Ordinal) || text.StartsWith("/*", StringComparison.Ordinal) || ((language is LanguageId.Python or LanguageId.R) && text.StartsWith('#'));
    private static string TrimComment(string text) => text.Trim().TrimStart('/', '*', '#', ' ').TrimEnd('*', '/', ' ');
}
