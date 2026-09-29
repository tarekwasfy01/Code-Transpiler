namespace CodeTranspiler.Managed;

public sealed record LanguageCapability(
    LanguageId Language,
    bool Frontend,
    bool Backend,
    bool Functions,
    bool Variables,
    bool Expressions,
    bool Conditionals,
    bool Loops,
    bool Collections,
    bool Types,
    bool FragmentFallback);

/// <summary>Machine-readable capability surface for callers and the GUI.</summary>
public static class Capabilities
{
    public static IReadOnlyList<LanguageCapability> Matrix { get; } = LanguageCatalog.All
        .Select(x => new LanguageCapability(x.Id, true, true, true, true, true, true, true, true, true, true))
        .ToArray();

    public static LanguageCapability For(string language) => Matrix.First(x => x.Language == LanguageCatalog.Parse(language));
}
