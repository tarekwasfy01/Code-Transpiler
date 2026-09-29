namespace CodeTranspiler.Managed;

internal enum TokenKind
{
    Identifier, Number, String, Character, Operator, Punctuation, NewLine, Indent, Dedent, Comment, End
}

internal readonly record struct Token(TokenKind Kind, string Text, int Line, int Column)
{
    public bool Is(string text) => string.Equals(Text, text, StringComparison.Ordinal);
}

internal static class Lexer
{
    private static readonly string[] MultiOperators =
    {
        "===", "!==", "<<=", ">>=", "**=", "...", "::", "->", "=>", ":=", "==", "!=", "<=", ">=",
        "&&", "||", "++", "--", "+=", "-=", "*=", "/=", "%=", "<<", ">>", "**", "??", "?.", "..<", "..="
    };

    public static List<Token> Tokenize(string source, LanguageId language)
    {
        var tokens = new List<Token>();
        var significantIndent = LanguageCatalog.Get(language).SignificantIndentation;
        var indents = new Stack<int>();
        indents.Push(0);

        var i = 0;
        var line = 1;
        var column = 1;
        var atLineStart = true;
        while (i < source.Length)
        {
            if (atLineStart && significantIndent)
            {
                var start = i;
                var width = 0;
                while (i < source.Length && (source[i] == ' ' || source[i] == '\t'))
                {
                    width += source[i] == '\t' ? 4 : 1;
                    i++; column++;
                }
                if (i < source.Length && source[i] != '\r' && source[i] != '\n' && !StartsLineComment(source, i, language))
                {
                    if (width > indents.Peek())
                    {
                        indents.Push(width);
                        tokens.Add(new Token(TokenKind.Indent, "<indent>", line, 1));
                    }
                    else
                    {
                        while (width < indents.Peek())
                        {
                            indents.Pop();
                            tokens.Add(new Token(TokenKind.Dedent, "<dedent>", line, 1));
                        }
                    }
                }
                atLineStart = false;
                if (i == start && i < source.Length && source[i] == '\n') { }
            }

            if (i >= source.Length) break;
            var ch = source[i];

            if (ch == '\r') { i++; continue; }
            if (ch == '\n')
            {
                tokens.Add(new Token(TokenKind.NewLine, "\n", line, column));
                i++; line++; column = 1; atLineStart = true; continue;
            }
            if (char.IsWhiteSpace(ch)) { i++; column++; continue; }

            if (TryReadComment(source, ref i, ref line, ref column, language, out var comment))
            {
                tokens.Add(comment);
                continue;
            }

            if (ch == '"' || ch == '\'' || (language == LanguageId.Rust && ch == 'r' && i + 1 < source.Length && source[i + 1] == '"'))
            {
                tokens.Add(ReadString(source, ref i, ref line, ref column, language));
                continue;
            }

            if (char.IsLetter(ch) || ch == '_' || ch == '$')
            {
                var sl = line; var sc = column; var start = i;
                i++; column++;
                while (i < source.Length && (char.IsLetterOrDigit(source[i]) || source[i] is '_' or '$' or '!')) { i++; column++; }
                tokens.Add(new Token(TokenKind.Identifier, source[start..i], sl, sc));
                continue;
            }

            if (char.IsDigit(ch) || (ch == '.' && i + 1 < source.Length && char.IsDigit(source[i + 1])))
            {
                var sl = line; var sc = column; var start = i;
                i++; column++;
                while (i < source.Length && (char.IsLetterOrDigit(source[i]) || source[i] is '.' or '_' or '+' or '-'))
                {
                    if ((source[i] == '+' || source[i] == '-') && source[i - 1] is not 'e' and not 'E') break;
                    i++; column++;
                }
                tokens.Add(new Token(TokenKind.Number, source[start..i], sl, sc));
                continue;
            }

            string? op = null;
            foreach (var candidate in MultiOperators)
            {
                if (i + candidate.Length <= source.Length && source.AsSpan(i, candidate.Length).SequenceEqual(candidate))
                { op = candidate; break; }
            }
            if (op is not null)
            {
                tokens.Add(new Token(TokenKind.Operator, op, line, column));
                i += op.Length; column += op.Length; continue;
            }

            var kind = "(){}[],:;.".Contains(ch) ? TokenKind.Punctuation : TokenKind.Operator;
            tokens.Add(new Token(kind, ch.ToString(), line, column));
            i++; column++;
        }

        if (significantIndent)
        {
            while (indents.Count > 1) { indents.Pop(); tokens.Add(new Token(TokenKind.Dedent, "<dedent>", line, 1)); }
        }
        tokens.Add(new Token(TokenKind.End, "<eof>", line, column));
        return tokens;
    }

    private static bool StartsLineComment(string source, int i, LanguageId language)
    {
        if (language == LanguageId.Python || language == LanguageId.R) return source[i] == '#';
        return i + 1 < source.Length && source[i] == '/' && source[i + 1] == '/';
    }

    private static bool TryReadComment(string source, ref int i, ref int line, ref int column, LanguageId language, out Token token)
    {
        var sl = line; var sc = column;
        if ((language == LanguageId.Python || language == LanguageId.R) && source[i] == '#')
        {
            var start = i;
            while (i < source.Length && source[i] != '\n') { i++; column++; }
            token = new Token(TokenKind.Comment, source[start..i], sl, sc); return true;
        }
        if (i + 1 < source.Length && source[i] == '/' && source[i + 1] == '/')
        {
            var start = i;
            while (i < source.Length && source[i] != '\n') { i++; column++; }
            token = new Token(TokenKind.Comment, source[start..i], sl, sc); return true;
        }
        if (i + 1 < source.Length && source[i] == '/' && source[i + 1] == '*')
        {
            var start = i; i += 2; column += 2;
            while (i + 1 < source.Length)
            {
                if (source[i] == '*' && source[i + 1] == '/') { i += 2; column += 2; break; }
                if (source[i] == '\n') { i++; line++; column = 1; }
                else { i++; column++; }
            }
            token = new Token(TokenKind.Comment, source[start..i], sl, sc); return true;
        }
        token = default; return false;
    }

    private static Token ReadString(string source, ref int i, ref int line, ref int column, LanguageId language)
    {
        var sl = line; var sc = column; var start = i;
        var raw = language == LanguageId.Rust && source[i] == 'r' && i + 1 < source.Length && source[i + 1] == '"';
        if (raw) { i++; column++; }
        var quote = source[i]; i++; column++;
        while (i < source.Length)
        {
            var ch = source[i];
            if (!raw && ch == '\\' && i + 1 < source.Length) { i += 2; column += 2; continue; }
            if (ch == quote) { i++; column++; break; }
            if (ch == '\n') { i++; line++; column = 1; }
            else { i++; column++; }
        }
        var text = source[start..i];
        return new Token(quote == '\'' ? TokenKind.Character : TokenKind.String, text, sl, sc);
    }
}
