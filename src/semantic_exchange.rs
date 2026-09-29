use crate::emit::emit;
use crate::ir::{Expr, Program, Stmt, Type};

pub const SE_MAGIC: &str = "SE/1";
const SEMANTICS_MARKER: &str = "--- semantics-json ---";
const PIVOT_MARKER: &str = "--- julia-pivot ---";
const END_MARKER: &str = "--- end ---";

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SemanticBindingInfo {
    pub name: String,
    pub kind: String,
    pub ty: String,
    pub scope: String,
    pub mutable: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SemanticOperationInfo {
    pub op: String,
    pub result_type: String,
    pub scope: String,
    pub source: String,
    pub semantics: Vec<(String, String)>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SemanticFunctionInfo {
    pub name: String,
    pub parameters: Vec<(String, String)>,
    pub return_type: String,
    pub bindings: Vec<SemanticBindingInfo>,
    pub operations: Vec<SemanticOperationInfo>,
    pub effects: Vec<String>,
    pub control_flow: Vec<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SemanticProgram {
    pub source_language: String,
    pub evaluation: String,
    pub value_model: String,
    pub source_index_base: usize,
    pub index_base: usize,
    pub functions: Vec<SemanticFunctionInfo>,
    pub features: Vec<String>,
    pub effects: Vec<String>,
    pub warnings: Vec<String>,
}

fn type_name(t: Option<&Type>) -> String {
    match t {
        None | Some(Type::Unit) => "Nothing".into(),
        Some(Type::Bool) => "Bool".into(),
        Some(Type::String) => "String".into(),
        Some(Type::Int) => "Int".into(),
        Some(Type::Int8) => "Int8".into(),
        Some(Type::Int16) => "Int16".into(),
        Some(Type::Int32) => "Int32".into(),
        Some(Type::Int64) => "Int64".into(),
        Some(Type::UInt) => "UInt".into(),
        Some(Type::UInt8) => "UInt8".into(),
        Some(Type::UInt16) => "UInt16".into(),
        Some(Type::UInt32) => "UInt32".into(),
        Some(Type::UInt64) => "UInt64".into(),
        Some(Type::Float32) => "Float32".into(),
        Some(Type::Float64) => "Float64".into(),
        Some(Type::Slice(x)) => format!("Vector{{{}}}", type_name(Some(x))),
        Some(Type::Map(k, v)) => format!("Dict{{{}, {}}}", type_name(Some(k)), type_name(Some(v))),
        Some(Type::Tuple(items)) => format!(
            "Tuple{{{}}}",
            items
                .iter()
                .map(|x| type_name(Some(x)))
                .collect::<Vec<_>>()
                .join(", ")
        ),
        Some(Type::Named(n)) => n.clone(),
    }
}

fn expr_text(e: &Expr) -> String {
    match e {
        Expr::Ident(x) | Expr::Int(x) | Expr::Float(x) => x.clone(),
        Expr::String(x) => format!("{:?}", x),
        Expr::Bool(v) => v.to_string(),
        Expr::Nil => "nothing".into(),
        Expr::Unary { op, value } => format!("{}{}", op, expr_text(value)),
        Expr::Binary { left, op, right } => {
            format!("{} {} {}", expr_text(left), op, expr_text(right))
        }
        Expr::Call { function, args } => format!(
            "{}({})",
            function,
            args.iter().map(expr_text).collect::<Vec<_>>().join(", ")
        ),
        Expr::Index { value, index } => format!("{}[{}]", expr_text(value), expr_text(index)),
        Expr::Array { items, .. } => format!(
            "[{}]",
            items.iter().map(expr_text).collect::<Vec<_>>().join(", ")
        ),
        Expr::Map { entries, .. } => format!(
            "Dict({})",
            entries
                .iter()
                .map(|(k, v)| format!("{} => {}", expr_text(k), expr_text(v)))
                .collect::<Vec<_>>()
                .join(", ")
        ),
        Expr::MapIndex { value, index, .. } => {
            format!("{}[{}]", expr_text(value), expr_text(index))
        }
        Expr::MapLookupOk { value, index, .. } => {
            format!("lookup_ok({}, {})", expr_text(value), expr_text(index))
        }
        Expr::Tuple(items) => format!(
            "({})",
            items.iter().map(expr_text).collect::<Vec<_>>().join(", ")
        ),
        Expr::StructLiteral {
            ty,
            fields,
            positional,
        } => {
            if !fields.is_empty() {
                format!(
                    "{}{{{}}}",
                    ty,
                    fields
                        .iter()
                        .map(|(k, v)| format!("{}={}", k, expr_text(v)))
                        .collect::<Vec<_>>()
                        .join(", ")
                )
            } else {
                format!(
                    "{}({})",
                    ty,
                    positional
                        .iter()
                        .map(expr_text)
                        .collect::<Vec<_>>()
                        .join(", ")
                )
            }
        }
        Expr::Raw(x) => x.clone(),
    }
}

fn operation(e: &Expr, scope: &str) -> Option<SemanticOperationInfo> {
    let (op, result, semantics) = match e {
        Expr::Binary { op, .. } => {
            let name = match op.as_str() {
                "+" => "numeric.add",
                "-" => "numeric.subtract",
                "*" => "numeric.multiply",
                "/" => "numeric.divide",
                "%" => "numeric.remainder",
                "==" => "comparison.equal",
                "!=" => "comparison.not_equal",
                "<" => "comparison.less",
                "<=" => "comparison.less_equal",
                ">" => "comparison.greater",
                ">=" => "comparison.greater_equal",
                "&&" => "logical.and",
                "||" => "logical.or",
                _ => "operator",
            };
            (
                name,
                "unknown",
                vec![("evaluation_order".into(), "left_to_right".into())],
            )
        }
        Expr::Index { .. } | Expr::MapIndex { .. } => (
            "collection.index",
            "unknown",
            vec![("bounds".into(), "source_language".into())],
        ),
        Expr::Call { function, .. } if function == "len" || function == "length" => (
            "collection.length",
            "Int",
            vec![("result".into(), "canonical_machine_integer".into())],
        ),
        Expr::Call { function, .. } if function == "append" || function == "push!" => (
            "collection.append",
            "Nothing",
            vec![("mutation".into(), "in_place_or_source_equivalent".into())],
        ),
        Expr::Call { .. } => (
            "function.call",
            "unknown",
            vec![("evaluation_order".into(), "left_to_right".into())],
        ),
        _ => return None,
    };
    Some(SemanticOperationInfo {
        op: op.into(),
        result_type: result.into(),
        scope: scope.into(),
        source: expr_text(e),
        semantics,
    })
}

fn inspect_expr(
    e: &Expr,
    scope: &str,
    ops: &mut Vec<SemanticOperationInfo>,
    features: &mut Vec<String>,
) {
    if let Some(op) = operation(e, scope) {
        ops.push(op);
    }
    match e {
        Expr::Binary { left, right, .. } => {
            inspect_expr(left, scope, ops, features);
            inspect_expr(right, scope, ops, features);
        }
        Expr::Unary { value, .. } => inspect_expr(value, scope, ops, features),
        Expr::Call { args, .. } => {
            for x in args {
                inspect_expr(x, scope, ops, features);
            }
        }
        Expr::Index { value, index }
        | Expr::MapIndex { value, index, .. }
        | Expr::MapLookupOk { value, index, .. } => {
            push_unique(features, "indexing");
            inspect_expr(value, scope, ops, features);
            inspect_expr(index, scope, ops, features);
        }
        Expr::Array { items, .. } | Expr::Tuple(items) => {
            for x in items {
                inspect_expr(x, scope, ops, features);
            }
        }
        Expr::Map { entries, .. } => {
            push_unique(features, "maps");
            for (k, v) in entries {
                inspect_expr(k, scope, ops, features);
                inspect_expr(v, scope, ops, features);
            }
        }
        Expr::StructLiteral {
            fields, positional, ..
        } => {
            push_unique(features, "structs");
            for (_, v) in fields {
                inspect_expr(v, scope, ops, features);
            }
            for v in positional {
                inspect_expr(v, scope, ops, features);
            }
        }
        _ => {}
    }
}

fn push_unique(v: &mut Vec<String>, value: &str) {
    if !v.iter().any(|x| x == value) {
        v.push(value.to_string());
    }
}

fn inspect_stmts(
    stmts: &[Stmt],
    scope: &str,
    info: &mut SemanticFunctionInfo,
    features: &mut Vec<String>,
) {
    for s in stmts {
        match s {
            Stmt::Let {
                name,
                ty,
                value,
                mutable,
            } => {
                info.bindings.push(SemanticBindingInfo {
                    name: name.clone(),
                    kind: "local".into(),
                    ty: type_name(ty.as_ref()),
                    scope: scope.into(),
                    mutable: *mutable,
                });
                inspect_expr(value, scope, &mut info.operations, features);
            }
            Stmt::MultiLet {
                names,
                value,
                mutable,
            } => {
                for name in names {
                    info.bindings.push(SemanticBindingInfo {
                        name: name.clone(),
                        kind: "local".into(),
                        ty: "unknown".into(),
                        scope: scope.into(),
                        mutable: *mutable,
                    });
                }
                inspect_expr(value, scope, &mut info.operations, features);
            }
            Stmt::Assign { value, .. } | Stmt::MultiAssign { value, .. } => {
                push_unique(&mut info.effects, "mutation.binding");
                inspect_expr(value, scope, &mut info.operations, features);
            }
            Stmt::IndexAssign {
                collection,
                index,
                value,
                ..
            } => {
                push_unique(&mut info.effects, "mutation.collection");
                push_unique(features, "indexing");
                inspect_expr(collection, scope, &mut info.operations, features);
                inspect_expr(index, scope, &mut info.operations, features);
                inspect_expr(value, scope, &mut info.operations, features);
            }
            Stmt::Expr(e) => inspect_expr(e, scope, &mut info.operations, features),
            Stmt::Print { args, .. } => {
                push_unique(&mut info.effects, "io.stdout");
                for e in args {
                    inspect_expr(e, scope, &mut info.operations, features);
                }
            }
            Stmt::Return(v) => {
                if let Some(e) = v {
                    inspect_expr(e, scope, &mut info.operations, features);
                }
            }
            Stmt::If {
                cond,
                then_body,
                else_body,
            } => {
                push_unique(features, "branching");
                info.control_flow.push("if".into());
                inspect_expr(cond, scope, &mut info.operations, features);
                inspect_stmts(then_body, scope, info, features);
                inspect_stmts(else_body, scope, info, features);
            }
            Stmt::While { cond, body } => {
                push_unique(features, "loops");
                info.control_flow.push("while".into());
                inspect_expr(cond, scope, &mut info.operations, features);
                inspect_stmts(body, scope, info, features);
            }
            Stmt::ForEach { iter, body, .. } => {
                push_unique(features, "loops");
                info.control_flow.push("for".into());
                inspect_expr(iter, scope, &mut info.operations, features);
                inspect_stmts(body, scope, info, features);
            }
            Stmt::Switch {
                value,
                cases,
                default,
            } => {
                push_unique(features, "branching");
                info.control_flow.push("switch".into());
                if let Some(v) = value {
                    inspect_expr(v, scope, &mut info.operations, features);
                }
                for c in cases {
                    for v in &c.values {
                        inspect_expr(v, scope, &mut info.operations, features);
                    }
                    inspect_stmts(&c.body, scope, info, features);
                }
                inspect_stmts(default, scope, info, features);
            }
            Stmt::Break | Stmt::Continue => {}
        }
    }
}

pub fn semantic_program(source_language: &str, program: &Program) -> SemanticProgram {
    let mut features = vec!["functions".into()];
    if !program.structs.is_empty() {
        push_unique(&mut features, "structs");
    }
    let mut functions = Vec::new();
    let mut global_effects = Vec::new();
    for f in &program.functions {
        let mut info = SemanticFunctionInfo {
            name: f.name.clone(),
            parameters: f
                .params
                .iter()
                .map(|p| (p.name.clone(), type_name(Some(&p.ty))))
                .collect(),
            return_type: type_name(f.return_type.as_ref()),
            bindings: f
                .params
                .iter()
                .map(|p| SemanticBindingInfo {
                    name: p.name.clone(),
                    kind: "parameter".into(),
                    ty: type_name(Some(&p.ty)),
                    scope: f.name.clone(),
                    mutable: false,
                })
                .collect(),
            operations: Vec::new(),
            effects: Vec::new(),
            control_flow: Vec::new(),
        };
        inspect_stmts(&f.body, &f.name, &mut info, &mut features);
        for e in &info.effects {
            push_unique(&mut global_effects, e);
        }
        functions.push(info);
    }
    let source_index_base = if source_language == "julia" || source_language == "r" {
        1
    } else {
        0
    };
    SemanticProgram {
        source_language: source_language.into(),
        evaluation: if source_language == "r" {
            "lazy_demand".into()
        } else {
            "eager_left_to_right".into()
        },
        value_model: match source_language {
            "rust" => "static_owned_values",
            "go" => "static_go_values",
            "c" | "cpp" => "static_native_machine_values",
            "python" => "dynamic_arbitrary_precision_integer",
            _ => "mixed_static_dynamic",
        }
        .into(),
        source_index_base,
        index_base: 1,
        functions,
        features,
        effects: global_effects,
        warnings: Vec::new(),
    }
}

fn esc(s: &str) -> String {
    let mut out = String::with_capacity(s.len() + 8);
    for c in s.chars() {
        match c {
            '\\' => out.push_str("\\\\"),
            '"' => out.push_str("\\\""),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            c if c.is_control() => out.push_str(&format!("\\u{:04x}", c as u32)),
            c => out.push(c),
        }
    }
    out
}
fn js(s: &str) -> String {
    format!("\"{}\"", esc(s))
}
fn jarr(v: &[String]) -> String {
    format!(
        "[{}]",
        v.iter().map(|x| js(x)).collect::<Vec<_>>().join(",")
    )
}

pub fn semantic_json(p: &SemanticProgram) -> String {
    let functions = p.functions.iter().map(|f| {
        let params = f.parameters.iter().map(|(n,t)| format!("{{\"name\":{},\"type\":{}}}", js(n), js(t))).collect::<Vec<_>>().join(",");
        let bindings = f.bindings.iter().map(|b| format!("{{\"name\":{},\"kind\":{},\"type\":{},\"scope\":{},\"mutable\":{}}}", js(&b.name), js(&b.kind), js(&b.ty), js(&b.scope), b.mutable)).collect::<Vec<_>>().join(",");
        let ops = f.operations.iter().map(|o| {
            let sem = o.semantics.iter().map(|(k,v)| format!("{}:{}", js(k), js(v))).collect::<Vec<_>>().join(",");
            format!("{{\"operation\":{},\"result_type\":{},\"scope\":{},\"source\":{},\"semantics\":{{{}}}}}", js(&o.op), js(&o.result_type), js(&o.scope), js(&o.source), sem)
        }).collect::<Vec<_>>().join(",");
        format!("{{\"name\":{},\"parameters\":[{}],\"return_type\":{},\"bindings\":[{}],\"operations\":[{}],\"effects\":{},\"control_flow\":{}}}", js(&f.name), params, js(&f.return_type), bindings, ops, jarr(&f.effects), jarr(&f.control_flow))
    }).collect::<Vec<_>>().join(",");
    format!("{{\"source_language\":{},\"evaluation\":{},\"value_model\":{},\"source_index_base\":{},\"index_base\":{},\"functions\":[{}],\"globals\":[],\"features\":{},\"effects\":{},\"warnings\":{}}}\n",
        js(&p.source_language), js(&p.evaluation), js(&p.value_model), p.source_index_base, p.index_base, functions, jarr(&p.features), jarr(&p.effects), jarr(&p.warnings))
}

pub fn semantic_text(p: &SemanticProgram) -> String {
    let mut out = String::new();
    out.push_str("SemanticProgram\n");
    out.push_str(&format!("  source: {}\n  evaluation: {}\n  value_model: {}\n  source_index_base: {}\n  canonical_index_base: {}\n  features: {}\n", p.source_language, p.evaluation, p.value_model, p.source_index_base, p.index_base, if p.features.is_empty() { "none".into() } else { p.features.join(", ") }));
    for f in &p.functions {
        out.push_str(&format!(
            "\nfunction {}({}) -> {}\n",
            f.name,
            f.parameters
                .iter()
                .map(|(n, t)| format!("{}::{}", n, t))
                .collect::<Vec<_>>()
                .join(", "),
            f.return_type
        ));
        if !f.control_flow.is_empty() {
            out.push_str(&format!("  control: {}\n", f.control_flow.join(" -> ")));
        }
        if !f.effects.is_empty() {
            out.push_str(&format!("  effects: {}\n", f.effects.join(", ")));
        }
        if !f.bindings.is_empty() {
            out.push_str("  bindings:\n");
            for b in &f.bindings {
                out.push_str(&format!(
                    "    - {} : {} [{}{}]\n",
                    b.name,
                    b.ty,
                    b.kind,
                    if b.mutable { ", mutable" } else { "" }
                ));
            }
        }
        if !f.operations.is_empty() {
            out.push_str("  operations:\n");
            for op in &f.operations {
                out.push_str(&format!(
                    "    - {} -> {}: {}\n",
                    op.op, op.result_type, op.source
                ));
            }
        }
    }
    out
}

pub fn semantic_exchange(source: &str, program: &Program) -> Result<String, String> {
    let pivot = emit(program, "julia").map_err(|e| e.to_string())?;
    let sem = semantic_json(&semantic_program(source, program));
    Ok(format!("{SE_MAGIC}\nsource: {source}\ncanonical: julia\n{SEMANTICS_MARKER}\n{sem}{PIVOT_MARKER}\n{pivot}{END_MARKER}\n"))
}

pub fn semantic_exchange_pivot(text: &str) -> Result<String, String> {
    let s = text.replace("\r\n", "\n");
    if !s.starts_with("SE/1\n") {
        return Err("invalid SE input: expected SE/1 header".into());
    }
    let marker = format!("{PIVOT_MARKER}\n");
    let start = s
        .find(&marker)
        .ok_or_else(|| "invalid SE input: missing Julia pivot section".to_string())?
        + marker.len();
    let end_marker = format!("\n{END_MARKER}");
    let rel = s[start..]
        .find(&end_marker)
        .ok_or_else(|| "invalid SE input: missing end marker".to_string())?;
    let pivot = &s[start..start + rel];
    if pivot.trim().is_empty() {
        return Err("invalid SE input: empty Julia pivot".into());
    }
    Ok(format!(
        "{}{}",
        pivot,
        if pivot.ends_with('\n') { "" } else { "\n" }
    ))
}
