use crate::matrix::Matrix;
use std::collections::BTreeSet;

pub const EXACT_CALL_RESOLUTION_CAPABILITY: &str = "call.resolution.exact.v1";

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CallCandidate {
    pub name: String,
    pub declaration: String,
}
#[derive(Debug, Clone, PartialEq)]
pub struct CallResolution {
    pub candidates: Vec<CallCandidate>,
    pub obligations: Vec<String>,
    pub selected: Option<usize>,
    pub required: Matrix,
    pub satisfied: Matrix,
    pub conversion_cost: Matrix,
    pub priority: Vec<f64>,
}

fn finite_nonnegative(v: f64) -> bool {
    v.is_finite() && v >= 0.0
}
fn score_less(a: (f64, f64, f64), b: (f64, f64, f64)) -> bool {
    a.0 < b.0 || (a.0 == b.0 && (a.1 < b.1 || (a.1 == b.1 && a.2 < b.2)))
}

pub fn validate_call_resolution(r: &CallResolution, arguments: usize) -> Result<usize, String> {
    let n = r.candidates.len();
    let obligations = r.obligations.len();
    let selected = r
        .selected
        .ok_or_else(|| "call resolution has no valid selected candidate".to_string())?;
    if n == 0 || selected >= n {
        return Err("call resolution has no valid selected candidate".into());
    }
    if !r.required.valid()
        || !r.satisfied.valid()
        || !r.conversion_cost.valid()
        || r.required.rows != n
        || r.required.cols != obligations
        || r.satisfied.rows != n
        || r.satisfied.cols != obligations
        || r.conversion_cost.rows != n
        || r.conversion_cost.cols != arguments
        || r.priority.len() != n
    {
        return Err(
            "call resolution matrix dimensions do not match candidates, obligations, and arguments"
                .into(),
        );
    }
    let mut obs = BTreeSet::new();
    for o in &r.obligations {
        if o.is_empty() || !obs.insert(o.clone()) {
            return Err("call resolution obligation names must be unique and nonempty".into());
        }
    }
    let mut cand = BTreeSet::new();
    for c in &r.candidates {
        let key = format!("{}\0{}", c.name, c.declaration);
        if c.name.is_empty() || !cand.insert(key) {
            return Err("call resolution candidates must be unique and named".into());
        }
    }
    let mut scores = Vec::with_capacity(n);
    for row in 0..n {
        if !finite_nonnegative(r.priority[row]) {
            return Err("call resolution priority must be finite and nonnegative".into());
        }
        let mut missing = 0.0;
        let mut cost = 0.0;
        for col in 0..obligations {
            let req = r.required.at(row, col);
            let sat = r.satisfied.at(row, col);
            if (req != 0.0 && req != 1.0) || (sat != 0.0 && sat != 1.0) || sat > req {
                return Err("call resolution obligation planes must be binary and satisfied implies required".into());
            }
            missing += req * (1.0 - sat);
        }
        for col in 0..arguments {
            let v = r.conversion_cost.at(row, col);
            if !finite_nonnegative(v) {
                return Err(
                    "call resolution conversion costs must be finite and nonnegative".into(),
                );
            }
            cost += v;
        }
        scores.push((missing, cost, r.priority[row]));
    }
    let mut best = 0usize;
    for i in 1..n {
        if score_less(scores[i], scores[best]) {
            best = i;
        }
    }
    let ties = scores.iter().filter(|&&s| s == scores[best]).count();
    if ties != 1 {
        return Err("call resolution is ambiguous at the minimum matrix score".into());
    }
    if selected != best {
        return Err(format!(
            "selected call candidate {selected} does not match matrix result {best}"
        ));
    }
    Ok(best)
}
