using System.Globalization;

namespace CodeTranspiler.Managed;

internal sealed class ExpressionParser
{
    private readonly IReadOnlyList<Token> _tokens;
    private readonly LanguageId _language;
    private int _position;

    private static readonly Dictionary<string, int> Precedence = new(StringComparer.Ordinal)
    {
        ["or"] = 1, ["||"] = 1, ["|"] = 2,
        ["and"] = 3, ["&&"] = 3, ["&"] = 4,
        ["=="] = 5, ["!="] = 5, ["==="] = 5, ["!=="] = 5,
        ["<"] = 6, ["<="] = 6, [">"] = 6, [">="] = 6, ["in"] = 6,
        ["<<"] = 7, [">>"] = 7,
        ["+"] = 8, ["-"] = 8,
        ["*"] = 9, ["/"] = 9, ["%"] = 9, ["//"] = 9,
        ["**"] = 10, ["^"] = 10
    };

    public ExpressionParser(IReadOnlyList<Token> tokens, LanguageId language)
    {
        _tokens = tokens;
        _language = language;
    }

    public static SemanticExpression ParseText(string text, LanguageId language, int? sourceLine = null)
    {
        try
        {
            var tokens = Lexer.Tokenize(text, language)
                .Where(x => x.Kind is not TokenKind.NewLine and not TokenKind.Indent and not TokenKind.Dedent and not TokenKind.Comment)
                .ToList();
            var parser = new ExpressionParser(tokens, language);
            return parser.ParseExpression();
        }
        catch
        {
            return new SemanticRawExpression(text.Trim(), sourceLine);
        }
    }

    public SemanticExpression ParseExpression(int minPrecedence = 0)
    {
        var left = ParsePrefix();
        left = ParsePostfix(left);

        while (!AtEnd)
        {
            var token = Peek();
            var op = NormalizeOperator(token.Text);
            if (!Precedence.TryGetValue(op, out var precedence) || precedence < minPrecedence) break;
            Next();
            var nextMin = op is "**" or "^" ? precedence : precedence + 1;
            var right = ParseExpression(nextMin);
            left = new SemanticBinary(left, op, right, token.Line);
        }

        if (Match("?"))
        {
            var whenTrue = ParseExpression();
            Expect(":");
            var whenFalse = ParseExpression();
            left = new SemanticConditional(left, whenTrue, whenFalse);
        }
        return left;
    }

    private SemanticExpression ParsePrefix()
    {
        var token = Next();
        if (token.Kind == TokenKind.Number) return ParseNumber(token);
        if (token.Kind is TokenKind.String or TokenKind.Character) return ParseString(token);

        if (token.Kind == TokenKind.Identifier)
        {
            var lower = token.Text.ToLowerInvariant();
            if (lower is "true" or "false") return new SemanticLiteral(lower == "true", "bool", token.Line);
            if (lower is "none" or "null" or "nil" or "nothing" or "nullptr") return new SemanticLiteral(null, "null", token.Line);
            if (lower == "not") return new SemanticUnary("!", ParseExpression(11), true, token.Line);
            return new SemanticIdentifier(token.Text, token.Line);
        }

        if (token.Text is "!" or "-" or "+" or "~" or "&" or "*")
            return new SemanticUnary(token.Text, ParseExpression(11), true, token.Line);

        if (token.Text == "(")
        {
            var inner = ParseExpression();
            if (Match(","))
            {
                var items = new List<SemanticExpression> { inner };
                do { items.Add(ParseExpression()); } while (Match(","));
                Expect(")");
                return new SemanticArray(items, token.Line);
            }
            Expect(")");
            return inner;
        }

        if (token.Text == "[")
        {
            var items = new List<SemanticExpression>();
            if (!Match("]"))
            {
                do { items.Add(ParseExpression()); } while (Match(","));
                Expect("]");
            }
            return new SemanticArray(items, token.Line);
        }

        if (token.Text == "{")
        {
            var items = new List<(SemanticExpression, SemanticExpression)>();
            if (!Match("}"))
            {
                do
                {
                    var key = ParseExpression();
                    Expect(":");
                    var value = ParseExpression();
                    items.Add((key, value));
                } while (Match(","));
                Expect("}");
            }
            return new SemanticMap(items, token.Line);
        }

        return new SemanticRawExpression(token.Text, token.Line);
    }

    private SemanticExpression ParsePostfix(SemanticExpression expression)
    {
        while (!AtEnd)
        {
            if (Match("("))
            {
                var args = new List<SemanticExpression>();
                Dictionary<string, SemanticExpression>? named = null;
                if (!Match(")"))
                {
                    do
                    {
                        if (Peek().Kind == TokenKind.Identifier && Peek(1).Text is "=" or ":")
                        {
                            var name = Next().Text; Next();
                            named ??= new Dictionary<string, SemanticExpression>(StringComparer.Ordinal);
                            named[name] = ParseExpression();
                        }
                        else args.Add(ParseExpression());
                    } while (Match(","));
                    Expect(")");
                }
                expression = new SemanticCall(expression, args, named, expression.SourceLine);
                continue;
            }
            if (Match(".") || Match("?."))
            {
                var member = Next();
                expression = new SemanticMember(expression, member.Text, member.Line);
                continue;
            }
            if (Match("::"))
            {
                var member = Next();
                expression = new SemanticMember(expression, member.Text, member.Line);
                continue;
            }
            if (Match("["))
            {
                if (Match(":"))
                {
                    SemanticExpression? end = Peek().Text == "]" ? null : ParseExpression();
                    SemanticExpression? step = null;
                    if (Match(":")) step = Peek().Text == "]" ? null : ParseExpression();
                    Expect("]");
                    expression = new SemanticSlice(expression, null, end, step, expression.SourceLine);
                    continue;
                }
                var first = ParseExpression();
                if (Match(":"))
                {
                    SemanticExpression? end = Peek().Text == "]" ? null : ParseExpression();
                    SemanticExpression? step = null;
                    if (Match(":")) step = Peek().Text == "]" ? null : ParseExpression();
                    Expect("]");
                    expression = new SemanticSlice(expression, first, end, step, expression.SourceLine);
                }
                else
                {
                    Expect("]");
                    expression = new SemanticIndex(expression, first, expression.SourceLine);
                }
                continue;
            }
            break;
        }
        return expression;
    }

    private static SemanticExpression ParseNumber(Token token)
    {
        var cleaned = token.Text.Replace("_", string.Empty, StringComparison.Ordinal);
        var suffixTrimmed = cleaned.TrimEnd('f', 'F', 'd', 'D', 'l', 'L', 'u', 'U', 'i', 'I');
        if (suffixTrimmed.StartsWith("0x", StringComparison.OrdinalIgnoreCase) && long.TryParse(suffixTrimmed[2..], NumberStyles.HexNumber, CultureInfo.InvariantCulture, out var hex))
            return new SemanticLiteral(hex, "int64", token.Line);
        if (long.TryParse(suffixTrimmed, NumberStyles.Integer, CultureInfo.InvariantCulture, out var integer))
            return new SemanticLiteral(integer, "int64", token.Line);
        if (double.TryParse(suffixTrimmed, NumberStyles.Float, CultureInfo.InvariantCulture, out var number))
            return new SemanticLiteral(number, "float64", token.Line);
        return new SemanticRawExpression(token.Text, token.Line);
    }

    private static SemanticExpression ParseString(Token token)
    {
        var text = token.Text;
        if (text.StartsWith("r\"", StringComparison.Ordinal) && text.EndsWith('"'))
            return new SemanticLiteral(text[2..^1], "string", token.Line);
        if (text.Length >= 2 && (text[0] == '"' || text[0] == '\'') && text[^1] == text[0])
        {
            var content = text[1..^1]
                .Replace("\\n", "\n", StringComparison.Ordinal)
                .Replace("\\r", "\r", StringComparison.Ordinal)
                .Replace("\\t", "\t", StringComparison.Ordinal)
                .Replace("\\\"", "\"", StringComparison.Ordinal)
                .Replace("\\'", "'", StringComparison.Ordinal)
                .Replace("\\\\", "\\", StringComparison.Ordinal);
            return new SemanticLiteral(content, token.Kind == TokenKind.Character ? "char" : "string", token.Line);
        }
        return new SemanticLiteral(text, "string", token.Line);
    }

    private string NormalizeOperator(string op)
    {
        if (op == "and") return "&&";
        if (op == "or") return "||";
        if (op == "===") return "==";
        if (op == "!==") return "!=";
        if (_language == LanguageId.R && op == "^") return "**";
        return op;
    }

    private bool AtEnd => _position >= _tokens.Count || Peek().Kind == TokenKind.End;
    private Token Peek(int offset = 0)
    {
        var index = Math.Min(_position + offset, _tokens.Count - 1);
        return _tokens[index];
    }
    private Token Next() => _tokens[Math.Min(_position++, _tokens.Count - 1)];
    private bool Match(string text)
    {
        if (!AtEnd && Peek().Text == text) { _position++; return true; }
        return false;
    }
    private void Expect(string text)
    {
        if (!Match(text)) throw new FormatException($"Expected '{text}' near token {Peek().Text}.");
    }
}
