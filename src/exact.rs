use std::fmt;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct RExact {
    pub raw: u128,
    pub bits: u32,
    pub signed: bool,
}
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ExactValue {
    Integer(RExact),
    Bool(bool),
    Text(String),
}
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ExactError {
    Width,
    Parse,
    Operand,
    TypeMismatch,
    DivideByZero,
    NegativeShift,
    Unsupported(String),
    Overflow,
}
impl fmt::Display for ExactError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{:?}", self)
    }
}
impl std::error::Error for ExactError {}

impl RExact {
    pub fn new(value: i128, bits: u32, signed: bool) -> Result<Self, ExactError> {
        if bits == 0 || bits > 64 {
            return Err(ExactError::Width);
        };
        let raw = (value as u128) & ((1u128 << bits) - 1);
        Ok(Self { raw, bits, signed })
    }
    pub fn from_raw(raw: u128, bits: u32, signed: bool) -> Result<Self, ExactError> {
        if bits == 0 || bits > 64 {
            return Err(ExactError::Width);
        };
        Ok(Self {
            raw: raw & ((1u128 << bits) - 1),
            bits,
            signed,
        })
    }
}
pub fn exact_value(x: RExact) -> i128 {
    if x.signed && x.raw >= (1u128 << (x.bits - 1)) {
        (x.raw as i128) - (1i128 << x.bits)
    } else {
        x.raw as i128
    }
}
fn check(x: RExact, bits: u32, signed: bool) -> Result<RExact, ExactError> {
    if x.bits == bits && x.signed == signed {
        Ok(x)
    } else {
        Err(ExactError::TypeMismatch)
    }
}

pub fn r_exact(
    name: &str,
    bits: u32,
    signed: bool,
    text: &str,
    values: &[RExact],
) -> Result<ExactValue, ExactError> {
    if name == "integer.literal" {
        let v = text.parse::<i128>().map_err(|_| ExactError::Parse)?;
        return Ok(ExactValue::Integer(RExact::new(v, bits, signed)?));
    }
    let a = *values.first().ok_or(ExactError::Operand)?;
    let a = if name == "integer.convert" {
        a
    } else {
        check(a, bits, signed)?
    };
    let av = exact_value(a);
    match name {
        "integer.value" => return Ok(ExactValue::Integer(a)),
        "integer.convert" => return Ok(ExactValue::Integer(RExact::new(av, bits, signed)?)),
        "integer.format" => return Ok(ExactValue::Text(av.to_string())),
        "integer.negate" => {
            return Ok(ExactValue::Integer(RExact::new(
                av.wrapping_neg(),
                bits,
                signed,
            )?))
        }
        "integer.complement" => {
            return Ok(ExactValue::Integer(RExact::from_raw(!a.raw, bits, signed)?))
        }
        _ => {}
    }
    let b = check(*values.get(1).ok_or(ExactError::Operand)?, bits, signed)?;
    let bv = exact_value(b);
    let integer = |raw: u128| RExact::from_raw(raw, bits, signed).map(ExactValue::Integer);
    match name {
        "integer.equal" => Ok(ExactValue::Bool(a.raw == b.raw)),
        "integer.not_equal" => Ok(ExactValue::Bool(a.raw != b.raw)),
        "integer.less" => Ok(ExactValue::Bool(av < bv)),
        "integer.less_equal" => Ok(ExactValue::Bool(av <= bv)),
        "integer.greater" => Ok(ExactValue::Bool(av > bv)),
        "integer.greater_equal" => Ok(ExactValue::Bool(av >= bv)),
        "integer.add" => integer(a.raw.wrapping_add(b.raw)),
        "integer.subtract" => integer(a.raw.wrapping_sub(b.raw)),
        "integer.multiply" => integer(a.raw.wrapping_mul(b.raw)),
        "integer.and" => integer(a.raw & b.raw),
        "integer.or" => integer(a.raw | b.raw),
        "integer.xor" => integer(a.raw ^ b.raw),
        "integer.and_not" => integer(a.raw & !b.raw),
        "integer.shift_left" => {
            if bv < 0 {
                return Err(ExactError::NegativeShift);
            };
            integer(if bv as u32 >= bits {
                0
            } else {
                a.raw.wrapping_shl(bv as u32)
            })
        }
        "integer.shift_right" => {
            if bv < 0 {
                return Err(ExactError::NegativeShift);
            };
            let v = if bv as u32 >= bits {
                if signed && av < 0 {
                    -1
                } else {
                    0
                }
            } else {
                av >> bv as u32
            };
            Ok(ExactValue::Integer(RExact::new(v, bits, signed)?))
        }
        "integer.divide" => {
            if bv == 0 {
                return Err(ExactError::DivideByZero);
            };
            let q = match av.checked_div(bv) {
                Some(v) => v,
                None if signed && bv == -1 => av,
                None => return Err(ExactError::Overflow),
            };
            Ok(ExactValue::Integer(RExact::new(q, bits, signed)?))
        }
        "integer.remainder" => {
            if bv == 0 {
                return Err(ExactError::DivideByZero);
            };
            let r = match av.checked_rem(bv) {
                Some(v) => v,
                None if signed && bv == -1 => 0,
                None => return Err(ExactError::Overflow),
            };
            Ok(ExactValue::Integer(RExact::new(r, bits, signed)?))
        }
        _ => Err(ExactError::Unsupported(name.into())),
    }
}
