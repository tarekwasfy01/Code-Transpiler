use crate::{
    backend_status, languages, open_browser, source_languages, targets_for_source, transpile,
};
use std::io::{self, BufRead, BufReader, Read, Write};
use std::net::{SocketAddr, TcpListener, TcpStream};
use std::sync::{
    atomic::{AtomicBool, Ordering},
    Arc,
};
use std::thread;

pub struct GuiHandle {
    pub addr: SocketAddr,
    stop: Arc<AtomicBool>,
    join: Option<thread::JoinHandle<io::Result<()>>>,
}
impl GuiHandle {
    pub fn url(&self) -> String {
        format!("http://{}/", self.addr)
    }
    pub fn stop(mut self) -> io::Result<()> {
        self.stop.store(true, Ordering::SeqCst);
        let _ = TcpStream::connect(self.addr);
        if let Some(j) = self.join.take() {
            j.join()
                .unwrap_or_else(|_| Err(io::Error::other("GUI thread panicked")))?;
        }
        Ok(())
    }
}
fn esc(s: &str) -> String {
    s.replace('&', "&amp;")
        .replace('<', "&lt;")
        .replace('>', "&gt;")
        .replace('"', "&quot;")
}
fn dec(s: &str) -> String {
    let b = s.as_bytes();
    let mut o = Vec::new();
    let mut i = 0;
    while i < b.len() {
        if b[i] == b'+' {
            o.push(b' ');
            i += 1
        } else if b[i] == b'%' && i + 2 < b.len() {
            if let Ok(v) = u8::from_str_radix(&s[i + 1..i + 3], 16) {
                o.push(v);
                i += 3
            } else {
                o.push(b[i]);
                i += 1
            }
        } else {
            o.push(b[i]);
            i += 1
        }
    }
    String::from_utf8_lossy(&o).into_owned()
}
fn form(body: &str, key: &str) -> String {
    for item in body.split('&') {
        let mut p = item.splitn(2, '=');
        if dec(p.next().unwrap_or("")) == key {
            return dec(p.next().unwrap_or(""));
        }
    }
    String::new()
}
fn page(source: &str, target: &str, code: &str, output: &str, error: &str) -> String {
    let source_opts = source_languages()
        .iter()
        .map(|l| {
            format!(
                "<option value=\"{}\"{}>{}</option>",
                l.id,
                if l.id == source { " selected" } else { "" },
                esc(l.name)
            )
        })
        .collect::<String>();
    let targets = targets_for_source(source);
    let target_opts = targets
        .iter()
        .filter_map(|id| languages().iter().find(|l| l.id == *id))
        .map(|l| {
            format!(
                "<option value=\"{}\"{}>{}</option>",
                l.id,
                if l.id == target { " selected" } else { "" },
                esc(l.name)
            )
        })
        .collect::<String>();
    let route_json = source_languages()
        .iter()
        .map(|lang| {
            let targets = targets_for_source(lang.id)
                .iter()
                .filter_map(|id| languages().iter().find(|l| l.id == *id))
                .map(|l| format!(r#"{{"id":"{}","name":"{}"}}"#, l.id, esc(l.name)))
                .collect::<Vec<_>>()
                .join(",");
            format!(r#""{}":[{}]"#, lang.id, targets)
        })
        .collect::<Vec<_>>()
        .join(",");
    let st = backend_status();
    let badge = if st.1 {
        "Pure Rust native engine"
    } else {
        "Native engine unavailable"
    };
    let err = if error.is_empty() {
        String::new()
    } else {
        format!("<div class=e>{}</div>", esc(error))
    };
    format!(
        r#"<!doctype html><html lang="en"><head><meta charset=utf-8><meta name=viewport content="width=device-width,initial-scale=1"><title>Code Transpiler</title><style>body{{margin:0;background:#11151b;color:#edf2f7;font-family:system-ui}}header{{padding:16px 22px;background:#171d25;display:flex;justify-content:space-between}}main{{padding:20px;max-width:1400px;margin:auto}}.c{{display:flex;gap:10px;margin-bottom:12px;align-items:end;flex-wrap:wrap}}label{{font-size:12px;color:#aeb8c5;display:flex;flex-direction:column;gap:5px}}select,button{{padding:8px;background:#1b222c;color:white;border:1px solid #3a4655;border-radius:8px}}button{{background:#356ae6;cursor:pointer}}.g{{display:grid;grid-template-columns:1fr 1fr;gap:12px}}textarea{{min-height:70vh;background:#0d1117;color:#e6edf3;border:1px solid #303b49;border-radius:9px;padding:12px;font:14px monospace}}.e{{background:#51252a;padding:10px;margin-bottom:10px}}@media(max-width:800px){{.g{{grid-template-columns:1fr}}}}</style></head><body><header><b>Code Transpiler</b><span>{badge}</span></header><main>{err}<form method=POST action=/transpile><div class=c><label>Source language<select id=source name=source>{source_opts}</select></label><label>Target language<select id=target name=target>{target_opts}</select></label><button id=transpile type=submit>Transpile</button></div><div class=g><textarea name=code aria-label="Source code">{}</textarea><textarea readonly aria-label="Generated code">{}</textarea></div></form></main><script>const routes={{{route_json}}};const source=document.getElementById('source');const target=document.getElementById('target');const submit=document.getElementById('transpile');function refreshTargets(){{const previous=target.value;const items=routes[source.value]||[];target.innerHTML='';if(items.length===0){{const o=document.createElement('option');o.value='';o.textContent='No native targets yet';o.disabled=true;o.selected=true;target.appendChild(o);submit.disabled=true;return;}}submit.disabled=false;for(const item of items){{const o=document.createElement('option');o.value=item.id;o.textContent=item.name;if(item.id===previous)o.selected=true;target.appendChild(o);}}}}source.addEventListener('change',refreshTargets);</script></body></html>"#,
        esc(code),
        esc(output)
    )
}
fn handle(mut s: TcpStream) -> io::Result<()> {
    let mut reader = BufReader::new(s.try_clone()?);
    let mut first = String::new();
    reader.read_line(&mut first)?;
    let mut len = 0usize;
    loop {
        let mut l = String::new();
        reader.read_line(&mut l)?;
        if l == "\r\n" || l == "\n" {
            break;
        }
        if let Some(v) = l.to_ascii_lowercase().strip_prefix("content-length:") {
            len = v.trim().parse().unwrap_or(0)
        }
    }
    let mut buf = vec![0u8; len];
    reader.read_exact(&mut buf)?;
    let body = String::from_utf8_lossy(&buf);
    let (status, content) = if first.starts_with("GET / ") {
        (
            "200 OK",
            page(
                "go",
                "rust",
                "package main\n\nimport \"fmt\"\n\nfunc main() {\n    fmt.Println(\"hello\")\n}\n",
                "",
                "",
            ),
        )
    } else if first.starts_with("POST /transpile ") {
        let src = form(&body, "source");
        let dst = form(&body, "target");
        let code = form(&body, "code");
        match transpile(&src, &dst, &code) {
            Ok(out) => ("200 OK", page(&src, &dst, &code, &out, "")),
            Err(e) => ("200 OK", page(&src, &dst, &code, "", &e.to_string())),
        }
    } else {
        ("404 Not Found", "not found".into())
    };
    write!(s,"HTTP/1.1 {status}\r\nContent-Type: text/html; charset=utf-8\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",content.len(),content)?;
    Ok(())
}
pub fn serve_gui(addr: &str, stop: Arc<AtomicBool>) -> io::Result<()> {
    let l = TcpListener::bind(addr)?;
    l.set_nonblocking(false)?;
    for s in l.incoming() {
        if stop.load(Ordering::SeqCst) {
            break;
        }
        if let Ok(s) = s {
            thread::spawn(move || {
                let _ = handle(s);
            });
        }
    }
    Ok(())
}
pub fn gui(port: u16, open: bool) -> io::Result<GuiHandle> {
    let listener = TcpListener::bind(("127.0.0.1", port))?;
    let addr = listener.local_addr()?;
    drop(listener);
    let stop = Arc::new(AtomicBool::new(false));
    let s2 = stop.clone();
    let a = addr.to_string();
    let join = thread::spawn(move || serve_gui(&a, s2));
    let url = format!("http://{addr}/");
    if open {
        let _ = open_browser(&url);
    }
    Ok(GuiHandle {
        addr,
        stop,
        join: Some(join),
    })
}
