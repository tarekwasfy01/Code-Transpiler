const _GUI_SERVER = Ref{Union{Nothing,Sockets.TCPServer}}(nothing)

_html_escape(s::AbstractString) = replace(String(s), '&' => "&amp;", '<' => "&lt;", '>' => "&gt;", '"' => "&quot;")

function _url_decode(s::AbstractString)
    text = replace(String(s), '+' => ' ')
    io = IOBuffer()
    i = firstindex(text)
    while i <= lastindex(text)
        if text[i] == '%' && i + 2 <= lastindex(text)
            h = text[nextind(text, i):nextind(text, nextind(text, i))]
            try
                write(io, UInt8(parse(Int, h; base=16)))
                i = nextind(text, nextind(text, nextind(text, i)))
                continue
            catch
            end
        end
        print(io, text[i])
        i = nextind(text, i)
    end
    return String(take!(io))
end

function _parse_form(body::AbstractString)
    data = Dict{String,String}()
    for item in split(String(body), '&')
        isempty(item) && continue
        pair = split(item, '='; limit=2)
        key = _url_decode(pair[1])
        val = length(pair) == 2 ? _url_decode(pair[2]) : ""
        data[key] = val
    end
    data
end

function _language_options(selected::String)
    join(("<option value=\"$(_html_escape(l.id))\"" * (l.id == selected ? " selected" : "") * ">$(_html_escape(l.name))</option>" for l in _LANGUAGES), "\n")
end

function _gui_page(; source="go", target="julia", code="package main\n\nimport \"fmt\"\n\nfunc main() {\n    fmt.Println(\"Hello from Go\")\n}\n", output="", semantics="", error="")
    badge = "Native Julia · 14 Sprachen inkl. SE · ohne Binary"
    errbox = isempty(error) ? "" : "<div class=\"error\">$(_html_escape(error))</div>"
    return """<!doctype html>
<html lang=\"de\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">
<title>CodeTranspiler.jl</title>
<style>
:root{font-family:Inter,system-ui,sans-serif;color-scheme:dark;background:#11151b;color:#edf2f7}body{margin:0}.bar{padding:18px 24px;background:#171d25;display:flex;justify-content:space-between;align-items:center;border-bottom:1px solid #29313d}.brand{font-size:20px;font-weight:700}.badge{font-size:12px;padding:7px 10px;border-radius:999px;background:#263141}.wrap{padding:22px;max-width:1400px;margin:auto}.controls{display:flex;gap:12px;align-items:end;margin-bottom:14px;flex-wrap:wrap}label{font-size:12px;color:#aeb8c5;display:flex;flex-direction:column;gap:5px}select,button{font:inherit;border-radius:8px;border:1px solid #364252;background:#1b222c;color:#f8fafc;padding:9px 12px}button{cursor:pointer;background:#356ae6;border-color:#356ae6;font-weight:650}.grid{display:grid;grid-template-columns:1fr 1fr 1fr;gap:14px}.pane-title{font-size:12px;color:#aeb8c5;margin:0 0 6px 2px}textarea{width:100%;box-sizing:border-box;min-height:68vh;resize:vertical;border:1px solid #303b49;border-radius:10px;background:#0d1117;color:#e6edf3;padding:14px;font:14px/1.5 ui-monospace,SFMono-Regular,Consolas,monospace}.error{background:#472025;border:1px solid #7e3039;padding:10px;border-radius:8px;margin-bottom:12px}@media(max-width:1100px){.grid{grid-template-columns:1fr}textarea{min-height:34vh}}
</style></head><body>
<div class=\"bar\"><div class=\"brand\">CodeTranspiler.jl</div><div class=\"badge\">$badge</div></div>
<div class=\"wrap\">$errbox<form method=\"POST\" action=\"/transpile\">
<div class=\"controls\"><label>Quelle<select name=\"source\">$(_language_options(source))</select></label><label>Ziel<select name=\"target\">$(_language_options(target))</select></label><button type=\"submit\">Transpilieren</button></div>
<div class=\"grid\"><div><div class=\"pane-title\">Quellcode</div><textarea name=\"code\" spellcheck=\"false\">$(_html_escape(code))</textarea></div><div><div class=\"pane-title\">Zielcode</div><textarea readonly spellcheck=\"false\">$(_html_escape(output))</textarea></div><div><div class=\"pane-title\">Semantik</div><textarea readonly spellcheck=\"false\">$(_html_escape(semantics))</textarea></div></div>
</form></div></body></html>"""
end

function _read_http_request(sock::Sockets.TCPSocket)
    firstline = readline(sock)
    isempty(firstline) && return ("", "", Dict{String,String}(), "")
    parts = split(chomp(firstline))
    length(parts) >= 2 || return ("", "", Dict{String,String}(), "")
    method, path = parts[1], parts[2]
    headers = Dict{String,String}()
    while !eof(sock)
        line = chomp(readline(sock))
        isempty(strip(line)) && break
        p = split(line, ':'; limit=2)
        length(p) == 2 && (headers[lowercase(strip(p[1]))] = strip(p[2]))
    end
    n = try parse(Int, get(headers, "content-length", "0")) catch; 0 end
    body = n > 0 ? String(read(sock, n)) : ""
    return (method, path, headers, body)
end

function _send_http(sock::Sockets.TCPSocket, status::String, body::String; content_type="text/html; charset=utf-8")
    bytes = ncodeunits(body)
    print(sock, "HTTP/1.1 $status\r\nContent-Type: $content_type\r\nContent-Length: $bytes\r\nConnection: close\r\n\r\n")
    print(sock, body)
    flush(sock)
end

function _handle_client(sock::Sockets.TCPSocket)
    try
        method, path, _, body = _read_http_request(sock)
        if method == "GET" && path == "/"
            _send_http(sock, "200 OK", _gui_page())
        elseif method == "POST" && path == "/transpile"
            form = _parse_form(body)
            source = get(form, "source", "go")
            target = get(form, "target", "julia")
            code = get(form, "code", "")
            generated, semantic_text, errmsg = try
                result = transpile_with_semantics(source, target, code)
                (result.code, result.semantics, "")
            catch err
                sem = try semantic_output(source, code; target=target) catch; "" end
                ("", sem, sprint(showerror, err))
            end
            _send_http(sock, "200 OK", _gui_page(source=source, target=target, code=code, output=generated, semantics=semantic_text, error=errmsg))
        elseif method == "GET" && path == "/health"
            _send_http(sock, "200 OK", "ok"; content_type="text/plain; charset=utf-8")
        else
            _send_http(sock, "404 Not Found", "not found"; content_type="text/plain; charset=utf-8")
        end
    catch
    finally
        close(sock)
    end
end

"""
    serve_gui(; host=ip"127.0.0.1", port=8765)

Start the local CodeTranspiler web GUI. This function blocks until
`stop_gui()` is called from another task/session.
"""
function serve_gui(; host=ip"127.0.0.1", port::Integer=8765)
    _GUI_SERVER[] === nothing || error("GUI server is already running")
    server = listen(host, port)
    _GUI_SERVER[] = server
    @info "CodeTranspiler GUI" url="http://$(host):$(port)/"
    try
        while isopen(server)
            sock = accept(server)
            @async _handle_client(sock)
        end
    catch err
        if isopen(server)
            rethrow(err)
        end
    finally
        _GUI_SERVER[] = nothing
        isopen(server) && close(server)
    end
    nothing
end

"""Stop a GUI server previously started with `serve_gui` or `gui`."""
function stop_gui()
    server = _GUI_SERVER[]
    server === nothing && return false
    isopen(server) && close(server)
    _GUI_SERVER[] = nothing
    return true
end

function _open_browser(url::String)
    cmd = if Sys.iswindows()
        `cmd /c start "" $url`
    elseif Sys.isapple()
        `open $url`
    else
        `xdg-open $url`
    end
    try
        run(cmd; wait=false)
        true
    catch
        false
    end
end

"""
    gui(; port=8765, open_browser=true)

Start the GUI asynchronously and return its URL.
"""
function gui(; port::Integer=8765, open_browser::Bool=true)
    url = "http://127.0.0.1:$(port)/"
    @async serve_gui(port=port)
    sleep(0.15)
    open_browser && _open_browser(url)
    return url
end
