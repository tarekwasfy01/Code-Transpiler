using System.Diagnostics;
using System.Net;
using System.Text;
using System.Text.Json;

namespace CodeTranspiler.Managed;

/// <summary>Dependency-free local browser UI backed only by the .NET base class library.</summary>
public sealed class TranspilerWebGui : IDisposable
{
    private readonly HttpListener _listener = new();
    private readonly CancellationTokenSource _stop = new();
    private Task? _loop;
    public Uri Address { get; }

    public TranspilerWebGui(int port = 0)
    {
        if (port <= 0) port = FindFreePort();
        Address = new Uri($"http://127.0.0.1:{port}/");
        _listener.Prefixes.Add(Address.ToString());
    }

    public void Start(bool openBrowser = true)
    {
        if (_listener.IsListening) return;
        _listener.Start();
        _loop = Task.Run(() => LoopAsync(_stop.Token));
        if (openBrowser) OpenBrowser(Address.ToString());
    }

    public async Task RunUntilCancelledAsync(CancellationToken cancellationToken = default)
    {
        Start();
        using var linked = CancellationTokenSource.CreateLinkedTokenSource(cancellationToken, _stop.Token);
        try { await Task.Delay(Timeout.Infinite, linked.Token).ConfigureAwait(false); }
        catch (OperationCanceledException) { }
    }

    private async Task LoopAsync(CancellationToken token)
    {
        while (!token.IsCancellationRequested && _listener.IsListening)
        {
            HttpListenerContext context;
            try { context = await _listener.GetContextAsync().ConfigureAwait(false); }
            catch when (token.IsCancellationRequested || !_listener.IsListening) { break; }
            _ = Task.Run(() => HandleAsync(context), token);
        }
    }

    private static async Task HandleAsync(HttpListenerContext ctx)
    {
        try
        {
            var path = ctx.Request.Url?.AbsolutePath ?? "/";
            if (ctx.Request.HttpMethod == "GET" && path == "/")
            {
                await WriteAsync(ctx, 200, "text/html; charset=utf-8", Html).ConfigureAwait(false);
                return;
            }
            if (ctx.Request.HttpMethod == "GET" && path == "/api/health")
            {
                await WriteAsync(ctx, 200, "application/json", "{\"ok\":true}").ConfigureAwait(false);
                return;
            }
            if (ctx.Request.HttpMethod == "GET" && path == "/api/languages")
            {
                var json = JsonSerializer.Serialize(LanguageCatalog.All.Select(x => new { id = LanguageCatalog.CanonicalName(x.Id), name = x.Name, extensions = x.Extensions }));
                await WriteAsync(ctx, 200, "application/json", json).ConfigureAwait(false);
                return;
            }
            if (ctx.Request.HttpMethod == "POST" && path == "/api/transpile")
            {
                if (ctx.Request.ContentLength64 > 4 * 1024 * 1024)
                {
                    await WriteAsync(ctx, 413, "application/json", "{\"error\":\"Request too large\"}").ConfigureAwait(false);
                    return;
                }
                using var reader = new StreamReader(ctx.Request.InputStream, ctx.Request.ContentEncoding ?? Encoding.UTF8);
                var body = await reader.ReadToEndAsync().ConfigureAwait(false);
                var req = JsonSerializer.Deserialize<GuiRequest>(body, JsonOptions) ?? throw new InvalidDataException("Invalid request.");
                var result = new CodeTranspiler().Transpile(req.Code ?? string.Empty, req.From ?? "python", req.To ?? "csharp", new TranspileOptions
                {
                    PreserveComments = true,
                    PreserveUnknownFragmentsAsComments = true,
                    EnableFragmentFallback = true,
                    EnableRouteRacing = true,
                    EmitHeader = req.EmitHeader,
                    PreferExplicitTypes = true
                });
                var json = JsonSerializer.Serialize(new
                {
                    code = result.Code,
                    success = result.Success,
                    diagnostics = result.Diagnostics,
                    attempts = result.Attempts,
                    semantic = SemanticDocument.Serialize(result.Program, true)
                }, JsonOptions);
                await WriteAsync(ctx, 200, "application/json", json).ConfigureAwait(false);
                return;
            }
            await WriteAsync(ctx, 404, "text/plain", "Not found").ConfigureAwait(false);
        }
        catch (Exception ex)
        {
            await WriteAsync(ctx, 500, "application/json", JsonSerializer.Serialize(new { error = ex.Message })).ConfigureAwait(false);
        }
    }

    private static async Task WriteAsync(HttpListenerContext ctx, int status, string contentType, string body)
    {
        var bytes = Encoding.UTF8.GetBytes(body);
        ctx.Response.StatusCode = status;
        ctx.Response.ContentType = contentType;
        ctx.Response.ContentLength64 = bytes.Length;
        ctx.Response.Headers["Cache-Control"] = "no-store";
        ctx.Response.Headers["X-Content-Type-Options"] = "nosniff";
        ctx.Response.Headers["X-Frame-Options"] = "DENY";
        ctx.Response.Headers["Referrer-Policy"] = "no-referrer";
        await ctx.Response.OutputStream.WriteAsync(bytes).ConfigureAwait(false);
        ctx.Response.Close();
    }

    private static int FindFreePort()
    {
        var listener = new System.Net.Sockets.TcpListener(IPAddress.Loopback, 0);
        listener.Start();
        var port = ((IPEndPoint)listener.LocalEndpoint).Port;
        listener.Stop();
        return port;
    }

    private static void OpenBrowser(string url)
    {
        try { Process.Start(new ProcessStartInfo(url) { UseShellExecute = true }); }
        catch { }
    }

    public void Dispose()
    {
        _stop.Cancel();
        try { _listener.Stop(); } catch { }
        try { _listener.Close(); } catch { }
        try { _loop?.Wait(TimeSpan.FromSeconds(1)); } catch { }
        _stop.Dispose();
    }

    private sealed class GuiRequest
    {
        public string? Code { get; set; }
        public string? From { get; set; }
        public string? To { get; set; }
        public bool EmitHeader { get; set; } = true;
    }

    private static readonly JsonSerializerOptions JsonOptions = new(JsonSerializerDefaults.Web) { WriteIndented = true };

    private const string Html = """
<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>CodeTranspiler.Managed</title><style>
:root{font-family:ui-sans-serif,system-ui,sans-serif;color-scheme:dark;background:#0f1115;color:#e8e8ea}*{box-sizing:border-box}body{margin:0;overflow:hidden}.bar{display:flex;gap:.65rem;align-items:center;padding:.7rem .9rem;background:#171a21;border-bottom:1px solid #30343d}.brand{font-weight:750;margin-right:auto}.workspace{display:grid;grid-template-columns:1fr 1fr;height:calc(100vh - 54px)}.pane{display:flex;flex-direction:column;min-width:0;min-height:0}.pane:first-child{border-right:1px solid #30343d}.editor{flex:1;min-height:0;display:flex}.editor textarea,.editor pre{width:100%;height:100%;margin:0;padding:1rem;background:#0f1115;color:#e8e8ea;border:0;outline:0;resize:none;font:13px/1.5 ui-monospace,SFMono-Regular,Consolas,monospace;white-space:pre;overflow:auto}.tools{display:flex;align-items:center;gap:.5rem;padding:.4rem .7rem;border-top:1px solid #30343d;background:#151820;min-height:39px}.details{height:32vh;border-top:1px solid #30343d;background:#0d0f13;display:none;grid-column:1/3}.details.open{display:grid;grid-template-columns:1fr 1fr}.details pre{margin:0;padding:.8rem;overflow:auto;font:12px/1.45 ui-monospace,SFMono-Regular,Consolas,monospace}.details pre:first-child{border-right:1px solid #30343d}select,button{background:#272b34;color:#eee;border:1px solid #404652;border-radius:6px;padding:.4rem .6rem}button{cursor:pointer}.status{font-size:.82rem;color:#aeb4c0}.ok{color:#78dba9}.bad{color:#ff8a8a}@media(max-width:850px){body{overflow:auto}.workspace{grid-template-columns:1fr;height:auto}.pane{height:48vh}.pane:first-child{border-right:0;border-bottom:1px solid #30343d}.details{grid-column:1;min-height:35vh;height:auto}.details.open{grid-template-columns:1fr}.details pre:first-child{border-right:0;border-bottom:1px solid #30343d}}
</style></head><body><div class="bar"><div class="brand">CodeTranspiler.Managed</div><select id="from"></select><span>→</span><select id="to"></select><button id="swap" title="Swap">↔</button><button id="run">Transpile</button><button id="inspect">Inspect</button></div><main class="workspace"><section class="pane"><div class="editor"><textarea id="src" spellcheck="false">def add(a: int, b: int) -> int:
    return a + b</textarea></div><div class="tools"><span class="status">Source · Ctrl/⌘+Enter to transpile</span></div></section><section class="pane"><div class="editor"><pre id="out"></pre></div><div class="tools"><button id="copy">Copy</button><span class="status" id="status">Ready</span></div></section><section id="details" class="details"><pre id="diag"></pre><pre id="semantic"></pre></section></main><script>
const $=id=>document.getElementById(id);let langs=[];async function init(){langs=await fetch('/api/languages').then(r=>r.json());for(const x of langs){for(const id of ['from','to']){const o=document.createElement('option');o.value=x.id;o.textContent=x.name;$(id).appendChild(o)}}$('from').value='python';$('to').value='csharp';run()}async function run(){const b=$('run');b.disabled=true;$('status').className='status';$('status').textContent='Transpiling…';try{const r=await fetch('/api/transpile',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({code:$('src').value,from:$('from').value,to:$('to').value,emitHeader:true})});const j=await r.json();if(j.error)throw new Error(j.error);$('out').textContent=j.code;$('semantic').textContent=j.semantic||'';$('diag').textContent='Diagnostics\n'+JSON.stringify(j.diagnostics,null,2)+'\n\nRoute attempts\n'+JSON.stringify(j.attempts,null,2);$('status').textContent=(j.success?'OK':'Completed with errors')+' · '+j.diagnostics.length+' diagnostics · '+j.attempts.length+' routes';$('status').className='status '+(j.success?'ok':'bad')}catch(e){$('status').textContent=e.message;$('status').className='status bad'}finally{b.disabled=false}}$('run').onclick=run;$('swap').onclick=()=>{const a=$('from').value;$('from').value=$('to').value;$('to').value=a;const s=$('src').value;$('src').value=$('out').textContent;$('out').textContent=s;run()};$('inspect').onclick=()=>{$('details').classList.toggle('open')};$('copy').onclick=async()=>{await navigator.clipboard.writeText($('out').textContent);$('status').textContent='Copied'};$('src').addEventListener('keydown',e=>{if((e.ctrlKey||e.metaKey)&&e.key==='Enter')run()});init();
</script></body></html>
""";
}
