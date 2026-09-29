use crate::registry::normalize_language;
use std::collections::{BTreeMap, BTreeSet};
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub enum PreservationMode {
    Direct,
    Rewrite,
    Helper,
    Emulate,
    Runtime,
    Error,
}
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PreservationRule {
    pub capability: String,
    pub target: String,
    pub mode: PreservationMode,
    pub preconditions: Vec<String>,
    pub requirements: Vec<String>,
    pub handler: String,
    pub test: String,
}
#[derive(Debug, Clone, Default)]
pub struct PreservationRegistry {
    pub rules: Vec<PreservationRule>,
}
impl PreservationRegistry {
    pub fn solve(&self, target: &str, capability: &str) -> Option<&PreservationRule> {
        let t = normalize_language(target);
        [
            PreservationMode::Direct,
            PreservationMode::Rewrite,
            PreservationMode::Helper,
            PreservationMode::Emulate,
            PreservationMode::Runtime,
        ]
        .into_iter()
        .find_map(|m| {
            self.rules
                .iter()
                .find(|r| r.target == t && r.capability == capability && r.mode == m)
        })
    }
}
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum RequirementKind {
    Import,
    Helper,
    Emulation,
    Runtime,
}
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Requirement {
    pub id: String,
    pub kind: RequirementKind,
    pub target: String,
    pub dependencies: Vec<String>,
    pub preconditions: Vec<String>,
    pub tests: Vec<String>,
    pub emit: String,
}
#[derive(Debug, Clone, Default)]
pub struct RequirementRegistry {
    pub rules: BTreeMap<String, Requirement>,
}
impl RequirementRegistry {
    pub fn resolve(&self, ids: &[String]) -> Result<Vec<&Requirement>, String> {
        fn visit<'a>(
            id: &str,
            r: &'a RequirementRegistry,
            state: &mut BTreeMap<String, u8>,
            out: &mut Vec<&'a Requirement>,
        ) -> Result<(), String> {
            match state.get(id).copied().unwrap_or(0) {
                1 => return Err(format!("cyclic requirement: {id}")),
                2 => return Ok(()),
                _ => {}
            }
            let rule = r
                .rules
                .get(id)
                .ok_or_else(|| format!("unknown requirement: {id}"))?;
            state.insert(id.into(), 1);
            for d in &rule.dependencies {
                visit(d, r, state, out)?;
            }
            state.insert(id.into(), 2);
            out.push(rule);
            Ok(())
        }
        let mut state = BTreeMap::new();
        let mut out = Vec::new();
        let mut uniq = BTreeSet::new();
        for id in ids {
            uniq.insert(id.clone());
        }
        for id in uniq {
            visit(&id, self, &mut state, &mut out)?;
        }
        Ok(out)
    }
}

pub fn target_name(target: &str, preferred: &str) -> String {
    let mut s: String = preferred
        .chars()
        .map(|c| {
            if c.is_ascii_alphanumeric() || c == '_' {
                c
            } else {
                '_'
            }
        })
        .collect();
    if s.is_empty() {
        s = "value".into();
    }
    if s.chars().next().unwrap().is_ascii_digit() {
        s.insert(0, '_');
    }
    if reserved(target).contains(s.as_str()) {
        format!("__uast_{s}")
    } else {
        s
    }
}
fn reserved(target: &str) -> BTreeSet<&'static str> {
    let words=match normalize_language(target).as_str(){"go"=>"var const func type package import return if else for switch case default range go defer map struct interface","rust"=>"as break const continue crate else enum extern false fn for if impl in let loop match mod move mut pub ref return self Self static struct super trait true type unsafe use where while","cpp"=>"auto class struct template typename namespace return if else for while switch case default const static void int double float char bool",_=>""};
    words.split_whitespace().collect()
}
pub fn target_na(target: &str) -> &'static str {
    match normalize_language(target).as_str() {
        "rust" => "RValue::Num(f64::NAN)",
        "go" => "math.NaN()",
        "julia" => "NaN",
        "python" => "float('nan')",
        _ => "NaN",
    }
}
pub fn target_inf(target: &str) -> &'static str {
    match normalize_language(target).as_str() {
        "rust" => "RValue::Num(f64::INFINITY)",
        "go" => "math.Inf(1)",
        "julia" => "Inf",
        "python" => "float('inf')",
        _ => "Inf",
    }
}
pub fn emit_dispatch(target: &str, name: &str, args: &[&str]) -> Result<String, String> {
    let k = if let Some(op) = name.strip_prefix("__binary_") {
        if ["+", "-", "*", "/", "^", "**", "%%", "%/%"].contains(&op) {
            "arithmetic"
        } else if ["==", "!=", "<", "<=", ">", ">="].contains(&op) {
            "relational"
        } else if ["&", "|", "&&", "||"].contains(&op) {
            "logical"
        } else {
            "language"
        }
    } else if name.starts_with("__unary_") {
        "arithmetic"
    } else {
        "runtime"
    };
    let a = args.join(", ");
    match normalize_language(target).as_str() {
        "rust" => Ok(format!("r_call({k:?}, {name:?}, vec![{a}])")),
        "go" => Ok(format!("rCall({k:?}, {name:?}, []any{{{a}}})")),
        "julia" => Ok(format!("r_call({k:?}, {name:?}, Any[{a}])")),
        "python" => Ok(format!("r_call({k:?}, {name:?}, [{a}])")),
        _ => Err(format!("target dispatch not yet native for {target}")),
    }
}
