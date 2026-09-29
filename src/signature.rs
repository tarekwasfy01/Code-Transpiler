use crate::matrix::SparseMatrix;
use std::collections::{BTreeMap, BTreeSet};

pub const EXACT_SIGNATURE_CAPABILITY: &str = "function.signature.exact.v1";

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SignatureParameter {
    pub name: String,
    pub passing: String,
    pub has_default: bool,
}
#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct SignatureArgument {
    pub name: String,
    pub spread: bool,
}
#[derive(Debug, Clone, PartialEq)]
pub struct SignatureBinding {
    pub parameter_arguments: SparseMatrix,
    pub use_defaults: Vec<usize>,
    pub argument_counts: Vec<usize>,
}

pub fn bind_signature(
    parameters: &[SignatureParameter],
    arguments: &[SignatureArgument],
) -> Result<SignatureBinding, String> {
    let rank: BTreeMap<&str, usize> = [
        ("positional_only", 0),
        ("positional_or_keyword", 1),
        ("variadic_positional", 2),
        ("keyword_only", 3),
        ("variadic_keyword", 4),
    ]
    .into_iter()
    .collect();
    let mut names = BTreeMap::<String, usize>::new();
    let mut positional = Vec::<usize>::new();
    let mut var_pos: Option<usize> = None;
    let mut var_kw: Option<usize> = None;
    let mut last_rank = 0usize;
    let mut seen_any = false;
    let mut optional_pos = false;
    for (i, p) in parameters.iter().enumerate() {
        let order = *rank
            .get(p.passing.as_str())
            .ok_or_else(|| format!("invalid signature parameter {i}"))?;
        if p.name.is_empty() {
            return Err(format!("invalid signature parameter {i}"));
        }
        if names.insert(p.name.clone(), i).is_some() {
            return Err(format!("duplicate parameter {:?}", p.name));
        }
        if seen_any && order < last_rank {
            return Err(format!("parameter {:?} has invalid mode order", p.name));
        }
        seen_any = true;
        last_rank = order;
        match p.passing.as_str() {
            "positional_only" | "positional_or_keyword" => {
                if optional_pos && !p.has_default {
                    return Err("required positional parameter after default".into());
                }
                optional_pos |= p.has_default;
                positional.push(i);
            }
            "variadic_positional" => {
                if var_pos.is_some() || p.has_default {
                    return Err("invalid variadic positional parameter".into());
                }
                var_pos = Some(i);
            }
            "variadic_keyword" => {
                if var_kw.is_some() || p.has_default {
                    return Err("invalid variadic keyword parameter".into());
                }
                var_kw = Some(i);
            }
            "keyword_only" => {}
            _ => unreachable!(),
        }
    }

    let mut binding = SignatureBinding {
        parameter_arguments: SparseMatrix::new(parameters.len(), arguments.len()),
        use_defaults: vec![0; parameters.len()],
        argument_counts: vec![0; parameters.len()],
    };
    let mut next_pos = 0usize;
    let mut keywords = BTreeSet::<String>::new();
    for (col, arg) in arguments.iter().enumerate() {
        if arg.spread {
            return Err(format!("argument {col} requires runtime spread expansion"));
        }
        let row = if arg.name.is_empty() {
            if next_pos < positional.len() {
                let r = Some(positional[next_pos]);
                next_pos += 1;
                r
            } else {
                var_pos
            }
        } else {
            if !keywords.insert(arg.name.clone()) {
                return Err(format!("duplicate keyword {:?}", arg.name));
            }
            match names.get(&arg.name).copied() {
                Some(i)
                    if matches!(
                        parameters[i].passing.as_str(),
                        "positional_or_keyword" | "keyword_only"
                    ) =>
                {
                    Some(i)
                }
                _ => var_kw,
            }
        }
        .ok_or_else(|| format!("argument {col} has no accepting parameter"))?;
        binding.parameter_arguments.set(row, col, 1.0);
    }
    for (i, p) in parameters.iter().enumerate() {
        let count = (0..arguments.len())
            .filter(|&c| binding.parameter_arguments.at(i, c) != 0.0)
            .count();
        binding.argument_counts[i] = count;
        if matches!(
            p.passing.as_str(),
            "variadic_positional" | "variadic_keyword"
        ) {
            continue;
        }
        if count > 1 {
            return Err(format!("parameter {:?} supplied more than once", p.name));
        }
        if count == 0 {
            if !p.has_default {
                return Err(format!("missing required parameter {:?}", p.name));
            }
            binding.use_defaults[i] = 1;
        }
    }
    Ok(binding)
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FunctionSignatureContract {
    pub binding: String,
    pub default_evaluation: String,
    pub parameters: Vec<SignatureParameter>,
}

/// Validate the optional exact_v1 named/default argument contract.
pub fn validate_signature_contract(
    contract: &FunctionSignatureContract,
    evaluation: &str,
) -> Result<bool, String> {
    if contract.binding.is_empty() {
        if !contract.default_evaluation.is_empty() {
            return Err("default evaluation requires an explicit function binding contract".into());
        }
        if contract.parameters.iter().any(|p| !p.passing.is_empty()) {
            return Err("parameter modes require exact binding".into());
        }
        return Ok(false);
    }
    if contract.binding != "exact_v1" {
        if contract.default_evaluation.is_empty() {
            if contract.parameters.iter().any(|p| !p.passing.is_empty()) {
                return Err("parameter modes require exact binding".into());
            }
            return Ok(false);
        }
        return Err("unsupported function binding/default contract".into());
    }
    if !matches!(contract.default_evaluation.as_str(), "definition" | "call") {
        return Err("unsupported function binding/default contract".into());
    }
    let args = contract
        .parameters
        .iter()
        .filter_map(|p| match p.passing.as_str() {
            "positional_only" | "positional_or_keyword" => Some(SignatureArgument::default()),
            "keyword_only" => Some(SignatureArgument {
                name: p.name.clone(),
                spread: false,
            }),
            "variadic_positional" | "variadic_keyword" => None,
            _ => Some(SignatureArgument::default()),
        })
        .collect::<Vec<_>>();
    bind_signature(&contract.parameters, &args)?;
    if evaluation != "eager_left_to_right" {
        return Err("exact signatures currently require explicit eager evaluation".into());
    }
    Ok(true)
}
