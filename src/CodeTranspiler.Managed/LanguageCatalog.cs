using System.Collections.ObjectModel;

namespace CodeTranspiler.Managed;

/// <summary>Stable identifiers for source and target languages.</summary>
public enum LanguageId
{
    R,
    Go,
    Python,
    Rust,
    C,
    Cpp,
    Zig,
    Julia,
    Nim,
    CSharp,
    Java,
    Kotlin,
    Swift
}

/// <summary>Metadata for a supported language.</summary>
public sealed record LanguageSpec(
    LanguageId Id,
    string Name,
    IReadOnlyList<string> Aliases,
    IReadOnlyList<string> Extensions,
    int IndexBase,
    bool SignificantIndentation = false);

/// <summary>Language normalization, aliases and file-extension inference.</summary>
public static class LanguageCatalog
{
    private static readonly ReadOnlyCollection<LanguageSpec> Specs = new(new[]
    {
        new LanguageSpec(LanguageId.R, "R", new[] { "r" }, new[] { ".r", ".R" }, 1),
        new LanguageSpec(LanguageId.Go, "Go", new[] { "go", "golang" }, new[] { ".go" }, 0),
        new LanguageSpec(LanguageId.Python, "Python", new[] { "python", "py" }, new[] { ".py" }, 0, true),
        new LanguageSpec(LanguageId.Rust, "Rust", new[] { "rust", "rs" }, new[] { ".rs" }, 0),
        new LanguageSpec(LanguageId.C, "C", new[] { "c" }, new[] { ".c", ".h" }, 0),
        new LanguageSpec(LanguageId.Cpp, "C++", new[] { "cpp", "c++", "cxx" }, new[] { ".cpp", ".cc", ".cxx", ".hpp", ".hh" }, 0),
        new LanguageSpec(LanguageId.Zig, "Zig", new[] { "zig" }, new[] { ".zig" }, 0),
        new LanguageSpec(LanguageId.Julia, "Julia", new[] { "julia", "jl" }, new[] { ".jl" }, 1),
        new LanguageSpec(LanguageId.Nim, "Nim", new[] { "nim" }, new[] { ".nim" }, 0, true),
        new LanguageSpec(LanguageId.CSharp, "C#", new[] { "csharp", "c#", "cs" }, new[] { ".cs" }, 0),
        new LanguageSpec(LanguageId.Java, "Java", new[] { "java" }, new[] { ".java" }, 0),
        new LanguageSpec(LanguageId.Kotlin, "Kotlin", new[] { "kotlin", "kt" }, new[] { ".kt", ".kts" }, 0),
        new LanguageSpec(LanguageId.Swift, "Swift", new[] { "swift" }, new[] { ".swift" }, 0),
    });

    private static readonly Dictionary<string, LanguageSpec> Aliases = BuildAliases();

    public static IReadOnlyList<LanguageSpec> All => Specs;

    public static LanguageSpec Get(LanguageId id) => Specs.First(x => x.Id == id);

    public static LanguageId Parse(string name)
    {
        if (string.IsNullOrWhiteSpace(name))
            throw new ArgumentException("Language name is required.", nameof(name));

        var key = name.Trim().ToLowerInvariant();
        if (Aliases.TryGetValue(key, out var spec))
            return spec.Id;
        throw new NotSupportedException($"Unsupported language '{name}'.");
    }

    public static bool TryParse(string? name, out LanguageId language)
    {
        language = default;
        if (string.IsNullOrWhiteSpace(name)) return false;
        if (!Aliases.TryGetValue(name.Trim().ToLowerInvariant(), out var spec)) return false;
        language = spec.Id;
        return true;
    }

    public static bool TryFromPath(string? path, out LanguageId language)
    {
        language = default;
        if (string.IsNullOrWhiteSpace(path)) return false;
        var extension = Path.GetExtension(path);
        foreach (var spec in Specs)
        {
            if (spec.Extensions.Any(x => string.Equals(x, extension, StringComparison.OrdinalIgnoreCase)))
            {
                language = spec.Id;
                return true;
            }
        }
        return false;
    }

    public static LanguageId FromPath(string path)
    {
        if (string.IsNullOrWhiteSpace(path))
            throw new ArgumentException("Path is required.", nameof(path));
        var extension = Path.GetExtension(path);
        foreach (var spec in Specs)
        {
            if (spec.Extensions.Any(x => string.Equals(x, extension, StringComparison.OrdinalIgnoreCase)))
                return spec.Id;
        }
        throw new NotSupportedException($"Cannot infer a supported language from '{path}'.");
    }

    public static string CanonicalName(LanguageId id) => id switch
    {
        LanguageId.R => "r",
        LanguageId.Go => "go",
        LanguageId.Python => "python",
        LanguageId.Rust => "rust",
        LanguageId.C => "c",
        LanguageId.Cpp => "cpp",
        LanguageId.Zig => "zig",
        LanguageId.Julia => "julia",
        LanguageId.Nim => "nim",
        LanguageId.CSharp => "csharp",
        LanguageId.Java => "java",
        LanguageId.Kotlin => "kotlin",
        LanguageId.Swift => "swift",
        _ => throw new ArgumentOutOfRangeException(nameof(id))
    };

    public static IReadOnlyList<(LanguageId Source, LanguageId Target)> Routes()
    {
        var routes = new List<(LanguageId, LanguageId)>();
        foreach (var source in Specs)
            foreach (var target in Specs)
                if (source.Id != target.Id)
                    routes.Add((source.Id, target.Id));
        return routes;
    }

    private static Dictionary<string, LanguageSpec> BuildAliases()
    {
        var map = new Dictionary<string, LanguageSpec>(StringComparer.OrdinalIgnoreCase);
        foreach (var spec in Specs)
        {
            map[CanonicalName(spec.Id)] = spec;
            map[spec.Name.ToLowerInvariant()] = spec;
            foreach (var alias in spec.Aliases)
                map[alias.ToLowerInvariant()] = spec;
        }
        return map;
    }
}
