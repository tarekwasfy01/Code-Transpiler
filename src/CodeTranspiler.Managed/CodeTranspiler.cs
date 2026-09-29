using System.Text.Json;
using System.Text.Json.Serialization;

namespace CodeTranspiler.Managed;

/// <summary>
/// Self-contained managed code transpiler. No external compiler process or foreign runtime is required.
/// </summary>
public sealed class CodeTranspiler
{
    /// <summary>Transpile source text between any registered language pair.</summary>
    public TranspileResult Transpile(string source, string sourceLanguage, string targetLanguage, TranspileOptions? options = null)
        => Transpile(source, LanguageCatalog.Parse(sourceLanguage), LanguageCatalog.Parse(targetLanguage), options);

    /// <summary>Transpile source text between any registered language pair.</summary>
    public TranspileResult Transpile(string source, LanguageId sourceLanguage, LanguageId targetLanguage, TranspileOptions? options = null)
    {
        ArgumentNullException.ThrowIfNull(source);
        options ??= new TranspileOptions();

        if (sourceLanguage == targetLanguage)
        {
            var diagnostics = new List<TranspileDiagnostic>();
            var program = FrontendParser.Parse(source, sourceLanguage, options, diagnostics);
            SemanticNormalizer.Normalize(program);
            return new TranspileResult
            {
                SourceLanguage = sourceLanguage,
                TargetLanguage = targetLanguage,
                Code = source,
                Program = program,
                Diagnostics = diagnostics,
                Attempts = new[] { new RouteAttempt("identity", true, int.MaxValue, program.OriginalSymbols.Count, program.OriginalSymbols.Count) }
            };
        }

        var attempts = new List<RouteAttempt>();
        var candidate = FragmentOrchestrator.Transpile(source, sourceLanguage, targetLanguage, options, attempts);
        var finalDiagnostics = candidate.Diagnostics.ToList();
        if (candidate.Preserved < candidate.Total)
            finalDiagnostics.Add(new TranspileDiagnostic(DiagnosticSeverity.Warning, "CT3001", $"Preserved {candidate.Preserved} of {candidate.Total} detected top-level symbols.", Route: candidate.Route));
        return new TranspileResult
        {
            SourceLanguage = sourceLanguage,
            TargetLanguage = targetLanguage,
            Code = candidate.Code,
            Program = candidate.Program,
            Diagnostics = finalDiagnostics,
            Attempts = attempts
        };
    }

    /// <summary>Transpile a file, inferring the source language from its extension.</summary>
    public TranspileResult TranspileFile(string inputPath, string outputPath, string targetLanguage, TranspileOptions? options = null)
    {
        var sourceLanguage = LanguageCatalog.FromPath(inputPath);
        var result = Transpile(File.ReadAllText(inputPath), sourceLanguage, LanguageCatalog.Parse(targetLanguage), options);
        var directory = Path.GetDirectoryName(Path.GetFullPath(outputPath));
        if (!string.IsNullOrEmpty(directory)) Directory.CreateDirectory(directory);
        File.WriteAllText(outputPath, result.Code);
        return result;
    }

    /// <summary>Lower source into the managed semantic IR without emitting a target language.</summary>
    public SemanticProgram Analyze(string source, string sourceLanguage, TranspileOptions? options = null)
    {
        options ??= new TranspileOptions();
        var diagnostics = new List<TranspileDiagnostic>();
        var program = FrontendParser.Parse(source, LanguageCatalog.Parse(sourceLanguage), options, diagnostics);
        SemanticNormalizer.Normalize(program);
        return program;
    }

    /// <summary>Serialize semantic IR as JSON for diagnostics/tooling.</summary>
    public string AnalyzeJson(string source, string sourceLanguage, bool indented = true, TranspileOptions? options = null)
    {
        var program = Analyze(source, sourceLanguage, options);
        return SemanticDocument.Serialize(program, indented);
    }

    public static IReadOnlyList<LanguageSpec> Languages => LanguageCatalog.All;
    public static IReadOnlyList<(LanguageId Source, LanguageId Target)> Routes => LanguageCatalog.Routes();
}

/// <summary>Convenience static entry points.</summary>
public static class Transpiler
{
    public static string Convert(string source, string from, string to, TranspileOptions? options = null)
        => new CodeTranspiler().Transpile(source, from, to, options).Code;

    public static TranspileResult ConvertDetailed(string source, string from, string to, TranspileOptions? options = null)
        => new CodeTranspiler().Transpile(source, from, to, options);
}
