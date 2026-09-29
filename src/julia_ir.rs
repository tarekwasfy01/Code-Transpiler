use crate::ir::{Expr, Function, Param, Program, Stmt, StructDef, StructField, Type};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct JuliaParseError(pub String);
impl std::fmt::Display for JuliaParseError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.0)
    }
}
impl std::error::Error for JuliaParseError {}

fn split_top_level(s: &str, needle: char) -> Vec<String> {
    let mut out = Vec::new();
    let mut depth = 0i32;
    let mut quote = false;
    let mut esc = false;
    let mut start = 0usize;
    for (i, c) in s.char_indices() {
        if quote {
            if esc {
                esc = false;
                continue;
            }
            if c == '\\' {
                esc = true;
                continue;
            }
            if c == '"' {
                quote = false
            }
            continue;
        }
        match c {
            '"' => quote = true,
            '(' | '[' | '{' => depth += 1,
            ')' | ']' | '}' => depth -= 1,
            _ if c == needle && depth == 0 => {
                out.push(s[start..i].trim().to_string());
                start = i + c.len_utf8();
            }
            _ => {}
        }
    }
    out.push(s[start..].trim().to_string());
    out
}
fn strip_outer(mut s: &str) -> &str {
    loop {
        let x = s.trim();
        if x.starts_with('(') && x.ends_with(')') {
            let mut d = 0i32;
            let mut ok = true;
            for (i, c) in x.char_indices() {
                match c {
                    '(' => d += 1,
                    ')' => {
                        d -= 1;
                        if d == 0 && i + 1 < x.len() {
                            ok = false;
                            break;
                        }
                    }
                    _ => {}
                }
            }
            if ok {
                s = &x[1..x.len() - 1];
                continue;
            }
        }
        return x;
    }
}
fn find_binary(s: &str, ops: &[&str]) -> Option<(usize, &'static str)> {
    let mut depth = 0i32;
    let mut quote = false;
    let b = s.as_bytes();
    let mut i = s.len();
    while i > 0 {
        i -= 1;
        let c = b[i] as char;
        if quote {
            if c == '"' && (i == 0 || b[i - 1] != b'\\') {
                quote = false
            }
            continue;
        }
        match c {
            '"' => quote = true,
            ')' | ']' | '}' => depth += 1,
            '(' | '[' | '{' => depth -= 1,
            _ => {}
        }
        if depth != 0 {
            continue;
        }
        for op in ops {
            if i + op.len() <= s.len() && &s[i..i + op.len()] == *op {
                if (*op == "-" || *op == "+") && i == 0 {
                    continue;
                }
                let v = match *op {
                    "||" => "||",
                    "&&" => "&&",
                    "==" => "==",
                    "!=" => "!=",
                    "<=" => "<=",
                    ">=" => ">=",
                    "<" => "<",
                    ">" => ">",
                    "+" => "+",
                    "-" => "-",
                    "*" => "*",
                    "/" => "/",
                    "%" => "%",
                    _ => continue,
                };
                return Some((i, v));
            }
        }
    }
    None
}

fn julia_type(s: &str) -> Type {
    let s = s.trim();
    if let Some(inner) = s.strip_prefix("Vector{").and_then(|x| x.strip_suffix('}')) {
        return Type::Slice(Box::new(julia_type(inner)));
    }
    if let Some(inner) = s.strip_prefix("Dict{").and_then(|x| x.strip_suffix('}')) {
        let p = split_top_level(inner, ',');
        if p.len() == 2 {
            return Type::Map(Box::new(julia_type(&p[0])), Box::new(julia_type(&p[1])));
        }
    }
    if let Some(inner) = s.strip_prefix("Tuple{").and_then(|x| x.strip_suffix('}')) {
        return Type::Tuple(
            split_top_level(inner, ',')
                .iter()
                .map(|x| julia_type(x))
                .collect(),
        );
    }
    match s {
        "Nothing" => Type::Unit,
        "Bool" => Type::Bool,
        "String" => Type::String,
        "Int" => Type::Int,
        "Int8" => Type::Int8,
        "Int16" => Type::Int16,
        "Int32" => Type::Int32,
        "Int64" => Type::Int64,
        "UInt" => Type::UInt,
        "UInt8" => Type::UInt8,
        "UInt16" => Type::UInt16,
        "UInt32" => Type::UInt32,
        "UInt64" => Type::UInt64,
        "Float32" => Type::Float32,
        "Float64" => Type::Float64,
        "Any" | "" => Type::Named("Any".into()),
        other => Type::Named(other.into()),
    }
}

pub fn parse_julia_expr(input: &str) -> Result<Expr, JuliaParseError> {
    let x = strip_outer(input.trim().trim_end_matches(';'));
    if x.is_empty() {
        return Err(JuliaParseError("empty Julia expression".into()));
    }
    let tuple = split_top_level(x, ',');
    if tuple.len() > 1 {
        return tuple
            .iter()
            .map(|p| parse_julia_expr(p))
            .collect::<Result<Vec<_>, _>>()
            .map(Expr::Tuple);
    }
    match x {
        "true" => return Ok(Expr::Bool(true)),
        "false" => return Ok(Expr::Bool(false)),
        "nothing" | "missing" => return Ok(Expr::Nil),
        _ => {}
    }
    if x.starts_with('"') && x.ends_with('"') && x.len() >= 2 {
        return Ok(Expr::String(
            x[1..x.len() - 1].replace("\\\"", "\"").replace("\\n", "\n"),
        ));
    }
    if x.parse::<i128>().is_ok() {
        return Ok(Expr::Int(x.into()));
    }
    if x.parse::<f64>().is_ok() && (x.contains('.') || x.contains('e') || x.contains('E')) {
        return Ok(Expr::Float(x.into()));
    }
    for ops in [
        &["||"][..],
        &["&&"][..],
        &["==", "!=", "<=", ">=", "<", ">"][..],
        &["+", "-"][..],
        &["*", "/", "%"][..],
    ] {
        if let Some((i, op)) = find_binary(x, ops) {
            return Ok(Expr::Binary {
                left: Box::new(parse_julia_expr(&x[..i])?),
                op: op.into(),
                right: Box::new(parse_julia_expr(&x[i + op.len()..])?),
            });
        }
    }
    if let Some(rest) = x.strip_prefix('!') {
        return Ok(Expr::Unary {
            op: "!".into(),
            value: Box::new(parse_julia_expr(rest)?),
        });
    }
    if x.starts_with('[') && x.ends_with(']') {
        let inside = &x[1..x.len() - 1];
        let items = if inside.trim().is_empty() {
            Vec::new()
        } else {
            split_top_level(inside, ',')
                .iter()
                .map(|p| parse_julia_expr(p))
                .collect::<Result<Vec<_>, _>>()?
        };
        return Ok(Expr::Array {
            items,
            element_type: None,
        });
    }
    if let Some(inner) = x.strip_prefix("Dict(").and_then(|y| y.strip_suffix(')')) {
        let mut entries = Vec::new();
        if !inner.trim().is_empty() {
            for p in split_top_level(inner, ',') {
                let mut depth = 0i32;
                let mut at = None;
                let bytes = p.as_bytes();
                let mut i = 0;
                while i + 1 < bytes.len() {
                    let c = bytes[i] as char;
                    match c {
                        '(' | '[' | '{' => depth += 1,
                        ')' | ']' | '}' => depth -= 1,
                        _ => {}
                    }
                    if depth == 0 && bytes[i] == b'=' && bytes[i + 1] == b'>' {
                        at = Some(i);
                        break;
                    }
                    i += 1;
                }
                let at =
                    at.ok_or_else(|| JuliaParseError(format!("unsupported Dict entry: {p}")))?;
                entries.push((parse_julia_expr(&p[..at])?, parse_julia_expr(&p[at + 2..])?));
            }
        }
        return Ok(Expr::Map {
            key_type: Box::new(Type::Named("Any".into())),
            value_type: Box::new(Type::Named("Any".into())),
            entries,
        });
    }
    if x.ends_with(']') {
        if let Some(open) = x.find('[') {
            let base = x[..open].trim();
            if !base.is_empty() {
                let idx = &x[open + 1..x.len() - 1];
                let one_based = parse_julia_expr(idx)?;
                let zero_based = Expr::Binary {
                    left: Box::new(one_based),
                    op: "-".into(),
                    right: Box::new(Expr::Int("1".into())),
                };
                return Ok(Expr::Index {
                    value: Box::new(parse_julia_expr(base)?),
                    index: Box::new(zero_based),
                });
            }
        }
    }
    if x.ends_with(')') {
        if let Some(open) = x.find('(') {
            let name = x[..open].trim();
            if name
                .chars()
                .all(|c| c.is_ascii_alphanumeric() || c == '_' || c == '!')
            {
                let raw = &x[open + 1..x.len() - 1];
                let args = if raw.trim().is_empty() {
                    Vec::new()
                } else {
                    split_top_level(raw, ',')
                        .iter()
                        .map(|p| parse_julia_expr(p))
                        .collect::<Result<Vec<_>, _>>()?
                };
                let function = match name {
                    "length" => "len",
                    "push!" => "append",
                    other => other,
                }
                .to_string();
                return Ok(Expr::Call { function, args });
            }
        }
    }
    if x.chars()
        .all(|c| c.is_ascii_alphanumeric() || c == '_' || c == '.')
    {
        return Ok(Expr::Ident(x.into()));
    }
    Err(JuliaParseError(format!(
        "unsupported Julia expression: {x}"
    )))
}

fn parse_signature(line: &str) -> Result<(String, Vec<Param>, Option<Type>), JuliaParseError> {
    let rest = line
        .trim()
        .strip_prefix("function ")
        .ok_or_else(|| JuliaParseError("not a function".into()))?;
    let open = rest
        .find('(')
        .ok_or_else(|| JuliaParseError(format!("invalid Julia function signature: {line}")))?;
    let mut depth = 0i32;
    let mut close = None;
    for (i, c) in rest[open..].char_indices() {
        match c {
            '(' => depth += 1,
            ')' => {
                depth -= 1;
                if depth == 0 {
                    close = Some(open + i);
                    break;
                }
            }
            _ => {}
        }
    }
    let close = close
        .ok_or_else(|| JuliaParseError(format!("unclosed Julia function signature: {line}")))?;
    let name = rest[..open].trim().to_string();
    let raw = &rest[open + 1..close];
    let tail = rest[close + 1..].trim();
    let mut params = Vec::new();
    if !raw.trim().is_empty() {
        for p in split_top_level(raw, ',') {
            let mut parts = p.splitn(2, "::");
            let n = parts.next().unwrap().trim();
            let t = parts.next().unwrap_or("Any").trim();
            params.push(Param {
                name: n.into(),
                ty: julia_type(t),
            });
        }
    }
    let ret = tail
        .strip_prefix("::")
        .map(julia_type)
        .filter(|t| !matches!(t, Type::Unit));
    Ok((name, params, ret))
}

fn top_assign(line: &str) -> Option<(String, String, String)> {
    for op in ["+=", "-=", "*=", "/=", "="] {
        let mut depth = 0i32;
        let b = line.as_bytes();
        let ob = op.as_bytes();
        let mut i = 0;
        while i + ob.len() <= b.len() {
            let c = b[i] as char;
            match c {
                '(' | '[' | '{' => depth += 1,
                ')' | ']' | '}' => depth -= 1,
                _ => {}
            }
            if depth == 0 && &b[i..i + ob.len()] == ob {
                if op == "="
                    && ((i > 0 && matches!(b[i - 1], b'=' | b'!' | b'<' | b'>'))
                        || (i + 1 < b.len() && b[i + 1] == b'='))
                {
                    i += 1;
                    continue;
                }
                return Some((
                    line[..i].trim().into(),
                    op.into(),
                    line[i + op.len()..].trim().into(),
                ));
            }
            i += 1;
        }
    }
    None
}

fn parse_block(
    lines: &[String],
    pos: &mut usize,
    stop_on_else: bool,
) -> Result<(Vec<Stmt>, Option<String>), JuliaParseError> {
    let mut out = Vec::new();
    while *pos < lines.len() {
        let line = lines[*pos].trim();
        if line.is_empty() || line.starts_with('#') {
            *pos += 1;
            continue;
        }
        if line == "end" {
            *pos += 1;
            return Ok((out, Some("end".into())));
        }
        if stop_on_else && (line == "else" || line.starts_with("elseif ")) {
            *pos += 1;
            return Ok((out, Some(line.into())));
        }
        if let Some(cond) = line.strip_prefix("if ") {
            *pos += 1;
            let cond = parse_julia_expr(cond)?;
            let (then_body, stop) = parse_block(lines, pos, true)?;
            let else_body = match stop.as_deref() {
                Some("else") => {
                    let (b, _) = parse_block(lines, pos, false)?;
                    b
                }
                Some(s) if s.starts_with("elseif ") => {
                    let nested_cond = parse_julia_expr(s.trim_start_matches("elseif "))?;
                    let (tb, st) = parse_block(lines, pos, true)?;
                    let eb = match st.as_deref() {
                        Some("else") => parse_block(lines, pos, false)?.0,
                        _ => Vec::new(),
                    };
                    vec![Stmt::If {
                        cond: nested_cond,
                        then_body: tb,
                        else_body: eb,
                    }]
                }
                _ => Vec::new(),
            };
            out.push(Stmt::If {
                cond,
                then_body,
                else_body,
            });
            continue;
        }
        if let Some(cond) = line.strip_prefix("while ") {
            *pos += 1;
            let (body, _) = parse_block(lines, pos, false)?;
            out.push(Stmt::While {
                cond: parse_julia_expr(cond)?,
                body,
            });
            continue;
        }
        if let Some(rest) = line.strip_prefix("for ") {
            if let Some((name, iter)) = rest.split_once(" in ") {
                if iter.contains(':') {
                    return Err(JuliaParseError(
                        "Julia range for-loops are not yet normalized safely".into(),
                    ));
                }
                *pos += 1;
                let (body, _) = parse_block(lines, pos, false)?;
                out.push(Stmt::ForEach {
                    value: name.trim().into(),
                    iter: parse_julia_expr(iter)?,
                    body,
                });
                continue;
            }
        }
        if let Some(rest) = line.strip_prefix("return") {
            *pos += 1;
            let v = rest.trim();
            out.push(Stmt::Return(if v.is_empty() {
                None
            } else {
                Some(parse_julia_expr(v)?)
            }));
            continue;
        }
        if line == "break" {
            out.push(Stmt::Break);
            *pos += 1;
            continue;
        }
        if line == "continue" {
            out.push(Stmt::Continue);
            *pos += 1;
            continue;
        }
        if (line.starts_with("println(") || line.starts_with("print(")) && line.ends_with(')') {
            let newline = line.starts_with("println(");
            let open = line.find('(').unwrap();
            let raw = &line[open + 1..line.len() - 1];
            let args = if raw.trim().is_empty() {
                Vec::new()
            } else {
                split_top_level(raw, ',')
                    .iter()
                    .map(|p| parse_julia_expr(p))
                    .collect::<Result<Vec<_>, _>>()?
            };
            out.push(Stmt::Print { newline, args });
            *pos += 1;
            continue;
        }
        if line.starts_with("push!(") && line.ends_with(')') {
            let raw = &line[6..line.len() - 1];
            let p = split_top_level(raw, ',');
            if p.len() == 2 {
                out.push(Stmt::Assign {
                    target: p[0].clone(),
                    op: "=".into(),
                    value: Expr::Call {
                        function: "append".into(),
                        args: vec![parse_julia_expr(&p[0])?, parse_julia_expr(&p[1])?],
                    },
                });
                *pos += 1;
                continue;
            }
        }
        if let Some((lhs, op, rhs)) = top_assign(line) {
            if lhs.contains(',') {
                out.push(if op == "=" {
                    Stmt::MultiLet {
                        names: split_top_level(&lhs, ','),
                        value: parse_julia_expr(&rhs)?,
                        mutable: true,
                    }
                } else {
                    return Err(JuliaParseError(
                        "compound tuple assignment unsupported".into(),
                    ));
                });
                *pos += 1;
                continue;
            }
            if lhs.ends_with(']') {
                if let Some(open) = lhs.find('[') {
                    let base = &lhs[..open];
                    let idx = &lhs[open + 1..lhs.len() - 1];
                    let zero = Expr::Binary {
                        left: Box::new(parse_julia_expr(idx)?),
                        op: "-".into(),
                        right: Box::new(Expr::Int("1".into())),
                    };
                    out.push(Stmt::IndexAssign {
                        collection: parse_julia_expr(base)?,
                        index: zero,
                        op,
                        value: parse_julia_expr(&rhs)?,
                        map_value_type: None,
                    });
                    *pos += 1;
                    continue;
                }
            }
            let existing = out
                .iter()
                .any(|s| matches!(s,Stmt::Let{name,..} if name==&lhs));
            if op == "=" && !existing {
                out.push(Stmt::Let {
                    name: lhs,
                    ty: None,
                    value: parse_julia_expr(&rhs)?,
                    mutable: true,
                })
            } else {
                out.push(Stmt::Assign {
                    target: lhs,
                    op,
                    value: parse_julia_expr(&rhs)?,
                })
            };
            *pos += 1;
            continue;
        }
        out.push(Stmt::Expr(parse_julia_expr(line)?));
        *pos += 1;
    }
    Ok((out, None))
}

pub fn parse_julia(code: &str) -> Result<Program, JuliaParseError> {
    let lines = code
        .replace("\r\n", "\n")
        .lines()
        .map(|x| x.trim().to_string())
        .collect::<Vec<_>>();
    let mut structs = Vec::new();
    let mut functions = Vec::new();
    let mut i = 0usize;
    while i < lines.len() {
        let line = lines[i].trim();
        if line.is_empty() || line.starts_with('#') || line == "main()" {
            i += 1;
            continue;
        }
        if let Some(name) = line
            .strip_prefix("mutable struct ")
            .or_else(|| line.strip_prefix("struct "))
        {
            i += 1;
            let mut fields = Vec::new();
            while i < lines.len() && lines[i].trim() != "end" {
                let f = lines[i].trim();
                if !f.is_empty() {
                    let mut p = f.splitn(2, "::");
                    let n = p.next().unwrap().trim();
                    let t = p.next().ok_or_else(|| {
                        JuliaParseError(format!("untyped Julia struct field unsupported: {f}"))
                    })?;
                    fields.push(StructField {
                        name: n.into(),
                        ty: julia_type(t),
                    });
                }
                i += 1;
            }
            if i >= lines.len() {
                return Err(JuliaParseError(format!("unterminated struct {name}")));
            }
            i += 1;
            structs.push(StructDef {
                name: name.trim().into(),
                fields,
            });
            continue;
        }
        if line.starts_with("function ") {
            let (name, params, return_type) = parse_signature(line)?;
            i += 1;
            let (body, _) = parse_block(&lines, &mut i, false)?;
            functions.push(Function {
                name,
                params,
                return_type,
                body,
            });
            continue;
        }
        // Generated keyword constructor helpers are safe to ignore for the neutral IR.
        if line.contains("(; ") && line.contains(" = ") {
            i += 1;
            continue;
        }
        return Err(JuliaParseError(format!(
            "unsupported Julia top-level syntax: {line}"
        )));
    }
    Ok(Program { structs, functions })
}
