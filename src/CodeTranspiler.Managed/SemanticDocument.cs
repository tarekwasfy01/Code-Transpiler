using System.Text.Json;
using System.Text.Json.Serialization;

namespace CodeTranspiler.Managed;

/// <summary>Serialization and composition helpers for the portable semantic representation.</summary>
public static class SemanticDocument
{
    private static readonly JsonSerializerOptions Options = CreateOptions();

    public static string Serialize(SemanticProgram program, bool indented = true)
    {
        ArgumentNullException.ThrowIfNull(program);
        var options = CreateOptions();
        options.WriteIndented = indented;
        return JsonSerializer.Serialize(program, options);
    }

    public static SemanticProgram Deserialize(string json)
    {
        ArgumentException.ThrowIfNullOrWhiteSpace(json);
        return JsonSerializer.Deserialize<SemanticProgram>(json, Options)
               ?? throw new InvalidDataException("Semantic document is empty.");
    }

    public static SemanticProgram Merge(IEnumerable<SemanticProgram> programs)
    {
        ArgumentNullException.ThrowIfNull(programs);
        var list = programs.ToList();
        if (list.Count == 0) throw new ArgumentException("At least one program is required.", nameof(programs));
        var merged = new SemanticProgram
        {
            SourceLanguage = list[0].SourceLanguage,
            SourceIndexBase = 0,
            CanonicalIndexBase = 0,
            Evaluation = list.Select(x => x.Evaluation).Distinct(StringComparer.Ordinal).Count() == 1 ? list[0].Evaluation : "mixed",
            ValueModel = list.Select(x => x.ValueModel).Distinct(StringComparer.Ordinal).Count() == 1 ? list[0].ValueModel : "mixed"
        };
        merged.Metadata["merged_units"] = list.Count.ToString(System.Globalization.CultureInfo.InvariantCulture);
        foreach (var program in list)
        {
            foreach (var import in program.Imports)
                if (!merged.Imports.Contains(import)) merged.Imports.Add(import);
            foreach (var declaration in program.Declarations)
                if (!merged.Declarations.Any(x => x.Name == declaration.Name && x.GetType() == declaration.GetType())) merged.Declarations.Add(declaration);
            merged.TopLevelStatements.AddRange(program.TopLevelStatements);
            foreach (var symbol in program.OriginalSymbols)
                if (!merged.OriginalSymbols.Contains(symbol, StringComparer.Ordinal)) merged.OriginalSymbols.Add(symbol);
            foreach (var item in program.Metadata)
                if (!merged.Metadata.ContainsKey(item.Key)) merged.Metadata[item.Key] = item.Value;
        }
        return merged;
    }

    public static string Emit(SemanticProgram program, string targetLanguage, TranspileOptions? options = null)
    {
        options ??= new TranspileOptions();
        var diagnostics = new List<TranspileDiagnostic>();
        SemanticNormalizer.Normalize(program);
        return CodeEmitter.Emit(program, LanguageCatalog.Parse(targetLanguage), options, diagnostics);
    }

    private static JsonSerializerOptions CreateOptions()
    {
        var options = new JsonSerializerOptions
        {
            PropertyNameCaseInsensitive = true,
            DefaultIgnoreCondition = JsonIgnoreCondition.WhenWritingNull,
            WriteIndented = true
        };
        options.Converters.Add(new JsonStringEnumConverter());
        return options;
    }
}
