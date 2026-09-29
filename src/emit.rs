use crate::ir::{Expr, Function, Program, Stmt, Type};
use std::collections::{BTreeMap, BTreeSet};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct EmitError(pub String);
impl std::fmt::Display for EmitError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.0)
    }
}
impl std::error::Error for EmitError {}

fn rust_type(t: &Type) -> String {
    match t {
        Type::Unit => "()".into(),
        Type::Bool => "bool".into(),
        Type::String => "String".into(),
        Type::Int => "isize".into(),
        Type::Int8 => "i8".into(),
        Type::Int16 => "i16".into(),
        Type::Int32 => "i32".into(),
        Type::Int64 => "i64".into(),
        Type::UInt => "usize".into(),
        Type::UInt8 => "u8".into(),
        Type::UInt16 => "u16".into(),
        Type::UInt32 => "u32".into(),
        Type::UInt64 => "u64".into(),
        Type::Float32 => "f32".into(),
        Type::Float64 => "f64".into(),
        Type::Slice(x) => format!("Vec<{}>", rust_type(x)),
        Type::Map(k, v) => format!(
            "std::collections::BTreeMap<{}, {}>",
            rust_type(k),
            rust_type(v)
        ),
        Type::Tuple(items) => format!(
            "({})",
            items.iter().map(rust_type).collect::<Vec<_>>().join(", ")
        ),
        Type::Named(n) => n.clone(),
    }
}
fn julia_type(t: &Type) -> String {
    match t {
        Type::Unit => "Nothing".into(),
        Type::Bool => "Bool".into(),
        Type::String => "String".into(),
        Type::Int => "Int".into(),
        Type::Int8 => "Int8".into(),
        Type::Int16 => "Int16".into(),
        Type::Int32 => "Int32".into(),
        Type::Int64 => "Int64".into(),
        Type::UInt => "UInt".into(),
        Type::UInt8 => "UInt8".into(),
        Type::UInt16 => "UInt16".into(),
        Type::UInt32 => "UInt32".into(),
        Type::UInt64 => "UInt64".into(),
        Type::Float32 => "Float32".into(),
        Type::Float64 => "Float64".into(),
        Type::Slice(x) => format!("Vector{{{}}}", julia_type(x)),
        Type::Map(k, v) => format!("Dict{{{}, {}}}", julia_type(k), julia_type(v)),
        Type::Tuple(items) => format!(
            "Tuple{{{}}}",
            items.iter().map(julia_type).collect::<Vec<_>>().join(", ")
        ),
        Type::Named(n) => n.clone(),
    }
}
fn c_type(t: &Type) -> String {
    match t {
        Type::Unit => "void".into(),
        Type::Bool => "bool".into(),
        Type::String => "const char*".into(),
        Type::Int => "ptrdiff_t".into(),
        Type::Int8 => "int8_t".into(),
        Type::Int16 => "int16_t".into(),
        Type::Int32 => "int32_t".into(),
        Type::Int64 => "int64_t".into(),
        Type::UInt => "size_t".into(),
        Type::UInt8 => "uint8_t".into(),
        Type::UInt16 => "uint16_t".into(),
        Type::UInt32 => "uint32_t".into(),
        Type::UInt64 => "uint64_t".into(),
        Type::Float32 => "float".into(),
        Type::Float64 => "double".into(),
        Type::Slice(x) => format!("{}*", c_type(x)),
        Type::Map(_, _) => "/* unsupported map */ void".into(),
        Type::Tuple(_) => "/* unsupported tuple */ void".into(),
        Type::Named(n) => n.clone(),
    }
}
fn quote(s: &str) -> String {
    format!(
        "\"{}\"",
        s.replace('\\', "\\\\")
            .replace('"', "\\\"")
            .replace('\n', "\\n")
    )
}

fn target_zero(t: &Type, target: &str) -> Result<String, EmitError> {
    Ok(match (target, t) {
        ("rust", Type::Bool) => "false".into(),
        ("rust", Type::String) => "String::new()".into(),
        ("rust", Type::Float32 | Type::Float64) => "0.0".into(),
        (
            "rust",
            Type::Int
            | Type::Int8
            | Type::Int16
            | Type::Int32
            | Type::Int64
            | Type::UInt
            | Type::UInt8
            | Type::UInt16
            | Type::UInt32
            | Type::UInt64,
        ) => "0".into(),
        ("rust", Type::Slice(_)) => "Vec::new()".into(),
        ("rust", Type::Map(_, _)) => "std::collections::BTreeMap::new()".into(),
        ("rust", Type::Named(_)) => "Default::default()".into(),
        ("python", Type::Bool) => "False".into(),
        ("python", Type::String) => "\"\"".into(),
        ("python", Type::Float32 | Type::Float64) => "0.0".into(),
        (
            "python",
            Type::Int
            | Type::Int8
            | Type::Int16
            | Type::Int32
            | Type::Int64
            | Type::UInt
            | Type::UInt8
            | Type::UInt16
            | Type::UInt32
            | Type::UInt64,
        ) => "0".into(),
        ("python", Type::Slice(_)) => "[]".into(),
        ("python", Type::Map(_, _)) => "{}".into(),
        ("julia", Type::Bool) => "false".into(),
        ("julia", Type::String) => "\"\"".into(),
        ("julia", Type::Float32) => "0.0f0".into(),
        ("julia", Type::Float64) => "0.0".into(),
        (
            "julia",
            Type::Int
            | Type::Int8
            | Type::Int16
            | Type::Int32
            | Type::Int64
            | Type::UInt
            | Type::UInt8
            | Type::UInt16
            | Type::UInt32
            | Type::UInt64,
        ) => format!("zero({})", julia_type(t)),
        ("julia", Type::Slice(inner)) => format!("{}[]", julia_type(inner)),
        ("julia", Type::Map(k, v)) => format!("Dict{{{}, {}}}()", julia_type(k), julia_type(v)),
        _ => {
            return Err(EmitError(format!(
                "cannot preserve Go zero value for {t:?} on {target}"
            )))
        }
    })
}

fn expr(e: &Expr, target: &str) -> Result<String, EmitError> {
    Ok(match e {
        Expr::Ident(x) => x.clone(),
        Expr::Int(x) | Expr::Float(x) => x.clone(),
        Expr::String(x) => {
            if target == "rust" {
                format!("{}.to_string()", quote(x))
            } else {
                quote(x)
            }
        }
        Expr::Bool(v) => match target {
            "python" => {
                if *v {
                    "True".into()
                } else {
                    "False".into()
                }
            }
            _ => {
                if *v {
                    "true".into()
                } else {
                    "false".into()
                }
            }
        },
        Expr::Nil => match target {
            "rust" | "python" => "None".into(),
            "julia" => "nothing".into(),
            "go" => "nil".into(),
            "c" | "cpp" => "NULL".into(),
            _ => "null".into(),
        },
        Expr::Unary { op, value } => {
            let mapped = if target == "python" && op == "!" {
                "not "
            } else {
                op.as_str()
            };
            format!("{}{}", mapped, expr(value, target)?)
        }
        Expr::Binary { left, op, right } => {
            let mapped = if target == "python" {
                match op.as_str() {
                    "&&" => "and",
                    "||" => "or",
                    other => other,
                }
            } else {
                op.as_str()
            };
            format!(
                "{} {} {}",
                expr(left, target)?,
                mapped,
                expr(right, target)?
            )
        }
        Expr::Call { function, args } => {
            if function.contains('.') {
                return Err(EmitError(format!(
                    "qualified/library call needs a semantic adapter for {target}: {function}"
                )));
            }
            let rendered = args
                .iter()
                .map(|x| expr(x, target))
                .collect::<Result<Vec<_>, _>>()?;
            if function == "__go_slice_values" && rendered.len() == 1 {
                match target {
                    "rust" => format!("{}.iter().cloned()", rendered[0]),
                    "python" | "julia" => rendered[0].clone(),
                    "c" | "cpp" => {
                        return Err(EmitError(
                            "range over slices is not yet native for C/C++".into(),
                        ))
                    }
                    _ => unreachable!(),
                }
            } else if function == "__go_map_values" && rendered.len() == 1 {
                match target {
                    "rust" => format!("{}.values().cloned()", rendered[0]),
                    "python" => format!("{}.values()", rendered[0]),
                    "julia" => format!("values({})", rendered[0]),
                    "c" | "cpp" => {
                        return Err(EmitError(
                            "range over maps is not yet native for C/C++".into(),
                        ))
                    }
                    _ => unreachable!(),
                }
            } else if function == "len" && rendered.len() == 1 {
                match target {
                    "rust" => format!("({}.len() as isize)", rendered[0]),
                    "python" => format!("len({})", rendered[0]),
                    "julia" => format!("length({})", rendered[0]),
                    "c" | "cpp" => {
                        return Err(EmitError(
                            "len lowering for C/C++ requires slice length metadata".into(),
                        ))
                    }
                    _ => format!("len({})", rendered[0]),
                }
            } else if function == "append" && rendered.len() == 2 {
                match target {
                    "rust" => format!(
                        "{{ let mut __v = {}; __v.push({}); __v }}",
                        rendered[0], rendered[1]
                    ),
                    "python" => format!("{} + [{}]", rendered[0], rendered[1]),
                    "julia" => format!("vcat({}, [{}])", rendered[0], rendered[1]),
                    "c" | "cpp" => {
                        return Err(EmitError(
                            "append lowering for C/C++ requires capacity/ownership metadata".into(),
                        ))
                    }
                    _ => {
                        return Err(EmitError(format!(
                            "append lowering not implemented for {target}"
                        )))
                    }
                }
            } else {
                format!("{}({})", function, rendered.join(", "))
            }
        }
        Expr::Index { value, index } => {
            let v = expr(value, target)?;
            let i = expr(index, target)?;
            if target == "julia" {
                format!("{v}[({i}) + 1]")
            } else if target == "rust" {
                format!("{v}[({i}) as usize]")
            } else {
                format!("{v}[{i}]")
            }
        }
        Expr::Array { items, .. } => {
            let values = items
                .iter()
                .map(|x| expr(x, target))
                .collect::<Result<Vec<_>, _>>()?
                .join(", ");
            match target {
                "rust" => format!("vec![{values}]"),
                "python" | "julia" => format!("[{values}]"),
                "c" | "cpp" => {
                    return Err(EmitError(
                        "slice literal lowering for C/C++ requires storage/lifetime metadata"
                            .into(),
                    ))
                }
                _ => format!("[{values}]"),
            }
        }
        Expr::Map { entries, .. } => {
            if matches!(target, "c" | "cpp") {
                return Err(EmitError(
                    "map literals are not yet native for C/C++".into(),
                ));
            }
            let pairs = entries
                .iter()
                .map(|(k, v)| Ok((expr(k, target)?, expr(v, target)?)))
                .collect::<Result<Vec<(String, String)>, EmitError>>()?;
            match target {
                "rust" => format!(
                    "std::collections::BTreeMap::from([{}])",
                    pairs
                        .iter()
                        .map(|(k, v)| format!("({k}, {v})"))
                        .collect::<Vec<_>>()
                        .join(", ")
                ),
                "python" => format!(
                    "{{{}}}",
                    pairs
                        .iter()
                        .map(|(k, v)| format!("{k}: {v}"))
                        .collect::<Vec<_>>()
                        .join(", ")
                ),
                "julia" => format!(
                    "Dict({})",
                    pairs
                        .iter()
                        .map(|(k, v)| format!("{k} => {v}"))
                        .collect::<Vec<_>>()
                        .join(", ")
                ),
                _ => unreachable!(),
            }
        }
        Expr::MapIndex {
            value,
            index,
            value_type,
        } => {
            let v = expr(value, target)?;
            let i = expr(index, target)?;
            let zero = target_zero(value_type, target)?;
            match target {
                "rust" => format!("{v}.get(&{i}).cloned().unwrap_or_else(|| {zero})"),
                "python" => format!("{v}.get({i}, {zero})"),
                "julia" => format!("get({v}, {i}, {zero})"),
                "c" | "cpp" => {
                    return Err(EmitError("map indexing is not yet native for C/C++".into()))
                }
                _ => unreachable!(),
            }
        }
        Expr::MapLookupOk {
            value,
            index,
            value_type,
        } => {
            let v = expr(value, target)?;
            let i = expr(index, target)?;
            let zero = target_zero(value_type, target)?;
            match target {
                "rust" => format!(
                    "{{ let __code_transpiler_key = {i}; match {v}.get(&__code_transpiler_key) {{ Some(__value) => (__value.clone(), true), None => ({zero}, false) }} }}"
                ),
                "python" => format!(
                    "(lambda __code_transpiler_key: ({v}.get(__code_transpiler_key, {zero}), __code_transpiler_key in {v}))({i})"
                ),
                "julia" => format!(
                    "let __code_transpiler_key = {i}; (get({v}, __code_transpiler_key, {zero}), haskey({v}, __code_transpiler_key)); end"
                ),
                "c" | "cpp" => {
                    return Err(EmitError("map comma-ok lookup is not yet native for C/C++".into()))
                }
                _ => unreachable!(),
            }
        }
        Expr::Tuple(items) => {
            if matches!(target, "c" | "cpp") {
                return Err(EmitError(
                    "multiple-return tuple expressions are not yet native for C/C++".into(),
                ));
            }
            let values = items
                .iter()
                .map(|x| expr(x, target))
                .collect::<Result<Vec<_>, _>>()?;
            if values.len() == 1 {
                format!("({},)", values[0])
            } else {
                format!("({})", values.join(", "))
            }
        }
        Expr::StructLiteral {
            ty,
            fields,
            positional,
        } => {
            if matches!(target, "c" | "cpp") {
                return Err(EmitError(
                    "struct composite literals are not yet emitted for C/C++".into(),
                ));
            }
            if !fields.is_empty() {
                let rendered = fields
                    .iter()
                    .map(|(name, value)| {
                        expr(value, target).map(|v| match target {
                            "rust" => format!("{name}: {v}"),
                            "python" | "julia" => format!("{name}={v}"),
                            _ => format!("{name}={v}"),
                        })
                    })
                    .collect::<Result<Vec<_>, _>>()?;
                match target {
                    "rust" => format!("{ty} {{ {} }}", rendered.join(", ")),
                    "python" => format!("{ty}({})", rendered.join(", ")),
                    "julia" => format!("{ty}(; {})", rendered.join(", ")),
                    _ => unreachable!(),
                }
            } else {
                let rendered = positional
                    .iter()
                    .map(|x| expr(x, target))
                    .collect::<Result<Vec<_>, _>>()?;
                match target {
                    "rust" => return Err(EmitError("positional Go struct literals need field-order resolution before Rust emission".into())),
                    "python" | "julia" => format!("{ty}({})", rendered.join(", ")),
                    _ => unreachable!(),
                }
            }
        }
        Expr::Raw(x) => {
            return Err(EmitError(format!(
                "expression requires manual lowering for {target}: {x}"
            )))
        }
    })
}

fn rust_print(args: &[Expr], newline: bool) -> Result<String, EmitError> {
    if args.is_empty() {
        return Ok(if newline {
            "println!();".into()
        } else {
            "print!(\"\");".into()
        });
    }
    if args.len() == 1 {
        if let Expr::String(s) = &args[0] {
            return Ok(format!(
                "{}!({});",
                if newline { "println" } else { "print" },
                quote(s)
            ));
        }
        return Ok(format!(
            "{}!(\"{{}}\", {});",
            if newline { "println" } else { "print" },
            expr(&args[0], "rust")?
        ));
    }
    let fmt = std::iter::repeat("{}")
        .take(args.len())
        .collect::<Vec<_>>()
        .join(" ");
    let vals = args
        .iter()
        .map(|x| expr(x, "rust"))
        .collect::<Result<Vec<_>, _>>()?
        .join(", ");
    Ok(format!(
        "{}!({}, {});",
        if newline { "println" } else { "print" },
        quote(&fmt),
        vals
    ))
}

fn assignment_root(target: &str) -> &str {
    target.split(['.', '[']).next().unwrap_or(target).trim()
}

fn collect_mutated(stmts: &[Stmt], out: &mut BTreeSet<String>) {
    for stmt in stmts {
        match stmt {
            Stmt::Assign { target, .. } => {
                out.insert(assignment_root(target).to_string());
            }
            Stmt::IndexAssign {
                collection: Expr::Ident(name),
                ..
            } => {
                out.insert(name.clone());
            }
            Stmt::MultiAssign { targets, .. } => {
                for target in targets {
                    out.insert(assignment_root(target).to_string());
                }
            }
            Stmt::If {
                then_body,
                else_body,
                ..
            } => {
                collect_mutated(then_body, out);
                collect_mutated(else_body, out);
            }
            Stmt::While { body, .. } | Stmt::ForEach { body, .. } => collect_mutated(body, out),
            Stmt::Switch { cases, default, .. } => {
                for case in cases {
                    collect_mutated(&case.body, out);
                }
                collect_mutated(default, out);
            }
            _ => {}
        }
    }
}

fn emit_rust_stmt(
    s: &Stmt,
    indent: usize,
    out: &mut String,
    mutable_names: &BTreeSet<String>,
) -> Result<(), EmitError> {
    let p = "    ".repeat(indent);
    match s {
        Stmt::Let {
            name,
            ty,
            value,
            mutable,
        } => {
            let mt = if *mutable && mutable_names.contains(name) {
                "mut "
            } else {
                ""
            };
            let inferred_array = match value {
                Expr::Array {
                    element_type: Some(t),
                    ..
                } => Some(Type::Slice(t.clone())),
                _ => None,
            };
            let annotation = ty.as_ref().or(inferred_array.as_ref());
            let ann = annotation
                .map(|t| format!(": {}", rust_type(t)))
                .unwrap_or_default();
            out.push_str(&format!(
                "{p}let {mt}{name}{ann} = {};\n",
                expr(value, "rust")?
            ));
        }
        Stmt::MultiLet {
            names,
            value,
            mutable,
        } => {
            let pattern = names
                .iter()
                .map(|n| {
                    if *mutable && mutable_names.contains(n) {
                        format!("mut {n}")
                    } else {
                        n.clone()
                    }
                })
                .collect::<Vec<_>>()
                .join(", ");
            out.push_str(&format!("{p}let ({pattern}) = {};\n", expr(value, "rust")?));
        }
        Stmt::Assign { target, op, value } => {
            out.push_str(&format!("{p}{target} {op} {};\n", expr(value, "rust")?))
        }
        Stmt::IndexAssign {
            collection,
            index,
            op,
            value,
            map_value_type,
        } => {
            let c = expr(collection, "rust")?;
            let i = expr(index, "rust")?;
            let v = expr(value, "rust")?;
            if let Some(value_type) = map_value_type {
                if op == "=" {
                    out.push_str(&format!("{p}{c}.insert({i}, {v});\n"));
                } else {
                    let zero = target_zero(value_type, "rust")?;
                    out.push_str(&format!(
                        "{p}*{c}.entry({i}).or_insert_with(|| {zero}) {op} {v};\n"
                    ));
                }
            } else {
                out.push_str(&format!("{p}{c}[({i}) as usize] {op} {v};\n"));
            }
        }
        Stmt::MultiAssign { targets, value } => {
            out.push_str(&format!(
                "{p}({}) = {};\n",
                targets.join(", "),
                expr(value, "rust")?
            ));
        }
        Stmt::Expr(e) => out.push_str(&format!("{p}{};\n", expr(e, "rust")?)),
        Stmt::Print { newline, args } => {
            out.push_str(&format!("{p}{}\n", rust_print(args, *newline)?))
        }
        Stmt::Return(v) => {
            if let Some(v) = v {
                out.push_str(&format!("{p}return {};\n", expr(v, "rust")?));
            } else {
                out.push_str(&format!("{p}return;\n"));
            }
        }
        Stmt::If {
            cond,
            then_body,
            else_body,
        } => {
            out.push_str(&format!("{p}if {} {{\n", expr(cond, "rust")?));
            for x in then_body {
                emit_rust_stmt(x, indent + 1, out, mutable_names)?;
            }
            if else_body.is_empty() {
                out.push_str(&format!("{p}}}\n"));
            } else {
                out.push_str(&format!("{p}}} else {{\n"));
                for x in else_body {
                    emit_rust_stmt(x, indent + 1, out, mutable_names)?;
                }
                out.push_str(&format!("{p}}}\n"));
            }
        }
        Stmt::While { cond, body } => {
            out.push_str(&format!("{p}while {} {{\n", expr(cond, "rust")?));
            for x in body {
                emit_rust_stmt(x, indent + 1, out, mutable_names)?;
            }
            out.push_str(&format!("{p}}}\n"));
        }
        Stmt::ForEach { value, iter, body } => {
            out.push_str(&format!("{p}for {value} in {} {{\n", expr(iter, "rust")?));
            for x in body {
                emit_rust_stmt(x, indent + 1, out, mutable_names)?;
            }
            out.push_str(&format!("{p}}}\n"));
        }
        Stmt::Switch {
            value,
            cases,
            default,
        } => {
            out.push_str(&format!("{p}{{\n"));
            if let Some(v) = value {
                out.push_str(&format!(
                    "{p}    let __code_transpiler_switch_value = {};\n",
                    expr(v, "rust")?
                ));
            }
            for (i, case) in cases.iter().enumerate() {
                let tests = case
                    .values
                    .iter()
                    .map(|v| {
                        expr(v, "rust").map(|rv| {
                            if value.is_some() {
                                format!("__code_transpiler_switch_value == {rv}")
                            } else {
                                rv
                            }
                        })
                    })
                    .collect::<Result<Vec<_>, _>>()?;
                let head = if i == 0 { "if" } else { "else if" };
                out.push_str(&format!("{p}    {head} {} {{\n", tests.join(" || ")));
                for x in &case.body {
                    emit_rust_stmt(x, indent + 2, out, mutable_names)?;
                }
                out.push_str(&format!("{p}    }}"));
                if i + 1 < cases.len() || !default.is_empty() {
                    out.push(' ');
                } else {
                    out.push('\n');
                }
            }
            if !default.is_empty() {
                if cases.is_empty() {
                    for x in default {
                        emit_rust_stmt(x, indent + 1, out, mutable_names)?;
                    }
                } else {
                    out.push_str("else {\n");
                    for x in default {
                        emit_rust_stmt(x, indent + 2, out, mutable_names)?;
                    }
                    out.push_str(&format!("{p}    }}\n"));
                }
            }
            out.push_str(&format!("{p}}}\n"));
        }
        Stmt::Break => out.push_str(&format!("{p}break;\n")),
        Stmt::Continue => out.push_str(&format!("{p}continue;\n")),
    }
    Ok(())
}
fn emit_rust_fn(f: &Function, out: &mut String) -> Result<(), EmitError> {
    let mut mutable_names = BTreeSet::new();
    collect_mutated(&f.body, &mut mutable_names);
    let ps = f
        .params
        .iter()
        .map(|p| {
            let mt = if mutable_names.contains(&p.name) {
                "mut "
            } else {
                ""
            };
            format!("{mt}{}: {}", p.name, rust_type(&p.ty))
        })
        .collect::<Vec<_>>()
        .join(", ");
    let rt = f
        .return_type
        .as_ref()
        .map(|x| format!(" -> {}", rust_type(x)))
        .unwrap_or_default();
    out.push_str("#[allow(non_snake_case)]\n");
    out.push_str(&format!("fn {}({}){} {{\n", f.name, ps, rt));
    for stmt in &f.body {
        emit_rust_stmt(stmt, 1, out, &mutable_names)?;
    }
    out.push_str("}\n\n");
    Ok(())
}

fn emit_python_stmt(s: &Stmt, indent: usize, out: &mut String) -> Result<(), EmitError> {
    let p = "    ".repeat(indent);
    match s {
        Stmt::Let { name, value, .. } => {
            out.push_str(&format!("{p}{name} = {}\n", expr(value, "python")?))
        }
        Stmt::MultiLet { names, value, .. } => {
            out.push_str(&format!(
                "{p}{} = {}\n",
                names.join(", "),
                expr(value, "python")?
            ));
        }
        Stmt::Assign { target, op, value } => {
            out.push_str(&format!("{p}{target} {op} {}\n", expr(value, "python")?))
        }
        Stmt::IndexAssign {
            collection,
            index,
            op,
            value,
            map_value_type,
        } => {
            let c = expr(collection, "python")?;
            let i = expr(index, "python")?;
            let v = expr(value, "python")?;
            if let Some(value_type) = map_value_type {
                if op == "=" {
                    out.push_str(&format!("{p}{c}[{i}] = {v}\n"));
                } else {
                    let zero = target_zero(value_type, "python")?;
                    let base = op.trim_end_matches('=');
                    out.push_str(&format!("{p}{c}[{i}] = {c}.get({i}, {zero}) {base} {v}\n"));
                }
            } else {
                out.push_str(&format!("{p}{c}[{i}] {op} {v}\n"));
            }
        }
        Stmt::MultiAssign { targets, value } => {
            out.push_str(&format!(
                "{p}{} = {}\n",
                targets.join(", "),
                expr(value, "python")?
            ));
        }
        Stmt::Expr(e) => out.push_str(&format!("{p}{}\n", expr(e, "python")?)),
        Stmt::Print { newline, args } => {
            let vals = args
                .iter()
                .map(|x| expr(x, "python"))
                .collect::<Result<Vec<_>, _>>()?
                .join(", ");
            let end = if *newline {
                ""
            } else if vals.is_empty() {
                "end=\"\""
            } else {
                ", end=\"\""
            };
            out.push_str(&format!("{p}print({vals}{end})\n"));
        }
        Stmt::Return(v) => {
            let tail = match v {
                Some(x) => format!(" {}", expr(x, "python")?),
                None => String::new(),
            };
            out.push_str(&format!("{p}return{tail}\n"));
        }
        Stmt::If {
            cond,
            then_body,
            else_body,
        } => {
            out.push_str(&format!("{p}if {}:\n", expr(cond, "python")?));
            if then_body.is_empty() {
                out.push_str(&format!("{p}    pass\n"));
            }
            for x in then_body {
                emit_python_stmt(x, indent + 1, out)?;
            }
            if !else_body.is_empty() {
                out.push_str(&format!("{p}else:\n"));
                for x in else_body {
                    emit_python_stmt(x, indent + 1, out)?;
                }
            }
        }
        Stmt::While { cond, body } => {
            out.push_str(&format!("{p}while {}:\n", expr(cond, "python")?));
            for x in body {
                emit_python_stmt(x, indent + 1, out)?;
            }
        }
        Stmt::ForEach { value, iter, body } => {
            out.push_str(&format!("{p}for {value} in {}:\n", expr(iter, "python")?));
            for x in body {
                emit_python_stmt(x, indent + 1, out)?;
            }
        }
        Stmt::Switch {
            value,
            cases,
            default,
        } => {
            if let Some(v) = value {
                out.push_str(&format!(
                    "{p}__code_transpiler_switch_value = {}\n",
                    expr(v, "python")?
                ));
            }
            if cases.is_empty() {
                for x in default {
                    emit_python_stmt(x, indent, out)?;
                }
            } else {
                for (i, case) in cases.iter().enumerate() {
                    let tests = case
                        .values
                        .iter()
                        .map(|v| {
                            expr(v, "python").map(|rv| {
                                if value.is_some() {
                                    format!("__code_transpiler_switch_value == {rv}")
                                } else {
                                    rv
                                }
                            })
                        })
                        .collect::<Result<Vec<_>, _>>()?;
                    out.push_str(&format!(
                        "{p}{} {}:\n",
                        if i == 0 { "if" } else { "elif" },
                        tests.join(" or ")
                    ));
                    if case.body.is_empty() {
                        out.push_str(&format!("{p}    pass\n"));
                    }
                    for x in &case.body {
                        emit_python_stmt(x, indent + 1, out)?;
                    }
                }
                if !default.is_empty() {
                    out.push_str(&format!("{p}else:\n"));
                    for x in default {
                        emit_python_stmt(x, indent + 1, out)?;
                    }
                }
            }
        }
        Stmt::Break => out.push_str(&format!("{p}break\n")),
        Stmt::Continue => out.push_str(&format!("{p}continue\n")),
    }
    Ok(())
}

fn emit_julia_stmt(s: &Stmt, indent: usize, out: &mut String) -> Result<(), EmitError> {
    let p = "    ".repeat(indent);
    match s {
        Stmt::Let {
            name, ty, value, ..
        } => {
            let ann = ty
                .as_ref()
                .map(|t| format!("::{}", julia_type(t)))
                .unwrap_or_default();
            out.push_str(&format!("{p}{name}{ann} = {}\n", expr(value, "julia")?));
        }
        Stmt::MultiLet { names, value, .. } => {
            out.push_str(&format!(
                "{p}{} = {}\n",
                names.join(", "),
                expr(value, "julia")?
            ));
        }
        Stmt::Assign { target, op, value } => {
            out.push_str(&format!("{p}{target} {op} {}\n", expr(value, "julia")?))
        }
        Stmt::IndexAssign {
            collection,
            index,
            op,
            value,
            map_value_type,
        } => {
            let c = expr(collection, "julia")?;
            let i = expr(index, "julia")?;
            let v = expr(value, "julia")?;
            if let Some(value_type) = map_value_type {
                if op == "=" {
                    out.push_str(&format!("{p}{c}[{i}] = {v}\n"));
                } else {
                    let zero = target_zero(value_type, "julia")?;
                    let base = op.trim_end_matches('=');
                    out.push_str(&format!("{p}{c}[{i}] = get({c}, {i}, {zero}) {base} {v}\n"));
                }
            } else {
                out.push_str(&format!("{p}{c}[({i}) + 1] {op} {v}\n"));
            }
        }
        Stmt::MultiAssign { targets, value } => {
            out.push_str(&format!(
                "{p}{} = {}\n",
                targets.join(", "),
                expr(value, "julia")?
            ));
        }
        Stmt::Expr(e) => out.push_str(&format!("{p}{}\n", expr(e, "julia")?)),
        Stmt::Print { newline, args } => {
            let vals = args
                .iter()
                .map(|x| expr(x, "julia"))
                .collect::<Result<Vec<_>, _>>()?
                .join(", ");
            out.push_str(&format!(
                "{p}{}({vals})\n",
                if *newline { "println" } else { "print" }
            ));
        }
        Stmt::Return(v) => {
            let tail = match v {
                Some(x) => format!(" {}", expr(x, "julia")?),
                None => String::new(),
            };
            out.push_str(&format!("{p}return{tail}\n"));
        }
        Stmt::If {
            cond,
            then_body,
            else_body,
        } => {
            out.push_str(&format!("{p}if {}\n", expr(cond, "julia")?));
            for x in then_body {
                emit_julia_stmt(x, indent + 1, out)?;
            }
            if !else_body.is_empty() {
                out.push_str(&format!("{p}else\n"));
                for x in else_body {
                    emit_julia_stmt(x, indent + 1, out)?;
                }
            }
            out.push_str(&format!("{p}end\n"));
        }
        Stmt::While { cond, body } => {
            out.push_str(&format!("{p}while {}\n", expr(cond, "julia")?));
            for x in body {
                emit_julia_stmt(x, indent + 1, out)?;
            }
            out.push_str(&format!("{p}end\n"));
        }
        Stmt::ForEach { value, iter, body } => {
            out.push_str(&format!("{p}for {value} in {}\n", expr(iter, "julia")?));
            for x in body {
                emit_julia_stmt(x, indent + 1, out)?;
            }
            out.push_str(&format!("{p}end\n"));
        }
        Stmt::Switch {
            value,
            cases,
            default,
        } => {
            if let Some(v) = value {
                out.push_str(&format!(
                    "{p}__code_transpiler_switch_value = {}\n",
                    expr(v, "julia")?
                ));
            }
            if cases.is_empty() {
                for x in default {
                    emit_julia_stmt(x, indent, out)?;
                }
            } else {
                for (i, case) in cases.iter().enumerate() {
                    let tests = case
                        .values
                        .iter()
                        .map(|v| {
                            expr(v, "julia").map(|rv| {
                                if value.is_some() {
                                    format!("__code_transpiler_switch_value == {rv}")
                                } else {
                                    rv
                                }
                            })
                        })
                        .collect::<Result<Vec<_>, _>>()?;
                    out.push_str(&format!(
                        "{p}{} {}\n",
                        if i == 0 { "if" } else { "elseif" },
                        tests.join(" || ")
                    ));
                    for x in &case.body {
                        emit_julia_stmt(x, indent + 1, out)?;
                    }
                }
                if !default.is_empty() {
                    out.push_str(&format!("{p}else\n"));
                    for x in default {
                        emit_julia_stmt(x, indent + 1, out)?;
                    }
                }
                out.push_str(&format!("{p}end\n"));
            }
        }
        Stmt::Break => out.push_str(&format!("{p}break\n")),
        Stmt::Continue => out.push_str(&format!("{p}continue\n")),
    }
    Ok(())
}

fn numeric_type(t: &Type) -> bool {
    matches!(
        t,
        Type::Int
            | Type::Int8
            | Type::Int16
            | Type::Int32
            | Type::Int64
            | Type::UInt
            | Type::UInt8
            | Type::UInt16
            | Type::UInt32
            | Type::UInt64
            | Type::Float32
            | Type::Float64
    )
}
fn infer_type(
    e: &Expr,
    env: &BTreeMap<String, Type>,
    functions: &BTreeMap<String, Option<Type>>,
) -> Option<Type> {
    match e {
        Expr::Int(_) => Some(Type::Int),
        Expr::Float(_) => Some(Type::Float64),
        Expr::String(_) => Some(Type::String),
        Expr::Bool(_) => Some(Type::Bool),
        Expr::Ident(n) => env.get(n).cloned(),
        Expr::Unary { value, .. } => infer_type(value, env, functions),
        Expr::Binary { left, op, right } => {
            if matches!(
                op.as_str(),
                "==" | "!=" | "<" | "<=" | ">" | ">=" | "&&" | "||"
            ) {
                Some(Type::Bool)
            } else {
                let a = infer_type(left, env, functions)?;
                let b = infer_type(right, env, functions)?;
                if a == b && numeric_type(&a) {
                    Some(a)
                } else {
                    None
                }
            }
        }
        Expr::Call { function, args } => {
            if function == "append" && args.len() == 2 {
                infer_type(&args[0], env, functions)
            } else {
                functions.get(function).cloned().flatten()
            }
        }
        Expr::Index { value, .. } => match infer_type(value, env, functions)? {
            Type::Slice(x) => Some(*x),
            _ => None,
        },
        Expr::Array {
            items,
            element_type,
        } => {
            if let Some(t) = element_type {
                return Some(Type::Slice(t.clone()));
            }
            let first = items.first()?;
            let t = infer_type(first, env, functions)?;
            if items
                .iter()
                .all(|x| infer_type(x, env, functions).as_ref() == Some(&t))
            {
                Some(Type::Slice(Box::new(t)))
            } else {
                None
            }
        }
        Expr::Map {
            key_type,
            value_type,
            ..
        } => Some(Type::Map(key_type.clone(), value_type.clone())),
        Expr::MapIndex { value_type, .. } => Some((**value_type).clone()),
        Expr::MapLookupOk { value_type, .. } => {
            Some(Type::Tuple(vec![(**value_type).clone(), Type::Bool]))
        }
        Expr::Tuple(items) => Some(Type::Tuple(
            items
                .iter()
                .map(|x| infer_type(x, env, functions))
                .collect::<Option<Vec<_>>>()?,
        )),
        Expr::StructLiteral { ty, .. } => Some(Type::Named(ty.clone())),
        Expr::Nil | Expr::Raw(_) => None,
    }
}
fn c_print_format(t: &Type) -> Option<&'static str> {
    match t {
        Type::String => Some("%s"),
        Type::Bool | Type::Int | Type::Int8 | Type::Int16 | Type::Int32 | Type::Int64 => {
            Some("%lld")
        }
        Type::UInt | Type::UInt8 | Type::UInt16 | Type::UInt32 | Type::UInt64 => Some("%llu"),
        Type::Float32 | Type::Float64 => Some("%g"),
        _ => None,
    }
}
fn c_print_cast(t: &Type, value: String) -> String {
    match t {
        Type::Bool | Type::Int | Type::Int8 | Type::Int16 | Type::Int32 | Type::Int64 => {
            format!("(long long)({value})")
        }
        Type::UInt | Type::UInt8 | Type::UInt16 | Type::UInt32 | Type::UInt64 => {
            format!("(unsigned long long)({value})")
        }
        Type::Float32 | Type::Float64 => format!("(double)({value})"),
        _ => value,
    }
}

fn emit_c_stmt(
    s: &Stmt,
    indent: usize,
    out: &mut String,
    cpp: bool,
    env: &mut BTreeMap<String, Type>,
    functions: &BTreeMap<String, Option<Type>>,
) -> Result<(), EmitError> {
    let p = "    ".repeat(indent);
    let target = if cpp { "cpp" } else { "c" };
    match s {
        Stmt::Let {
            name, ty, value, ..
        } => {
            let inferred = match ty {
                Some(t) => t.clone(),
                None => infer_type(value, env, functions).ok_or_else(|| {
                    EmitError(format!("cannot prove C/C++ type for local {name}"))
                })?,
            };
            let decl = if cpp && ty.is_none() {
                "auto".into()
            } else {
                c_type(&inferred)
            };
            out.push_str(&format!("{p}{decl} {name} = {};\n", expr(value, target)?));
            env.insert(name.clone(), inferred);
        }
        Stmt::MultiLet { .. } | Stmt::MultiAssign { .. } => {
            return Err(EmitError(
                "multiple-return destructuring is not yet native for C/C++".into(),
            ));
        }
        Stmt::Assign {
            target: lhs,
            op,
            value,
        } => out.push_str(&format!("{p}{lhs} {op} {};\n", expr(value, target)?)),
        Stmt::IndexAssign { .. } => {
            return Err(EmitError(
                "indexed mutation for C/C++ requires native storage metadata".into(),
            ));
        }
        Stmt::Expr(e) => out.push_str(&format!("{p}{};\n", expr(e, target)?)),
        Stmt::Print { newline, args } => {
            if cpp {
                for (i, a) in args.iter().enumerate() {
                    if i > 0 {
                        out.push_str(&format!("{p}std::cout << \" \";\n"));
                    }
                    out.push_str(&format!("{p}std::cout << {};\n", expr(a, "cpp")?));
                }
                if *newline {
                    out.push_str(&format!("{p}std::cout << std::endl;\n"));
                }
            } else {
                for (i, a) in args.iter().enumerate() {
                    if i > 0 {
                        out.push_str(&format!("{p}printf(\" \");\n"));
                    }
                    let t = infer_type(a, env, functions)
                        .ok_or_else(|| EmitError("cannot prove C print argument type".into()))?;
                    let fmt = c_print_format(&t)
                        .ok_or_else(|| EmitError("C print type unsupported".into()))?;
                    let val = c_print_cast(&t, expr(a, "c")?);
                    out.push_str(&format!("{p}printf(\"{fmt}\", {val});\n"));
                }
                if *newline {
                    out.push_str(&format!("{p}printf(\"\\n\");\n"));
                }
            }
        }
        Stmt::Return(v) => {
            if let Some(v) = v {
                out.push_str(&format!("{p}return {};\n", expr(v, target)?));
            } else {
                out.push_str(&format!("{p}return;\n"));
            }
        }
        Stmt::If {
            cond,
            then_body,
            else_body,
        } => {
            out.push_str(&format!("{p}if ({}) {{\n", expr(cond, target)?));
            let mut a = env.clone();
            for x in then_body {
                emit_c_stmt(x, indent + 1, out, cpp, &mut a, functions)?;
            }
            if else_body.is_empty() {
                out.push_str(&format!("{p}}}\n"));
            } else {
                out.push_str(&format!("{p}}} else {{\n"));
                let mut b = env.clone();
                for x in else_body {
                    emit_c_stmt(x, indent + 1, out, cpp, &mut b, functions)?;
                }
                out.push_str(&format!("{p}}}\n"));
            }
        }
        Stmt::While { cond, body } => {
            out.push_str(&format!("{p}while ({}) {{\n", expr(cond, target)?));
            let mut inner = env.clone();
            for x in body {
                emit_c_stmt(x, indent + 1, out, cpp, &mut inner, functions)?;
            }
            out.push_str(&format!("{p}}}\n"));
        }
        Stmt::ForEach { .. } => {
            return Err(EmitError(
                "C/C++ foreach needs element/length metadata and is not emitted unsafely".into(),
            ))
        }
        Stmt::Switch { .. } => {
            return Err(EmitError("switch lowering for C/C++ needs typed single-evaluation temporaries and is not emitted unsafely".into()));
        }
        Stmt::Break => out.push_str(&format!("{p}break;\n")),
        Stmt::Continue => out.push_str(&format!("{p}continue;\n")),
    }
    Ok(())
}

pub fn emit(program: &Program, target: &str) -> Result<String, EmitError> {
    let mut out = String::new();
    match target {
        "rust" => {
            out.push_str("// Generated by code-transpiler native IR\n\n");
            for st in &program.structs {
                out.push_str(
                    "#[allow(non_snake_case)]\n#[derive(Debug, Clone, PartialEq, Default)]\n",
                );
                out.push_str(&format!("struct {} {{\n", st.name));
                for field in &st.fields {
                    out.push_str(&format!("    {}: {},\n", field.name, rust_type(&field.ty)));
                }
                out.push_str("}\n\n");
            }
            for f in &program.functions {
                emit_rust_fn(f, &mut out)?;
            }
        }
        "python" => {
            out.push_str("# Generated by code-transpiler native IR\n\n");
            for st in &program.structs {
                out.push_str(&format!("class {}:\n", st.name));
                let args = st
                    .fields
                    .iter()
                    .map(|f| f.name.clone())
                    .collect::<Vec<_>>()
                    .join(", ");
                out.push_str(&format!("    def __init__(self, {}):\n", args));
                if st.fields.is_empty() {
                    out.push_str("        pass\n");
                }
                for field in &st.fields {
                    out.push_str(&format!("        self.{} = {}\n", field.name, field.name));
                }
                out.push('\n');
            }
            for f in &program.functions {
                let ps = f
                    .params
                    .iter()
                    .map(|p| p.name.clone())
                    .collect::<Vec<_>>()
                    .join(", ");
                out.push_str(&format!("def {}({}):\n", f.name, ps));
                if f.body.is_empty() {
                    out.push_str("    pass\n");
                }
                for s in &f.body {
                    emit_python_stmt(s, 1, &mut out)?;
                }
                out.push('\n');
            }
            if program.functions.iter().any(|f| f.name == "main") {
                out.push_str("if __name__ == \"__main__\":\n    main()\n");
            }
        }
        "julia" => {
            out.push_str("# Generated by code-transpiler native IR\n\n");
            for st in &program.structs {
                out.push_str(&format!("mutable struct {}\n", st.name));
                for field in &st.fields {
                    out.push_str(&format!("    {}::{}\n", field.name, julia_type(&field.ty)));
                }
                out.push_str("end\n");
                if !st.fields.is_empty() {
                    let names = st.fields.iter().map(|f| f.name.clone()).collect::<Vec<_>>();
                    out.push_str(&format!(
                        "{}(; {}) = {}({})\n",
                        st.name,
                        names.join(", "),
                        st.name,
                        names.join(", ")
                    ));
                }
                out.push('\n');
            }
            for f in &program.functions {
                let ps = f
                    .params
                    .iter()
                    .map(|p| format!("{}::{}", p.name, julia_type(&p.ty)))
                    .collect::<Vec<_>>()
                    .join(", ");
                let rt = f
                    .return_type
                    .as_ref()
                    .map(|t| format!("::{}", julia_type(t)))
                    .unwrap_or_default();
                out.push_str(&format!("function {}({}){}\n", f.name, ps, rt));
                for s in &f.body {
                    emit_julia_stmt(s, 1, &mut out)?;
                }
                out.push_str("end\n\n");
            }
            if program.functions.iter().any(|f| f.name == "main") {
                out.push_str("main()\n");
            }
        }
        "c" | "cpp" => {
            let cpp = target == "cpp";
            if cpp {
                out.push_str("#include <cstdint>\n#include <cstddef>\n#include <iostream>\n\n");
                for st in &program.structs {
                    out.push_str(&format!("struct {} {{\n", st.name));
                    for field in &st.fields {
                        out.push_str(&format!("    {} {};\n", c_type(&field.ty), field.name));
                    }
                    out.push_str("};\n\n");
                }
            } else {
                out.push_str("#include <stdbool.h>\n#include <stdint.h>\n#include <stddef.h>\n#include <stdio.h>\n\n");
                for st in &program.structs {
                    out.push_str(&format!("typedef struct {} {{\n", st.name));
                    for field in &st.fields {
                        out.push_str(&format!("    {} {};\n", c_type(&field.ty), field.name));
                    }
                    out.push_str(&format!("}} {};\n\n", st.name));
                }
            }
            if program
                .functions
                .iter()
                .any(|f| matches!(f.return_type, Some(Type::Tuple(_))))
            {
                return Err(EmitError(
                    "multiple-return functions are not yet emitted for C/C++".into(),
                ));
            }
            let functions: BTreeMap<String, Option<Type>> = program
                .functions
                .iter()
                .map(|f| (f.name.clone(), f.return_type.clone()))
                .collect();
            for f in &program.functions {
                let mut env: BTreeMap<String, Type> = f
                    .params
                    .iter()
                    .map(|p| (p.name.clone(), p.ty.clone()))
                    .collect();
                let rt = f
                    .return_type
                    .as_ref()
                    .map(c_type)
                    .unwrap_or_else(|| "void".into());
                let ps = f
                    .params
                    .iter()
                    .map(|p| format!("{} {}", c_type(&p.ty), p.name))
                    .collect::<Vec<_>>()
                    .join(", ");
                let rt = if f.name == "main" && f.return_type.is_none() {
                    "int".into()
                } else {
                    rt
                };
                out.push_str(&format!("{rt} {}({ps}) {{\n", f.name));
                for s in &f.body {
                    emit_c_stmt(s, 1, &mut out, cpp, &mut env, &functions)?;
                }
                if f.name == "main" && f.return_type.is_none() {
                    out.push_str("    return 0;\n");
                }
                out.push_str("}\n\n");
            }
        }
        _ => {
            return Err(EmitError(format!(
                "native emitter not implemented for target {target}"
            )))
        }
    }
    Ok(out)
}
