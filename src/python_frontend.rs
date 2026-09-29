use crate::julia_ir::{parse_julia, JuliaParseError};
use crate::Program;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PythonParseError(pub String);
impl std::fmt::Display for PythonParseError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.0)
    }
}
impl std::error::Error for PythonParseError {}

fn py_type_to_julia(t: &str) -> Result<String, PythonParseError> {
    let t = t.trim();
    if let Some(inner) = t.strip_prefix("list[").and_then(|x| x.strip_suffix(']')) {
        return Ok(format!("Vector{{{}}}", py_type_to_julia(inner)?));
    }
    if let Some(inner) = t.strip_prefix("List[").and_then(|x| x.strip_suffix(']')) {
        return Ok(format!("Vector{{{}}}", py_type_to_julia(inner)?));
    }
    Ok(match t {
        "int" => "Int",
        "float" => "Float64",
        "bool" => "Bool",
        "str" => "String",
        "None" => "Nothing",
        "" => "Any",
        other => {
            return Err(PythonParseError(format!(
                "unsupported Python type annotation: {other}"
            )))
        }
    }
    .into())
}
fn split_top_level(s: &str, needle: char) -> Vec<String> {
    let mut out = Vec::new();
    let mut d = 0i32;
    let mut q = false;
    let mut esc = false;
    let mut start = 0;
    for (i, c) in s.char_indices() {
        if q {
            if esc {
                esc = false;
                continue;
            }
            if c == '\\' {
                esc = true;
                continue;
            }
            if c == '"' || c == '\'' {
                q = false
            }
            continue;
        }
        match c {
            '"' | '\'' => q = true,
            '(' | '[' | '{' => d += 1,
            ')' | ']' | '}' => d -= 1,
            _ if c == needle && d == 0 => {
                out.push(s[start..i].trim().into());
                start = i + c.len_utf8()
            }
            _ => {}
        }
    }
    out.push(s[start..].trim().into());
    out
}
fn typed_params(raw: &str) -> Result<String, PythonParseError> {
    if raw.trim().is_empty() {
        return Ok(String::new());
    }
    split_top_level(raw, ',')
        .into_iter()
        .map(|p| {
            let (n, t) = p.split_once(':').ok_or_else(|| {
                PythonParseError(format!(
                    "Python parameters require type annotations in native frontend: {p}"
                ))
            })?;
            Ok(format!("{}::{}", n.trim(), py_type_to_julia(t)?))
        })
        .collect::<Result<Vec<_>, _>>()
        .map(|v| v.join(", "))
}
fn normalize_expr(mut x: String, zero_based_arrays: &[String]) -> String {
    x = x
        .replace("True", "true")
        .replace("False", "false")
        .replace("None", "nothing");
    x = x.replace(" and ", " && ").replace(" or ", " || ");
    if let Some(rest) = x.strip_prefix("not ") {
        x = format!("!({rest})")
    }
    x = x.replace("len(", "length(");
    for name in zero_based_arrays {
        let needle = format!("{name}[");
        let mut from = 0usize;
        while let Some(rel) = x[from..].find(&needle) {
            let start = from + rel + needle.len();
            let mut depth = 1i32;
            let mut end = None;
            for (i, c) in x[start..].char_indices() {
                match c {
                    '[' => depth += 1,
                    ']' => {
                        depth -= 1;
                        if depth == 0 {
                            end = Some(start + i);
                            break;
                        }
                    }
                    _ => {}
                }
            }
            let Some(end) = end else { break };
            let idx = x[start..end].to_string();
            let repl = format!("{name}[({idx}) + 1]");
            let whole_start = start - needle.len();
            x.replace_range(whole_start..=end, &repl);
            from = whole_start + repl.len();
        }
    }
    x
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Kind {
    Function,
    If,
    While,
}

pub fn python_to_julia_pivot(code: &str) -> Result<String, PythonParseError> {
    let mut out = Vec::<String>::new();
    let mut blocks = Vec::<(usize, Kind)>::new();
    let mut arrays = Vec::<String>::new();
    for raw in code.replace("\r\n", "\n").lines() {
        if raw.trim().is_empty() || raw.trim_start().starts_with('#') {
            continue;
        }
        let indent = raw.len() - raw.trim_start().len();
        let mut line = raw.trim().to_string();
        let continuation = line.starts_with("elif ") || line == "else:";
        while let Some(&(bi, bk)) = blocks.last() {
            if indent < bi || (indent == bi && !(continuation && bk == Kind::If)) {
                blocks.pop();
                out.push("end".into())
            } else {
                break;
            }
        }
        let mut opened = None;
        if let Some(rest) = line.strip_prefix("def ") {
            if !line.ends_with(':') {
                return Err(PythonParseError(format!(
                    "invalid Python function header: {line}"
                )));
            }
            let rest = &rest[..rest.len() - 1];
            let open = rest.find('(').ok_or_else(|| {
                PythonParseError(format!("invalid Python function header: {line}"))
            })?;
            let close = rest.rfind(')').ok_or_else(|| {
                PythonParseError(format!("invalid Python function header: {line}"))
            })?;
            let name = rest[..open].trim();
            let params = typed_params(&rest[open + 1..close])?;
            let tail = rest[close + 1..].trim();
            let ret = if let Some(t) = tail.strip_prefix("->") {
                format!("::{}", py_type_to_julia(t)?)
            } else {
                String::new()
            };
            out.push(format!("function {name}({params}){ret}"));
            opened = Some(Kind::Function)
        } else if let Some(c) = line.strip_prefix("if ").and_then(|x| x.strip_suffix(':')) {
            out.push(format!("if {}", normalize_expr(c.into(), &arrays)));
            opened = Some(Kind::If)
        } else if let Some(c) = line.strip_prefix("elif ").and_then(|x| x.strip_suffix(':')) {
            if blocks.last().map(|x| x.1) != Some(Kind::If) {
                return Err(PythonParseError("elif without matching if".into()));
            }
            out.push(format!("elseif {}", normalize_expr(c.into(), &arrays)))
        } else if line == "else:" {
            if blocks.last().map(|x| x.1) != Some(Kind::If) {
                return Err(PythonParseError("else without matching if".into()));
            }
            out.push("else".into())
        } else if let Some(c) = line
            .strip_prefix("while ")
            .and_then(|x| x.strip_suffix(':'))
        {
            out.push(format!("while {}", normalize_expr(c.into(), &arrays)));
            opened = Some(Kind::While)
        } else if line.starts_with("for ") {
            return Err(PythonParseError("Python for/range lowering is the next porting chunk; use while in the current native frontend".into()));
        } else if let Some(rest) = line.strip_prefix("return") {
            let e = rest.trim();
            out.push(if e.is_empty() {
                "return".into()
            } else {
                format!("return {}", normalize_expr(e.into(), &arrays))
            })
        } else if line == "break" || line == "continue" {
            out.push(line)
        } else {
            if let Some((lhs, rhs)) = line.split_once('=') {
                if !lhs.contains(['!', '<', '>']) && !rhs.starts_with('=') {
                    let n = lhs.trim();
                    let r = rhs.trim();
                    if n.chars().all(|c| c.is_ascii_alphanumeric() || c == '_')
                        && r.starts_with('[')
                        && r.ends_with(']')
                        && !arrays.iter().any(|x| x == n)
                    {
                        arrays.push(n.into())
                    }
                }
            }
            if line.starts_with("print(") {
                line = line.replacen("print(", "println(", 1)
            }
            out.push(normalize_expr(line, &arrays));
        }
        if let Some(k) = opened {
            blocks.push((indent, k))
        }
    }
    while blocks.pop().is_some() {
        out.push("end".into())
    }
    Ok(out.join("\n") + "\n")
}

pub fn parse_python(code: &str) -> Result<Program, PythonParseError> {
    let pivot = python_to_julia_pivot(code)?;
    parse_julia(&pivot).map_err(|e: JuliaParseError| PythonParseError(e.to_string()))
}
