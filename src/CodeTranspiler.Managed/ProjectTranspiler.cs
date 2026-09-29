using System.Collections.Concurrent;

namespace CodeTranspiler.Managed;

/// <summary>Transpiles source trees without requiring external compilers or runtimes.</summary>
public sealed class ProjectTranspiler
{
    private readonly CodeTranspiler _transpiler = new();

    public ProjectTranspileResult TranspileDirectory(
        string inputDirectory,
        string outputDirectory,
        string targetLanguage,
        ProjectTranspileOptions? options = null)
    {
        ArgumentException.ThrowIfNullOrWhiteSpace(inputDirectory);
        ArgumentException.ThrowIfNullOrWhiteSpace(outputDirectory);
        options ??= new ProjectTranspileOptions();
        var target = LanguageCatalog.Parse(targetLanguage);
        var root = Path.GetFullPath(inputDirectory);
        var output = Path.GetFullPath(outputDirectory);
        if (!Directory.Exists(root)) throw new DirectoryNotFoundException(root);
        if (string.Equals(root.TrimEnd(Path.DirectorySeparatorChar), output.TrimEnd(Path.DirectorySeparatorChar), StringComparison.OrdinalIgnoreCase))
            throw new ArgumentException("Output directory must differ from the input directory.", nameof(outputDirectory));
        Directory.CreateDirectory(output);

        var files = Directory.EnumerateFiles(root, "*", options.Recursive ? SearchOption.AllDirectories : SearchOption.TopDirectoryOnly)
            .Where(path => LanguageCatalog.TryFromPath(path, out _))
            .Where(path => !Path.GetFullPath(path).StartsWith(output + Path.DirectorySeparatorChar, StringComparison.OrdinalIgnoreCase))
            .ToArray();

        var results = new ConcurrentBag<ProjectFileResult>();
        var po = new ParallelOptions { MaxDegreeOfParallelism = Math.Max(1, options.MaxDegreeOfParallelism) };
        Parallel.ForEach(files, po, file =>
        {
            try
            {
                var sourceLanguage = LanguageCatalog.FromPath(file);
                var relative = Path.GetRelativePath(root, file);
                var outRelative = Path.ChangeExtension(relative, LanguageCatalog.Get(target).Extensions.First());
                var outPath = Path.Combine(output, outRelative);
                Directory.CreateDirectory(Path.GetDirectoryName(outPath)!);
                var result = _transpiler.Transpile(File.ReadAllText(file), sourceLanguage, target, options.TranspileOptions);
                File.WriteAllText(outPath, result.Code);
                results.Add(new ProjectFileResult(file, outPath, sourceLanguage, target, result.Success, result.Diagnostics, null));
            }
            catch (Exception ex)
            {
                results.Add(new ProjectFileResult(file, null, null, target, false, Array.Empty<TranspileDiagnostic>(), ex.Message));
            }
        });

        var ordered = results.OrderBy(x => x.InputPath, StringComparer.OrdinalIgnoreCase).ToArray();
        return new ProjectTranspileResult(root, output, target, ordered);
    }
}

public sealed class ProjectTranspileOptions
{
    public bool Recursive { get; init; } = true;
    public int MaxDegreeOfParallelism { get; init; } = Environment.ProcessorCount;
    public TranspileOptions TranspileOptions { get; init; } = new();
}

public sealed record ProjectFileResult(
    string InputPath,
    string? OutputPath,
    LanguageId? SourceLanguage,
    LanguageId TargetLanguage,
    bool Success,
    IReadOnlyList<TranspileDiagnostic> Diagnostics,
    string? Error);

public sealed record ProjectTranspileResult(
    string InputDirectory,
    string OutputDirectory,
    LanguageId TargetLanguage,
    IReadOnlyList<ProjectFileResult> Files)
{
    public int SuccessCount => Files.Count(x => x.Success);
    public int FailureCount => Files.Count - SuccessCount;
    public bool Success => FailureCount == 0;
}
