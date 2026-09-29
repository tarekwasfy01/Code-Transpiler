using System.Text.Json;

namespace CodeTranspiler.Managed;

/// <summary>Command-line host shipped in the same assembly as the library.</summary>
public static class Program
{
    public static async Task<int> Main(string[] args)
    {
        try
        {
            if (args.Length == 0 || Is(args[0], "gui"))
                return await RunGuiAsync(args.Skip(args.Length == 0 ? 0 : 1).ToArray()).ConfigureAwait(false);
            if (Is(args[0], "help", "--help", "-h")) { PrintHelp(); return 0; }
            if (Is(args[0], "version", "--version")) { Console.WriteLine(typeof(CodeTranspiler).Assembly.GetName().Version?.ToString() ?? "0.0.0"); return 0; }
            if (Is(args[0], "languages", "targets")) { foreach (var l in LanguageCatalog.All) Console.WriteLine($"{LanguageCatalog.CanonicalName(l.Id),-8} {l.Name,-8} {string.Join(',', l.Extensions)}"); return 0; }
            if (Is(args[0], "routes")) { foreach (var r in LanguageCatalog.Routes()) Console.WriteLine($"{LanguageCatalog.CanonicalName(r.Source)} -> {LanguageCatalog.CanonicalName(r.Target)}"); return 0; }
            if (Is(args[0], "analyze", "semantic-export")) return RunAnalyze(args[1..]);
            if (Is(args[0], "semantic-transpile")) return RunSemanticTranspile(args[1..]);
            if (Is(args[0], "transpile-batch")) return RunBatch();
            if (Is(args[0], "capabilities", "capability-matrix")) { Console.WriteLine(JsonSerializer.Serialize(Capabilities.Matrix, new JsonSerializerOptions { WriteIndented = true })); return 0; }
            if (Is(args[0], "project")) return RunProject(args[1..]);
            if (Is(args[0], "transpile")) return RunTranspile(args[1..]);
            Console.Error.WriteLine($"Unknown command '{args[0]}'."); PrintHelp(); return 2;
        }
        catch (Exception ex) { Console.Error.WriteLine(ex.Message); return 1; }
    }

    private static int RunTranspile(string[] args)
    {
        var input = Positional(args, 0) ?? throw new ArgumentException("Input file is required (or use '-' for stdin).");
        var fromText = Opt(args, "--from", "-f");
        var toText = Opt(args, "--to", "-t", "--target") ?? throw new ArgumentException("--to is required.");
        var output = Opt(args, "--out", "-o");
        if (input == "-" && fromText is null) throw new ArgumentException("--from is required when reading stdin.");
        var from = fromText is null ? LanguageCatalog.FromPath(input) : LanguageCatalog.Parse(fromText);
        var to = LanguageCatalog.Parse(toText);
        var source = input == "-" ? Console.In.ReadToEnd() : File.ReadAllText(input);
        var result = new CodeTranspiler().Transpile(source, from, to);
        if (output is null || output == "-") Console.Write(result.Code); else { Directory.CreateDirectory(Path.GetDirectoryName(Path.GetFullPath(output))!); File.WriteAllText(output, result.Code); }
        foreach (var d in result.Diagnostics) Console.Error.WriteLine($"{d.Severity} {d.Code}: {d.Message}");
        return result.Success ? 0 : 1;
    }

    private static int RunAnalyze(string[] args)
    {
        var input = Positional(args, 0) ?? throw new ArgumentException("Input file is required (or use '-' for stdin).");
        var fromText = Opt(args, "--from", "-f");
        if (input == "-" && fromText is null) throw new ArgumentException("--from is required when reading stdin.");
        var from = fromText is null ? LanguageCatalog.FromPath(input) : LanguageCatalog.Parse(fromText);
        var source = input == "-" ? Console.In.ReadToEnd() : File.ReadAllText(input);
        Console.WriteLine(new CodeTranspiler().AnalyzeJson(source, LanguageCatalog.CanonicalName(from)));
        return 0;
    }

    private static int RunSemanticTranspile(string[] args)
    {
        var input = Positional(args, 0) ?? throw new ArgumentException("Semantic JSON file is required.");
        var target = Opt(args, "--to", "-t", "--target") ?? throw new ArgumentException("--to is required.");
        var output = Opt(args, "--out", "-o");
        var program = SemanticDocument.Deserialize(File.ReadAllText(input));
        var code = SemanticDocument.Emit(program, target);
        if (output is null) Console.Write(code); else { Directory.CreateDirectory(Path.GetDirectoryName(Path.GetFullPath(output))!); File.WriteAllText(output, code); }
        return 0;
    }

    private static int RunBatch()
    {
        var json = Console.In.ReadToEnd();
        var items = JsonSerializer.Deserialize<BatchTranspileItem[]>(json, new JsonSerializerOptions(JsonSerializerDefaults.Web)) ?? Array.Empty<BatchTranspileItem>();
        var result = BatchTranspiler.Transpile(items);
        Console.WriteLine(JsonSerializer.Serialize(result, new JsonSerializerOptions(JsonSerializerDefaults.Web) { WriteIndented = true }));
        return result.All(x => x.Error is null) ? 0 : 1;
    }

    private static int RunProject(string[] args)
    {
        var input = Positional(args, 0) ?? throw new ArgumentException("Input directory is required.");
        var output = Opt(args, "--out", "-o") ?? throw new ArgumentException("--out is required.");
        var target = Opt(args, "--to", "-t", "--target") ?? throw new ArgumentException("--to is required.");
        var result = new ProjectTranspiler().TranspileDirectory(input, output, target);
        Console.WriteLine(JsonSerializer.Serialize(new { result.Success, result.SuccessCount, result.FailureCount, result.OutputDirectory }, new JsonSerializerOptions { WriteIndented = true }));
        return result.Success ? 0 : 1;
    }

    private static async Task<int> RunGuiAsync(string[] args)
    {
        var portText = Opt(args, "--port", "-p");
        var port = int.TryParse(portText, out var p) ? p : 0;
        using var gui = new TranspilerWebGui(port);
        gui.Start(openBrowser: !args.Contains("--no-browser", StringComparer.OrdinalIgnoreCase));
        Console.WriteLine($"CodeTranspiler GUI: {gui.Address}");
        Console.WriteLine("Press Ctrl+C to stop.");
        var done = new TaskCompletionSource<bool>(TaskCreationOptions.RunContinuationsAsynchronously);
        Console.CancelKeyPress += (_, e) => { e.Cancel = true; done.TrySetResult(true); };
        await done.Task.ConfigureAwait(false);
        return 0;
    }

    private static string? Positional(string[] args, int index)
    {
        var positionals = new List<string>();
        for (var i = 0; i < args.Length; i++)
        {
            if (Is(args[i], "--from", "-f", "--to", "-t", "--target", "--out", "-o", "--port", "-p")) { i++; continue; }
            if (args[i] == "-" || !args[i].StartsWith('-')) positionals.Add(args[i]);
        }
        return positionals.Skip(index).FirstOrDefault();
    }
    private static string? Opt(string[] args, params string[] names)
    {
        for (var i = 0; i < args.Length; i++)
        {
            if (!names.Any(n => string.Equals(n, args[i], StringComparison.OrdinalIgnoreCase))) continue;
            if (i + 1 >= args.Length || args[i + 1].StartsWith("--", StringComparison.Ordinal)) throw new ArgumentException($"Missing value for {args[i]}.");
            return args[i + 1];
        }
        return null;
    }
    private static bool Is(string value, params string[] values) => values.Any(x => string.Equals(value, x, StringComparison.OrdinalIgnoreCase));
    private static void PrintHelp() => Console.WriteLine("""
CodeTranspiler.Managed
  gui [--port N] [--no-browser]
  transpile <file|-> --to <language> [--from <language>] [-o output|-]
  project <directory> --to <language> -o <directory>
  analyze|semantic-export <file|-> [--from <language>]
  semantic-transpile <semantic.json> --to <language> [-o output]
  transpile-batch              # JSON array on stdin
  capabilities
  languages | routes | version | help

The package is self-contained managed .NET and has no external runtime/compiler dependency.
""");
}
