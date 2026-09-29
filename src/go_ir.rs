use crate::ir::{Expr, Function, Param, Program, Stmt, StructDef, StructField, SwitchCase, Type};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct GoParseError(pub String);
impl std::fmt::Display for GoParseError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.0)
    }
}
impl std::error::Error for GoParseError {}

fn go_type(s: &str) -> Type {
    let s = s.trim();
    if let Some(inner) = s.strip_prefix("[]") {
        return Type::Slice(Box::new(go_type(inner)));
    }
    if let Some(rest) = s.strip_prefix("map[") {
        if let Some(close) = rest.find(']') {
            let key = &rest[..close];
            let value = &rest[close + 1..];
            return Type::Map(Box::new(go_type(key)), Box::new(go_type(value)));
        }
    }
    match s {
        "bool" => Type::Bool,
        "string" => Type::String,
        "int" => Type::Int,
        "int8" => Type::Int8,
        "int16" => Type::Int16,
        "int32" | "rune" => Type::Int32,
        "int64" => Type::Int64,
        "uint" => Type::UInt,
        "uint8" | "byte" => Type::UInt8,
        "uint16" => Type::UInt16,
        "uint32" => Type::UInt32,
        "uint64" => Type::UInt64,
        "float32" => Type::Float32,
        "float64" => Type::Float64,
        "" => Type::Unit,
        other => Type::Named(other.to_string()),
    }
}

fn parse_return_type(s: &str) -> Result<Option<Type>, GoParseError> {
    let s = s.trim();
    if s.is_empty() {
        return Ok(None);
    }
    if s.starts_with('(') && s.ends_with(')') {
        let inner = &s[1..s.len() - 1];
        let mut types = Vec::new();
        for part in split_top_level(inner, ',') {
            let words: Vec<_> = part.split_whitespace().collect();
            match words.as_slice() {
                [ty] => types.push(go_type(ty)),
                [_name, ty] => types.push(go_type(ty)),
                _ => {
                    return Err(GoParseError(format!(
                        "unsupported multiple return syntax: {part}"
                    )))
                }
            }
        }
        return Ok(Some(Type::Tuple(types)));
    }
    Ok(Some(go_type(s)))
}

fn zero_expr(t: &Type) -> Option<Expr> {
    match t {
        Type::Bool => Some(Expr::Bool(false)),
        Type::String => Some(Expr::String(String::new())),
        Type::Int
        | Type::Int8
        | Type::Int16
        | Type::Int32
        | Type::Int64
        | Type::UInt
        | Type::UInt8
        | Type::UInt16
        | Type::UInt32
        | Type::UInt64 => Some(Expr::Int("0".into())),
        Type::Float32 | Type::Float64 => Some(Expr::Float("0.0".into())),
        _ => None,
    }
}

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
                quote = false;
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

fn find_top_level_char(s: &str, needle: char) -> Option<usize> {
    let mut depth = 0i32;
    let mut quote = false;
    let mut esc = false;
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
                quote = false;
            }
            continue;
        }
        match c {
            '"' => quote = true,
            '(' | '[' | '{' => depth += 1,
            ')' | ']' | '}' => depth -= 1,
            _ if c == needle && depth == 0 => return Some(i),
            _ => {}
        }
    }
    None
}

fn matching_paren(s: &str, open: usize) -> Option<usize> {
    let mut depth = 0usize;
    let mut quote = false;
    let mut esc = false;
    for (offset, c) in s[open..].char_indices() {
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
                quote = false;
            }
            continue;
        }
        match c {
            '"' => quote = true,
            '(' => depth += 1,
            ')' => {
                depth = depth.checked_sub(1)?;
                if depth == 0 {
                    return Some(open + offset);
                }
            }
            _ => {}
        }
    }
    None
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
                quote = false;
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
                if (op == &"-" || op == &"+") && i == 0 {
                    continue;
                }
                let leaked: &'static str = match *op {
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
                return Some((i, leaked));
            }
        }
    }
    None
}

pub fn parse_expr(s: &str) -> Result<Expr, GoParseError> {
    let x = s.trim().trim_end_matches(';').trim();
    if x.is_empty() {
        return Err(GoParseError("empty expression".into()));
    }

    let tuple_parts = split_top_level(x, ',');
    if tuple_parts.len() > 1 {
        return tuple_parts
            .iter()
            .map(|part| parse_expr(part))
            .collect::<Result<Vec<_>, _>>()
            .map(Expr::Tuple);
    }

    if x == "true" {
        return Ok(Expr::Bool(true));
    }
    if x == "false" {
        return Ok(Expr::Bool(false));
    }
    if x == "nil" {
        return Ok(Expr::Nil);
    }
    if x.starts_with('"') && x.ends_with('"') && x.len() >= 2 {
        return Ok(Expr::String(x[1..x.len() - 1].to_string()));
    }
    if x.parse::<i128>().is_ok() {
        return Ok(Expr::Int(x.to_string()));
    }
    if x.contains('.') && x.parse::<f64>().is_ok() {
        return Ok(Expr::Float(x.to_string()));
    }

    for ops in [
        &["||"][..],
        &["&&"][..],
        &["==", "!=", "<=", ">=", "<", ">"][..],
        &["+", "-"][..],
        &["*", "/", "%"][..],
    ] {
        if let Some((i, op)) = find_binary(x, ops) {
            let l = parse_expr(&x[..i])?;
            let r = parse_expr(&x[i + op.len()..])?;
            return Ok(Expr::Binary {
                left: Box::new(l),
                op: op.to_string(),
                right: Box::new(r),
            });
        }
    }
    if let Some(rest) = x.strip_prefix('!') {
        return Ok(Expr::Unary {
            op: "!".into(),
            value: Box::new(parse_expr(rest)?),
        });
    }
    if let Some(rest) = x.strip_prefix('-') {
        return Ok(Expr::Unary {
            op: "-".into(),
            value: Box::new(parse_expr(rest)?),
        });
    }

    if x.starts_with("[]") && x.ends_with('}') {
        if let Some(open) = x.find('{') {
            let element_text = x[2..open].trim();
            let element_type = if element_text.is_empty() {
                None
            } else {
                Some(Box::new(go_type(element_text)))
            };
            let inside = &x[open + 1..x.len() - 1];
            let mut items = Vec::new();
            if !inside.trim().is_empty() {
                for item in split_top_level(inside, ',') {
                    items.push(parse_expr(&item)?);
                }
            }
            return Ok(Expr::Array {
                items,
                element_type,
            });
        }
    }

    // Go map composite literal: map[K]V{k1: v1, k2: v2}.
    if x.starts_with("map[") && x.ends_with('}') {
        if let Some(open) = x.find('{') {
            let ty_text = x[..open].trim();
            if let Type::Map(key_type, value_type) = go_type(ty_text) {
                let inside = &x[open + 1..x.len() - 1];
                let mut entries = Vec::new();
                if !inside.trim().is_empty() {
                    for part in split_top_level(inside, ',') {
                        let colon = find_top_level_char(&part, ':').ok_or_else(|| {
                            GoParseError(format!("map literal entry needs key:value: {part}"))
                        })?;
                        entries
                            .push((parse_expr(&part[..colon])?, parse_expr(&part[colon + 1..])?));
                    }
                }
                return Ok(Expr::Map {
                    key_type,
                    value_type,
                    entries,
                });
            }
        }
    }

    // Named struct composite literal: Point{X: 1, Y: 2} or Point{1, 2}.
    if x.ends_with('}') {
        if let Some(open) = x.find('{') {
            let ty = x[..open].trim();
            if !ty.is_empty()
                && ty
                    .chars()
                    .all(|c| c.is_ascii_alphanumeric() || c == '_' || c == '.')
            {
                let inside = &x[open + 1..x.len() - 1];
                let parts = if inside.trim().is_empty() {
                    Vec::new()
                } else {
                    split_top_level(inside, ',')
                };
                let keyed = parts.iter().any(|p| find_top_level_char(p, ':').is_some());
                if keyed {
                    let mut fields = Vec::new();
                    for part in parts {
                        let colon = find_top_level_char(&part, ':').ok_or_else(|| {
                            GoParseError("cannot mix keyed and positional struct fields".into())
                        })?;
                        let name = part[..colon].trim();
                        if name.is_empty() {
                            return Err(GoParseError("empty struct field name".into()));
                        }
                        fields.push((name.to_string(), parse_expr(&part[colon + 1..])?));
                    }
                    return Ok(Expr::StructLiteral {
                        ty: ty.to_string(),
                        fields,
                        positional: Vec::new(),
                    });
                }
                let positional = parts
                    .iter()
                    .map(|part| parse_expr(part))
                    .collect::<Result<Vec<_>, _>>()?;
                return Ok(Expr::StructLiteral {
                    ty: ty.to_string(),
                    fields: Vec::new(),
                    positional,
                });
            }
        }
    }

    if x.ends_with(')') {
        if let Some(open) = x.find('(') {
            let fun = x[..open].trim();
            if !fun.is_empty() {
                let inside = &x[open + 1..x.len() - 1];
                let mut args = Vec::new();
                if !inside.trim().is_empty() {
                    for a in split_top_level(inside, ',') {
                        args.push(parse_expr(&a)?);
                    }
                }
                return Ok(Expr::Call {
                    function: fun.to_string(),
                    args,
                });
            }
        }
    }
    if x.ends_with(']') {
        if let Some(open) = x.rfind('[') {
            return Ok(Expr::Index {
                value: Box::new(parse_expr(&x[..open])?),
                index: Box::new(parse_expr(&x[open + 1..x.len() - 1])?),
            });
        }
    }
    if x.chars()
        .all(|c| c.is_ascii_alphanumeric() || c == '_' || c == '.')
    {
        return Ok(Expr::Ident(x.to_string()));
    }
    Ok(Expr::Raw(x.to_string()))
}

fn parse_params(s: &str) -> Result<Vec<Param>, GoParseError> {
    if s.trim().is_empty() {
        return Ok(Vec::new());
    }
    let chunks = split_top_level(s, ',');
    let mut out = Vec::new();
    let mut pending_names: Vec<String> = Vec::new();
    for item in chunks {
        let words: Vec<&str> = item.split_whitespace().collect();
        match words.as_slice() {
            [name] => pending_names.push((*name).to_string()),
            [name, ty] => {
                pending_names.push((*name).to_string());
                for n in pending_names.drain(..) {
                    out.push(Param {
                        name: n,
                        ty: go_type(ty),
                    });
                }
            }
            _ => {
                return Err(GoParseError(format!(
                    "unsupported Go parameter syntax: {item}"
                )))
            }
        }
    }
    if !pending_names.is_empty() {
        return Err(GoParseError("parameter group missing type".into()));
    }
    Ok(out)
}

#[derive(Debug)]
enum Frame {
    Function(Function),
    If {
        cond: Expr,
        then_body: Vec<Stmt>,
        else_body: Vec<Stmt>,
        in_else: bool,
    },
    While {
        cond: Expr,
        body: Vec<Stmt>,
    },
    ForEach {
        value: String,
        iter: Expr,
        body: Vec<Stmt>,
    },
    ForClassic {
        cond: Expr,
        body: Vec<Stmt>,
        post: Box<Stmt>,
    },
    Switch {
        value: Option<Expr>,
        cases: Vec<SwitchCase>,
        default: Vec<Stmt>,
        active_case: Option<usize>,
        in_default: bool,
    },
}

fn push_stmt(frames: &mut [Frame], stmt: Stmt) -> Result<(), GoParseError> {
    let top = frames
        .last_mut()
        .ok_or_else(|| GoParseError("statement outside function".into()))?;
    match top {
        Frame::Function(f) => f.body.push(stmt),
        Frame::If {
            then_body,
            else_body,
            in_else,
            ..
        } => {
            if *in_else {
                else_body.push(stmt)
            } else {
                then_body.push(stmt)
            }
        }
        Frame::While { body, .. } => body.push(stmt),
        Frame::ForEach { body, .. } => body.push(stmt),
        Frame::ForClassic { body, .. } => body.push(stmt),
        Frame::Switch {
            cases,
            default,
            active_case,
            in_default,
            ..
        } => {
            if *in_default {
                default.push(stmt);
            } else if let Some(i) = *active_case {
                cases[i].body.push(stmt);
            } else {
                return Err(GoParseError(
                    "switch statement before first case/default".into(),
                ));
            }
        }
    }
    Ok(())
}

fn contains_continue(body: &[Stmt]) -> bool {
    body.iter().any(|s| match s {
        Stmt::Continue => true,
        Stmt::If {
            then_body,
            else_body,
            ..
        } => contains_continue(then_body) || contains_continue(else_body),
        Stmt::While { body, .. } | Stmt::ForEach { body, .. } => contains_continue(body),
        Stmt::Switch { cases, default, .. } => {
            cases.iter().any(|c| contains_continue(&c.body)) || contains_continue(default)
        }
        _ => false,
    })
}

fn contains_break(body: &[Stmt]) -> bool {
    body.iter().any(|s| match s {
        Stmt::Break => true,
        Stmt::If {
            then_body,
            else_body,
            ..
        } => contains_break(then_body) || contains_break(else_body),
        Stmt::While { body, .. } | Stmt::ForEach { body, .. } => contains_break(body),
        Stmt::Switch { cases, default, .. } => {
            cases.iter().any(|c| contains_break(&c.body)) || contains_break(default)
        }
        _ => false,
    })
}

fn close_frame(frames: &mut Vec<Frame>, program: &mut Program) -> Result<(), GoParseError> {
    let frame = frames
        .pop()
        .ok_or_else(|| GoParseError("unbalanced closing brace".into()))?;
    match frame {
        Frame::Function(f) => {
            if frames.is_empty() {
                program.functions.push(f);
                Ok(())
            } else {
                Err(GoParseError("nested Go functions are unsupported".into()))
            }
        }
        Frame::If {
            cond,
            then_body,
            else_body,
            ..
        } => push_stmt(
            frames,
            Stmt::If {
                cond,
                then_body,
                else_body,
            },
        ),
        Frame::While { cond, body } => push_stmt(frames, Stmt::While { cond, body }),
        Frame::ForEach { value, iter, body } => {
            push_stmt(frames, Stmt::ForEach { value, iter, body })
        }
        Frame::ForClassic {
            cond,
            mut body,
            post,
        } => {
            if contains_continue(&body) {
                return Err(GoParseError("classic Go for-loop with continue is deferred until post-on-continue lowering is native".into()));
            }
            body.push(*post);
            push_stmt(frames, Stmt::While { cond, body })
        }
        Frame::Switch {
            value,
            cases,
            default,
            ..
        } => {
            if cases.iter().any(|c| contains_break(&c.body)) || contains_break(&default) {
                return Err(GoParseError(
                    "break inside switch is deferred until switch-exit lowering is native".into(),
                ));
            }
            push_stmt(
                frames,
                Stmt::Switch {
                    value,
                    cases,
                    default,
                },
            )
        }
    }
}

fn parse_inline_stmt(s: &str) -> Result<Stmt, GoParseError> {
    let s = s.trim().trim_end_matches(';').trim();
    if let Some(pos) = s.find(":=") {
        let names = split_top_level(&s[..pos], ',');
        let value = parse_expr(&s[pos + 2..])?;
        if names.len() > 1 {
            return Ok(Stmt::MultiLet {
                names,
                value,
                mutable: true,
            });
        }
        return Ok(Stmt::Let {
            name: names[0].clone(),
            ty: None,
            value,
            mutable: true,
        });
    }
    if let Some(stripped) = s.strip_suffix("++") {
        return Ok(Stmt::Assign {
            target: stripped.trim().into(),
            op: "+=".into(),
            value: Expr::Int("1".into()),
        });
    }
    if let Some(stripped) = s.strip_suffix("--") {
        return Ok(Stmt::Assign {
            target: stripped.trim().into(),
            op: "-=".into(),
            value: Expr::Int("1".into()),
        });
    }
    for op in ["+=", "-=", "*=", "/=", "="] {
        if let Some(pos) = s.find(op) {
            if op == "="
                && (s.contains("==") || s.contains("!=") || s.contains("<=") || s.contains(">="))
            {
                continue;
            }
            let lhs = &s[..pos];
            let targets = split_top_level(lhs, ',');
            if targets.len() > 1 {
                if op != "=" {
                    return Err(GoParseError(
                        "compound tuple assignment is unsupported".into(),
                    ));
                }
                return Ok(Stmt::MultiAssign {
                    targets,
                    value: parse_expr(&s[pos + 1..])?,
                });
            }
            if lhs.contains('[') {
                match parse_expr(lhs.trim())? {
                    Expr::Index {
                        value: collection,
                        index,
                    } => {
                        return Ok(Stmt::IndexAssign {
                            collection: *collection,
                            index: *index,
                            op: op.into(),
                            value: parse_expr(&s[pos + op.len()..])?,
                            map_value_type: None,
                        });
                    }
                    _ => {
                        return Err(GoParseError(
                            "only direct slice/map indexed assignment is native".into(),
                        ));
                    }
                }
            }
            return Ok(Stmt::Assign {
                target: lhs.trim().into(),
                op: op.into(),
                value: parse_expr(&s[pos + op.len()..])?,
            });
        }
    }
    Ok(Stmt::Expr(parse_expr(s)?))
}

fn parse_struct_field_line(s: &str) -> Result<Vec<StructField>, GoParseError> {
    let mut words: Vec<&str> = s.trim_end_matches(';').split_whitespace().collect();
    if words.len() < 2 {
        return Err(GoParseError(format!("unsupported struct field: {s}")));
    }
    let ty = words.pop().unwrap();
    let names = words.join(" ");
    let mut fields = Vec::new();
    for name in names.split(',').map(str::trim).filter(|x| !x.is_empty()) {
        fields.push(StructField {
            name: name.to_string(),
            ty: go_type(ty),
        });
    }
    Ok(fields)
}

fn local_expr_type(
    e: &Expr,
    env: &std::collections::BTreeMap<String, Type>,
    functions: &std::collections::BTreeMap<String, Option<Type>>,
) -> Option<Type> {
    match e {
        Expr::Ident(name) => env.get(name).cloned(),
        Expr::Int(_) => Some(Type::Int),
        Expr::Float(_) => Some(Type::Float64),
        Expr::String(_) => Some(Type::String),
        Expr::Bool(_) => Some(Type::Bool),
        Expr::Unary { value, .. } => local_expr_type(value, env, functions),
        Expr::Binary { left, op, .. } => {
            if matches!(
                op.as_str(),
                "==" | "!=" | "<" | "<=" | ">" | ">=" | "&&" | "||"
            ) {
                Some(Type::Bool)
            } else {
                local_expr_type(left, env, functions)
            }
        }
        Expr::Call { function, .. } => functions.get(function).cloned().flatten(),
        Expr::Index { value, .. } => match local_expr_type(value, env, functions)? {
            Type::Slice(inner) => Some(*inner),
            Type::Map(_, value) => Some(*value),
            _ => None,
        },
        Expr::MapIndex { value_type, .. } => Some((**value_type).clone()),
        Expr::MapLookupOk { value_type, .. } => {
            Some(Type::Tuple(vec![(**value_type).clone(), Type::Bool]))
        }
        Expr::Array {
            element_type,
            items,
        } => element_type
            .as_ref()
            .map(|t| Type::Slice(t.clone()))
            .or_else(|| {
                items
                    .first()
                    .and_then(|x| local_expr_type(x, env, functions))
                    .map(|t| Type::Slice(Box::new(t)))
            }),
        Expr::Map {
            key_type,
            value_type,
            ..
        } => Some(Type::Map(key_type.clone(), value_type.clone())),
        Expr::Tuple(items) => Some(Type::Tuple(
            items
                .iter()
                .map(|x| local_expr_type(x, env, functions))
                .collect::<Option<Vec<_>>>()?,
        )),
        Expr::StructLiteral { ty, .. } => Some(Type::Named(ty.clone())),
        Expr::Nil | Expr::Raw(_) => None,
    }
}

fn annotate_expr(
    e: &mut Expr,
    env: &std::collections::BTreeMap<String, Type>,
    functions: &std::collections::BTreeMap<String, Option<Type>>,
) {
    match e {
        Expr::Unary { value, .. } => annotate_expr(value, env, functions),
        Expr::Binary { left, right, .. } => {
            annotate_expr(left, env, functions);
            annotate_expr(right, env, functions);
        }
        Expr::Call { args, .. } => {
            for arg in args {
                annotate_expr(arg, env, functions);
            }
        }
        Expr::Index { value, index } => {
            annotate_expr(value, env, functions);
            annotate_expr(index, env, functions);
            if let Some(Type::Map(_, value_type)) = local_expr_type(value, env, functions) {
                let v = std::mem::replace(value, Box::new(Expr::Nil));
                let i = std::mem::replace(index, Box::new(Expr::Nil));
                *e = Expr::MapIndex {
                    value: v,
                    index: i,
                    value_type,
                };
            }
        }
        Expr::MapIndex { value, index, .. } | Expr::MapLookupOk { value, index, .. } => {
            annotate_expr(value, env, functions);
            annotate_expr(index, env, functions);
        }
        Expr::Array { items, .. } => {
            for item in items {
                annotate_expr(item, env, functions);
            }
        }
        Expr::Map { entries, .. } => {
            for (key, value) in entries {
                annotate_expr(key, env, functions);
                annotate_expr(value, env, functions);
            }
        }
        Expr::Tuple(items) => {
            for item in items {
                annotate_expr(item, env, functions);
            }
        }
        Expr::StructLiteral {
            fields, positional, ..
        } => {
            for (_, value) in fields {
                annotate_expr(value, env, functions);
            }
            for value in positional {
                annotate_expr(value, env, functions);
            }
        }
        Expr::Ident(_)
        | Expr::Int(_)
        | Expr::Float(_)
        | Expr::String(_)
        | Expr::Bool(_)
        | Expr::Nil
        | Expr::Raw(_) => {}
    }
}

fn annotate_statements(
    stmts: &mut [Stmt],
    env: &mut std::collections::BTreeMap<String, Type>,
    functions: &std::collections::BTreeMap<String, Option<Type>>,
) {
    for stmt in stmts {
        match stmt {
            Stmt::Let {
                name, ty, value, ..
            } => {
                annotate_expr(value, env, functions);
                if let Some(t) = ty
                    .clone()
                    .or_else(|| local_expr_type(value, env, functions))
                {
                    env.insert(name.clone(), t);
                }
            }
            Stmt::MultiLet { names, value, .. } => {
                annotate_expr(value, env, functions);
                if names.len() == 2 {
                    if let Expr::MapIndex {
                        value: map,
                        index,
                        value_type,
                    } = value
                    {
                        let map = std::mem::replace(map, Box::new(Expr::Nil));
                        let index = std::mem::replace(index, Box::new(Expr::Nil));
                        let value_type = value_type.clone();
                        *value = Expr::MapLookupOk {
                            value: map,
                            index,
                            value_type,
                        };
                    }
                }
                if let Some(Type::Tuple(types)) = local_expr_type(value, env, functions) {
                    for (name, ty) in names.iter().zip(types) {
                        env.insert(name.clone(), ty);
                    }
                }
            }
            Stmt::Assign { value, .. } | Stmt::MultiAssign { value, .. } | Stmt::Expr(value) => {
                annotate_expr(value, env, functions)
            }
            Stmt::IndexAssign {
                collection,
                index,
                value,
                map_value_type,
                ..
            } => {
                annotate_expr(collection, env, functions);
                annotate_expr(index, env, functions);
                annotate_expr(value, env, functions);
                if let Some(Type::Map(_, item)) = local_expr_type(collection, env, functions) {
                    *map_value_type = Some((*item).clone());
                }
            }
            Stmt::Print { args, .. } => {
                for arg in args {
                    annotate_expr(arg, env, functions);
                }
            }
            Stmt::Return(Some(value)) => annotate_expr(value, env, functions),
            Stmt::Return(None) | Stmt::Break | Stmt::Continue => {}
            Stmt::If {
                cond,
                then_body,
                else_body,
            } => {
                annotate_expr(cond, env, functions);
                let mut then_env = env.clone();
                annotate_statements(then_body, &mut then_env, functions);
                let mut else_env = env.clone();
                annotate_statements(else_body, &mut else_env, functions);
            }
            Stmt::While { cond, body } => {
                annotate_expr(cond, env, functions);
                let mut inner = env.clone();
                annotate_statements(body, &mut inner, functions);
            }
            Stmt::ForEach { value, iter, body } => {
                annotate_expr(iter, env, functions);
                let iter_type = local_expr_type(iter, env, functions);
                let mut inner = env.clone();
                match iter_type {
                    Some(Type::Slice(item)) => {
                        inner.insert(value.clone(), (*item).clone());
                        let original = std::mem::replace(iter, Expr::Nil);
                        *iter = Expr::Call {
                            function: "__go_slice_values".into(),
                            args: vec![original],
                        };
                    }
                    Some(Type::Map(_, item)) => {
                        inner.insert(value.clone(), (*item).clone());
                        let original = std::mem::replace(iter, Expr::Nil);
                        *iter = Expr::Call {
                            function: "__go_map_values".into(),
                            args: vec![original],
                        };
                    }
                    _ => {}
                }
                annotate_statements(body, &mut inner, functions);
            }
            Stmt::Switch {
                value,
                cases,
                default,
            } => {
                if let Some(v) = value {
                    annotate_expr(v, env, functions);
                }
                for case in cases {
                    for v in &mut case.values {
                        annotate_expr(v, env, functions);
                    }
                    let mut inner = env.clone();
                    annotate_statements(&mut case.body, &mut inner, functions);
                }
                let mut inner = env.clone();
                annotate_statements(default, &mut inner, functions);
            }
        }
    }
}

fn annotate_program(program: &mut Program) {
    let functions = program
        .functions
        .iter()
        .map(|f| (f.name.clone(), f.return_type.clone()))
        .collect::<std::collections::BTreeMap<_, _>>();
    for function in &mut program.functions {
        let mut env = function
            .params
            .iter()
            .map(|p| (p.name.clone(), p.ty.clone()))
            .collect::<std::collections::BTreeMap<_, _>>();
        annotate_statements(&mut function.body, &mut env, &functions);
    }
}

pub fn parse_go(code: &str) -> Result<Program, GoParseError> {
    let normalized = code.replace("\r\n", "\n");
    let mut program = Program {
        structs: Vec::new(),
        functions: Vec::new(),
    };
    let mut frames: Vec<Frame> = Vec::new();
    let mut import_block = false;
    let mut struct_builder: Option<StructDef> = None;

    for (line_no, raw) in normalized.lines().enumerate() {
        let mut s = raw.trim();
        if s.is_empty() || s.starts_with("//") || s.starts_with("package ") {
            continue;
        }

        if let Some(def) = struct_builder.as_mut() {
            if s == "}" {
                program.structs.push(struct_builder.take().unwrap());
            } else {
                def.fields.extend(
                    parse_struct_field_line(s)
                        .map_err(|e| GoParseError(format!("line {}: {}", line_no + 1, e.0)))?,
                );
            }
            continue;
        }

        if import_block {
            if s == ")" {
                import_block = false;
            }
            continue;
        }
        if s.starts_with("import (") {
            import_block = true;
            continue;
        }
        if s.starts_with("import ") {
            continue;
        }

        if frames.is_empty() {
            if let Some(rest) = s.strip_prefix("type ") {
                if let Some(name) = rest.strip_suffix(" struct {") {
                    struct_builder = Some(StructDef {
                        name: name.trim().to_string(),
                        fields: Vec::new(),
                    });
                    continue;
                }
                return Err(GoParseError(format!(
                    "line {}: only struct type declarations are native so far",
                    line_no + 1
                )));
            }
            if s.starts_with("const ") || s.starts_with("var ") {
                return Err(GoParseError(format!(
                    "line {}: top-level const/var declarations are not yet native",
                    line_no + 1
                )));
            }
        }

        if s == "}" {
            close_frame(&mut frames, &mut program)?;
            continue;
        }
        if s == "} else {" {
            match frames.last_mut() {
                Some(Frame::If { in_else, .. }) => {
                    *in_else = true;
                    continue;
                }
                _ => {
                    return Err(GoParseError(format!(
                        "line {}: else without matching if",
                        line_no + 1
                    )))
                }
            }
        }

        if let Some(rest) = s.strip_prefix("func ") {
            if !frames.is_empty() {
                return Err(GoParseError(format!(
                    "line {}: methods/nested functions are not yet native",
                    line_no + 1
                )));
            }
            let open = rest
                .find('(')
                .ok_or_else(|| GoParseError(format!("line {}: malformed function", line_no + 1)))?;
            let close = matching_paren(rest, open)
                .ok_or_else(|| GoParseError(format!("line {}: malformed function", line_no + 1)))?;
            let name = rest[..open].trim().to_string();
            let params = parse_params(&rest[open + 1..close])?;
            let tail = rest[close + 1..].trim();
            if !tail.ends_with('{') {
                return Err(GoParseError(format!(
                    "line {}: function body must open on same line",
                    line_no + 1
                )));
            }
            let ret = tail[..tail.len() - 1].trim();
            let return_type = parse_return_type(ret)?;
            frames.push(Frame::Function(Function {
                name,
                params,
                return_type,
                body: Vec::new(),
            }));
            continue;
        }

        if s.starts_with("if ") && s.ends_with('{') {
            let cond = parse_expr(s[3..s.len() - 1].trim())?;
            frames.push(Frame::If {
                cond,
                then_body: Vec::new(),
                else_body: Vec::new(),
                in_else: false,
            });
            continue;
        }

        if (s == "switch {" || s.starts_with("switch ")) && s.ends_with('{') {
            let inner = s["switch".len()..s.len() - 1].trim();
            let value = if inner.is_empty() {
                None
            } else {
                Some(parse_expr(inner)?)
            };
            frames.push(Frame::Switch {
                value,
                cases: Vec::new(),
                default: Vec::new(),
                active_case: None,
                in_default: false,
            });
            continue;
        }
        if let Some(case_text) = s.strip_prefix("case ").and_then(|x| x.strip_suffix(':')) {
            match frames.last_mut() {
                Some(Frame::Switch {
                    cases,
                    active_case,
                    in_default,
                    ..
                }) => {
                    let values = split_top_level(case_text, ',')
                        .iter()
                        .map(|x| parse_expr(x))
                        .collect::<Result<Vec<_>, _>>()?;
                    cases.push(SwitchCase {
                        values,
                        body: Vec::new(),
                    });
                    *active_case = Some(cases.len() - 1);
                    *in_default = false;
                    continue;
                }
                _ => {
                    return Err(GoParseError(format!(
                        "line {}: case outside switch",
                        line_no + 1
                    )))
                }
            }
        }
        if s == "default:" {
            match frames.last_mut() {
                Some(Frame::Switch {
                    active_case,
                    in_default,
                    ..
                }) => {
                    *active_case = None;
                    *in_default = true;
                    continue;
                }
                _ => {
                    return Err(GoParseError(format!(
                        "line {}: default outside switch",
                        line_no + 1
                    )))
                }
            }
        }

        if s.starts_with("for ") && s.ends_with('{') {
            let inner = s[4..s.len() - 1].trim();
            if inner.is_empty() {
                frames.push(Frame::While {
                    cond: Expr::Bool(true),
                    body: Vec::new(),
                });
                continue;
            }
            if let Some(pos) = inner.find(":= range ") {
                let lhs = inner[..pos].trim();
                let iter = parse_expr(inner[pos + 9..].trim())?;
                if let Some(value) = lhs.strip_prefix("_,") {
                    frames.push(Frame::ForEach {
                        value: value.trim().to_string(),
                        iter,
                        body: Vec::new(),
                    });
                    continue;
                }
                return Err(GoParseError(format!("line {}: index-bearing range loops are deferred to preserve Go's zero-based index semantics", line_no + 1)));
            }
            let clauses = split_top_level(inner, ';');
            if clauses.len() == 3 {
                if clauses[0].trim().is_empty()
                    || clauses[1].trim().is_empty()
                    || clauses[2].trim().is_empty()
                {
                    return Err(GoParseError(format!(
                        "line {}: partial classic for clauses are not yet native",
                        line_no + 1
                    )));
                }
                let init = parse_inline_stmt(&clauses[0])?;
                push_stmt(&mut frames, init)?;
                let cond = parse_expr(&clauses[1])?;
                let post = parse_inline_stmt(&clauses[2])?;
                frames.push(Frame::ForClassic {
                    cond,
                    body: Vec::new(),
                    post: Box::new(post),
                });
                continue;
            }
            frames.push(Frame::While {
                cond: parse_expr(inner)?,
                body: Vec::new(),
            });
            continue;
        }

        if s.starts_with("defer ") || s.starts_with("go ") || s == "fallthrough" {
            return Err(GoParseError(format!(
                "line {}: unsupported Go control syntax: {s}",
                line_no + 1
            )));
        }

        s = s.trim_end_matches(';').trim();
        if s == "break" {
            push_stmt(&mut frames, Stmt::Break)?;
            continue;
        }
        if s == "continue" {
            push_stmt(&mut frames, Stmt::Continue)?;
            continue;
        }
        if s == "return" {
            push_stmt(&mut frames, Stmt::Return(None))?;
            continue;
        }
        if let Some(v) = s.strip_prefix("return ") {
            push_stmt(&mut frames, Stmt::Return(Some(parse_expr(v)?)))?;
            continue;
        }

        for (prefix, newline) in [("fmt.Println(", true), ("fmt.Print(", false)] {
            if let Some(inner) = s.strip_prefix(prefix).and_then(|v| v.strip_suffix(')')) {
                let mut args = Vec::new();
                if !inner.trim().is_empty() {
                    for a in split_top_level(inner, ',') {
                        args.push(parse_expr(&a)?);
                    }
                }
                push_stmt(&mut frames, Stmt::Print { newline, args })?;
                s = "";
                break;
            }
        }
        if s.is_empty() {
            continue;
        }

        if let Some(pos) = s.find(":=") {
            let names = split_top_level(&s[..pos], ',');
            let value = parse_expr(&s[pos + 2..])?;
            if names.len() > 1 {
                push_stmt(
                    &mut frames,
                    Stmt::MultiLet {
                        names,
                        value,
                        mutable: true,
                    },
                )?;
            } else {
                push_stmt(
                    &mut frames,
                    Stmt::Let {
                        name: names[0].clone(),
                        ty: None,
                        value,
                        mutable: true,
                    },
                )?;
            }
            continue;
        }
        if let Some(rest) = s.strip_prefix("var ") {
            let mut parts = rest
                .splitn(3, char::is_whitespace)
                .filter(|x| !x.is_empty());
            let name = parts
                .next()
                .ok_or_else(|| GoParseError("malformed var".into()))?;
            let ty_s = parts
                .next()
                .ok_or_else(|| GoParseError("typed var requires type".into()))?;
            let tail = parts.next().unwrap_or("").trim();
            let declared = go_type(ty_s);
            let value = if let Some(stripped) = tail.strip_prefix('=') {
                parse_expr(stripped.trim())?
            } else {
                zero_expr(&declared).ok_or_else(|| {
                    GoParseError(format!(
                        "line {}: zero value not yet native for type {ty_s}",
                        line_no + 1
                    ))
                })?
            };
            push_stmt(
                &mut frames,
                Stmt::Let {
                    name: name.into(),
                    ty: Some(declared),
                    value,
                    mutable: true,
                },
            )?;
            continue;
        }
        if let Some(stripped) = s.strip_suffix("++") {
            push_stmt(
                &mut frames,
                Stmt::Assign {
                    target: stripped.trim().into(),
                    op: "+=".into(),
                    value: Expr::Int("1".into()),
                },
            )?;
            continue;
        }
        if let Some(stripped) = s.strip_suffix("--") {
            push_stmt(
                &mut frames,
                Stmt::Assign {
                    target: stripped.trim().into(),
                    op: "-=".into(),
                    value: Expr::Int("1".into()),
                },
            )?;
            continue;
        }

        let mut assigned = false;
        for op in ["+=", "-=", "*=", "/=", "="] {
            if let Some(pos) = s.find(op) {
                if op == "="
                    && (s.contains("==")
                        || s.contains("!=")
                        || s.contains("<=")
                        || s.contains(">="))
                {
                    continue;
                }
                let targets = split_top_level(&s[..pos], ',');
                if targets.len() > 1 {
                    if op != "=" {
                        return Err(GoParseError(format!(
                            "line {}: compound tuple assignment unsupported",
                            line_no + 1
                        )));
                    }
                    push_stmt(
                        &mut frames,
                        Stmt::MultiAssign {
                            targets,
                            value: parse_expr(&s[pos + 1..])?,
                        },
                    )?;
                } else {
                    if targets[0].contains('[') {
                        match parse_expr(targets[0].trim())? {
                            Expr::Index {
                                value: collection,
                                index,
                            } => {
                                push_stmt(
                                    &mut frames,
                                    Stmt::IndexAssign {
                                        collection: *collection,
                                        index: *index,
                                        op: op.into(),
                                        value: parse_expr(&s[pos + op.len()..])?,
                                        map_value_type: None,
                                    },
                                )?;
                            }
                            _ => {
                                return Err(GoParseError(format!(
                                    "line {}: only direct slice/map indexed assignment is native",
                                    line_no + 1
                                )));
                            }
                        }
                    } else {
                        push_stmt(
                            &mut frames,
                            Stmt::Assign {
                                target: targets[0].clone(),
                                op: op.into(),
                                value: parse_expr(&s[pos + op.len()..])?,
                            },
                        )?;
                    }
                }
                assigned = true;
                break;
            }
        }
        if assigned {
            continue;
        }
        push_stmt(&mut frames, Stmt::Expr(parse_expr(s)?))?;
    }

    if struct_builder.is_some() {
        return Err(GoParseError("unclosed struct declaration".into()));
    }
    if !frames.is_empty() {
        return Err(GoParseError("unbalanced Go blocks at end of file".into()));
    }
    if program.functions.is_empty() {
        return Err(GoParseError("no Go functions found".into()));
    }
    annotate_program(&mut program);
    Ok(program)
}
