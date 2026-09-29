use crate::ir::Type;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TypeConversionStatus {
    Exact,
    Unresolved,
}
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum LoweringExactness {
    Exact,
}
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TypeConversionRecipe {
    pub id: String,
    pub from_kind: String,
    pub to_kind: String,
    pub target: String,
    pub guard: Vec<String>,
    pub preservation_class: LoweringExactness,
    pub status: TypeConversionStatus,
}
fn kind(t: &Type) -> &'static str {
    match t {
        Type::Unit => "unit",
        Type::Bool => "boolean",
        Type::String => "string",
        Type::Int
        | Type::Int8
        | Type::Int16
        | Type::Int32
        | Type::Int64
        | Type::UInt
        | Type::UInt8
        | Type::UInt16
        | Type::UInt32
        | Type::UInt64 => "integer",
        Type::Float32 | Type::Float64 => "float",
        Type::Slice(_) => "list",
        Type::Map(_, _) => "map",
        Type::Tuple(_) => "tuple",
        Type::Named(_) => "named",
    }
}
fn integer_shape(t: &Type) -> Option<(usize, bool)> {
    Some(match t {
        Type::Int => (usize::BITS as usize, true),
        Type::Int8 => (8, true),
        Type::Int16 => (16, true),
        Type::Int32 => (32, true),
        Type::Int64 => (64, true),
        Type::UInt => (usize::BITS as usize, false),
        Type::UInt8 => (8, false),
        Type::UInt16 => (16, false),
        Type::UInt32 => (32, false),
        Type::UInt64 => (64, false),
        _ => return None,
    })
}
fn float_bits(t: &Type) -> Option<usize> {
    match t {
        Type::Float32 => Some(32),
        Type::Float64 => Some(64),
        _ => None,
    }
}

pub fn universal_type_conversion_registry() -> Vec<TypeConversionRecipe> {
    vec![
        TypeConversionRecipe {
            id: "type.identity".into(),
            from_kind: "*".into(),
            to_kind: "*".into(),
            target: "*".into(),
            guard: vec![],
            preservation_class: LoweringExactness::Exact,
            status: TypeConversionStatus::Exact,
        },
        TypeConversionRecipe {
            id: "type.integer.same_width".into(),
            from_kind: "integer".into(),
            to_kind: "integer".into(),
            target: "*".into(),
            guard: vec![
                "bits_equal".into(),
                "signedness_equal".into(),
                "overflow_equal".into(),
            ],
            preservation_class: LoweringExactness::Exact,
            status: TypeConversionStatus::Exact,
        },
        TypeConversionRecipe {
            id: "type.float.same_precision".into(),
            from_kind: "float".into(),
            to_kind: "float".into(),
            target: "*".into(),
            guard: vec![
                "precision_equal".into(),
                "rounding_equal".into(),
                "nan_equal".into(),
            ],
            preservation_class: LoweringExactness::Exact,
            status: TypeConversionStatus::Exact,
        },
        TypeConversionRecipe {
            id: "type.list.elementwise".into(),
            from_kind: "list".into(),
            to_kind: "list".into(),
            target: "*".into(),
            guard: vec![
                "element_conversion_exact".into(),
                "order_equal".into(),
                "nullability_equal".into(),
                "mutability_equal".into(),
            ],
            preservation_class: LoweringExactness::Exact,
            status: TypeConversionStatus::Exact,
        },
    ]
}

pub fn resolve_universal_type_conversion(
    from: &Type,
    to: &Type,
    target: &str,
) -> Result<TypeConversionRecipe, String> {
    if target.is_empty() {
        return Err("TYPE_CONVERSION_UNRESOLVED: missing target".into());
    }
    if from == to {
        return Ok(TypeConversionRecipe {
            id: "type.identity".into(),
            from_kind: kind(from).into(),
            to_kind: kind(to).into(),
            target: target.into(),
            guard: vec![],
            preservation_class: LoweringExactness::Exact,
            status: TypeConversionStatus::Exact,
        });
    }
    if let (Some(a), Some(b)) = (integer_shape(from), integer_shape(to)) {
        if a != b {
            return Err("TYPE_CONVERSION_UNRESOLVED: integer width/signedness".into());
        }
        return Ok(TypeConversionRecipe {
            id: "type.integer.same_width".into(),
            from_kind: "integer".into(),
            to_kind: "integer".into(),
            target: target.into(),
            guard: vec![
                "bits_equal".into(),
                "signedness_equal".into(),
                "overflow_equal".into(),
            ],
            preservation_class: LoweringExactness::Exact,
            status: TypeConversionStatus::Exact,
        });
    }
    if let (Some(a), Some(b)) = (float_bits(from), float_bits(to)) {
        if a != b {
            return Err("TYPE_CONVERSION_UNRESOLVED: float precision".into());
        }
        return Ok(TypeConversionRecipe {
            id: "type.float.same_precision".into(),
            from_kind: "float".into(),
            to_kind: "float".into(),
            target: target.into(),
            guard: vec![
                "precision_equal".into(),
                "rounding_equal".into(),
                "nan_equal".into(),
            ],
            preservation_class: LoweringExactness::Exact,
            status: TypeConversionStatus::Exact,
        });
    }
    if let (Type::Slice(a), Type::Slice(b)) = (from, to) {
        resolve_universal_type_conversion(a, b, target)?;
        return Ok(TypeConversionRecipe {
            id: "type.list.elementwise".into(),
            from_kind: "list".into(),
            to_kind: "list".into(),
            target: target.into(),
            guard: vec![
                "element_conversion_exact".into(),
                "order_equal".into(),
                "nullability_equal".into(),
                "mutability_equal".into(),
            ],
            preservation_class: LoweringExactness::Exact,
            status: TypeConversionStatus::Exact,
        });
    }
    Err(format!(
        "TYPE_CONVERSION_UNRESOLVED: {} -> {}",
        kind(from),
        kind(to)
    ))
}
