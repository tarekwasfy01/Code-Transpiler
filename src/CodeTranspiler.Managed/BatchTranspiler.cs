namespace CodeTranspiler.Managed;

public sealed record BatchTranspileItem(string Id, string Source, string Target, string Code);
public sealed record BatchTranspileOutput(string Id, string? Code, string? Error, IReadOnlyList<TranspileDiagnostic>? Diagnostics = null);

/// <summary>In-process batch translation API; safe to use from build tools and servers.</summary>
public static class BatchTranspiler
{
    public static IReadOnlyList<BatchTranspileOutput> Transpile(IEnumerable<BatchTranspileItem> items, int maxDegreeOfParallelism = 0)
    {
        ArgumentNullException.ThrowIfNull(items);
        var input = items.ToArray();
        var output = new BatchTranspileOutput[input.Length];
        Parallel.For(0, input.Length, new ParallelOptions { MaxDegreeOfParallelism = maxDegreeOfParallelism <= 0 ? Environment.ProcessorCount : maxDegreeOfParallelism }, i =>
        {
            var item = input[i];
            try
            {
                var result = new CodeTranspiler().Transpile(item.Code, item.Source, item.Target);
                output[i] = new BatchTranspileOutput(item.Id, result.Code, result.Success ? null : string.Join("; ", result.Diagnostics.Where(x => x.Severity == DiagnosticSeverity.Error).Select(x => x.Message)), result.Diagnostics);
            }
            catch (Exception ex) { output[i] = new BatchTranspileOutput(item.Id, null, ex.Message); }
        });
        return output;
    }
}
