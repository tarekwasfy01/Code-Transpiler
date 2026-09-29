#[derive(Debug, Clone, PartialEq)]
pub enum RValue {
    Null,
    Num(f64),
    Bool(bool),
    Str(String),
    Vec(Vec<RValue>),
    Int { raw: u128, bits: u32, signed: bool },
}

impl RValue {
    pub fn number(&self) -> f64 {
        match self {
            Self::Num(x) => *x,
            Self::Bool(x) => {
                if *x {
                    1.0
                } else {
                    0.0
                }
            }
            _ => f64::NAN,
        }
    }
    pub fn truth(&self) -> bool {
        match self {
            Self::Null => false,
            Self::Bool(x) => *x,
            Self::Num(x) => *x != 0.0 && !x.is_nan(),
            Self::Str(x) => !x.is_empty(),
            Self::Vec(x) => !x.is_empty(),
            Self::Int { raw, .. } => *raw != 0,
        }
    }
    pub fn into_iter_values(self) -> Vec<RValue> {
        match self {
            Self::Vec(x) => x,
            x => vec![x],
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RuntimeError(pub String);
impl std::fmt::Display for RuntimeError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.0)
    }
}
impl std::error::Error for RuntimeError {}

pub fn r_bind(args: &[RValue], index: usize, default: RValue) -> RValue {
    args.get(index).cloned().unwrap_or(default)
}

fn binary(op: &str, a: &RValue, b: &RValue) -> Result<RValue, RuntimeError> {
    if matches!(op, "==" | "!=") {
        if let (RValue::Str(x), RValue::Str(y)) = (a, b) {
            return Ok(RValue::Bool(if op == "==" { x == y } else { x != y }));
        }
    }
    let x = a.number();
    let y = b.number();
    Ok(match op {
        "+" => RValue::Num(x + y),
        "-" => RValue::Num(x - y),
        "*" => RValue::Num(x * y),
        "/" => RValue::Num(x / y),
        "%%" | "%" => RValue::Num(x % y),
        "%/%" => RValue::Num((x / y).floor()),
        "^" | "**" => RValue::Num(x.powf(y)),
        "==" => RValue::Bool(x == y),
        "!=" => RValue::Bool(x != y),
        "<" => RValue::Bool(x < y),
        "<=" => RValue::Bool(x <= y),
        ">" => RValue::Bool(x > y),
        ">=" => RValue::Bool(x >= y),
        "&" | "&&" => RValue::Bool(a.truth() && b.truth()),
        "|" | "||" => RValue::Bool(a.truth() || b.truth()),
        ":" => {
            let mut out = Vec::new();
            let step = if x <= y { 1.0 } else { -1.0 };
            let mut q = x;
            while if step > 0.0 { q <= y } else { q >= y } {
                out.push(RValue::Num(q));
                q += step;
            }
            RValue::Vec(out)
        }
        _ => return Err(RuntimeError(format!("unsupported binary operator: {op}"))),
    })
}

pub fn r_call(kernel: &str, name: &str, args: Vec<RValue>) -> Result<RValue, RuntimeError> {
    if let Some(op) = name.strip_prefix("__binary_") {
        let a = args
            .first()
            .ok_or_else(|| RuntimeError("binary call missing lhs".into()))?;
        let b = args
            .get(1)
            .ok_or_else(|| RuntimeError("binary call missing rhs".into()))?;
        return binary(op, a, b);
    }
    if let Some(op) = name.strip_prefix("__unary_") {
        let a = args
            .first()
            .ok_or_else(|| RuntimeError("unary call missing operand".into()))?;
        return match op {
            "!" => Ok(RValue::Bool(!a.truth())),
            "-" => Ok(RValue::Num(-a.number())),
            "+" => Ok(RValue::Num(a.number())),
            _ => Err(RuntimeError(format!("unsupported unary operator: {op}"))),
        };
    }
    match name {
        "c" | "list" => Ok(RValue::Vec(args)),
        "length" => Ok(RValue::Num(
            args.first()
                .cloned()
                .unwrap_or(RValue::Null)
                .into_iter_values()
                .len() as f64,
        )),
        "[" | "[[" => {
            let seq = args
                .first()
                .cloned()
                .unwrap_or(RValue::Null)
                .into_iter_values();
            let one_based = args.get(1).map(RValue::number).unwrap_or(f64::NAN) as isize;
            if one_based >= 1 && (one_based as usize) <= seq.len() {
                Ok(seq[one_based as usize - 1].clone())
            } else {
                Ok(RValue::Null)
            }
        }
        "print" => Ok(args.first().cloned().unwrap_or(RValue::Null)),
        _ if kernel == "predicate" || kernel == "numeric-predicate" || kernel == "missingness" => {
            Ok(RValue::Bool(false))
        }
        _ => Err(RuntimeError(format!(
            "runtime primitive not yet implemented: {kernel}/{name}"
        ))),
    }
}
