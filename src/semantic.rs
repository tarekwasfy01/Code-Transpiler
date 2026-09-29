use crate::runtime::RValue;
use std::collections::BTreeMap;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SemanticEffectSummary {
    pub counts: BTreeMap<String, usize>,
    pub conservative_pure: bool,
    pub unknown: bool,
}

/// Port of the effect-summary decision rule. Input is already-projected effect axes.
pub fn summarize_effect_axes(axes: &[(String, Vec<bool>)]) -> SemanticEffectSummary {
    let mut out = SemanticEffectSummary {
        counts: BTreeMap::new(),
        conservative_pure: true,
        unknown: false,
    };
    for (axis, rows) in axes {
        let count = rows.iter().filter(|&&x| x).count();
        if count == 0 {
            continue;
        }
        out.counts.insert(axis.clone(), count);
        match axis.as_str() {
            "local.read" | "control" => {}
            "call.unknown" => {
                out.unknown = true;
                out.conservative_pure = false;
            }
            _ => out.conservative_pure = false,
        }
    }
    out
}

/// Snapshot semantics for iteration: null -> empty, vector -> private outer copy, scalar -> singleton.
pub fn snapshot_iteration(value: &RValue) -> Vec<RValue> {
    match value {
        RValue::Null => Vec::new(),
        RValue::Vec(xs) => xs.clone(),
        x => vec![x.clone()],
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct StructuredConstructInput {
    pub family: String,
    pub node_kind: String,
    pub roles: Vec<String>,
    pub operand_count: usize,
    pub binding_count: usize,
    pub type_count: usize,
    pub fields: BTreeMap<String, String>,
    pub source_offset: usize,
}

pub fn structured_input_fields(input: &StructuredConstructInput) -> BTreeMap<&'static str, bool> {
    let mut out = BTreeMap::new();
    out.insert("node kind", !input.node_kind.is_empty());
    for role in &input.roles {
        out.insert("child", true);
        match role.as_str() {
            "base" => {
                out.insert("operand", true);
            }
            "index" => {
                out.insert("index", true);
            }
            "start" | "end" | "step" => {
                out.insert("slice bounds", true);
            }
            "parameter" => {
                out.insert("parameter", true);
            }
            "binding" => {
                out.insert("binding", true);
            }
            "body" => {
                out.insert("control/body reference", true);
            }
            "iterable" => {
                out.insert("iterable", true);
            }
            "condition" | "filter" => {
                out.insert("filter/condition", true);
            }
            _ => {}
        }
    }
    if input.family == "CONTAINER" && !input.roles.is_empty() {
        out.insert("child", true);
    }
    if input.operand_count > 0 {
        out.insert("operand", true);
    }
    if input.binding_count > 0 {
        out.insert("binding", true);
    }
    if input.type_count > 0 {
        out.insert("type", true);
    }
    out
}

pub fn missing_structured_fields(input: &StructuredConstructInput) -> Vec<&'static str> {
    let required: &[&str] = match input.family.as_str() {
        "CONTAINER" => &[
            "node kind",
            "child",
            "operand",
            "type",
            "binding",
            "allocation size",
        ],
        "ITERATION" => &[
            "node kind",
            "iterable",
            "binding",
            "child",
            "control/body reference",
            "filter/condition",
        ],
        "CLOSURE_FUNCTION_VALUE" => &[
            "node kind",
            "parameter",
            "capture",
            "binding",
            "child",
            "control/body reference",
        ],
        "INDEX_SLICE" => &["node kind", "operand", "index", "slice bounds", "binding"],
        _ => &[],
    };
    let available = structured_input_fields(input);
    required
        .iter()
        .copied()
        .filter(|field| !available.get(field).copied().unwrap_or(false))
        .collect()
}
