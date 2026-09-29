use crate::ir::Type;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[repr(u64)]
pub enum NativeValueTag {
    Invalid = 0,
    Integer = 1,
    Unsigned = 2,
    Float = 3,
    Bool = 4,
    String = 5,
    Pointer = 6,
}
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct NativeValue {
    pub tag: NativeValueTag,
    pub payload: u64,
}
impl NativeValue {
    pub fn integer(v: i64) -> Self {
        Self {
            tag: NativeValueTag::Integer,
            payload: v as u64,
        }
    }
    pub fn unsigned(v: u64) -> Self {
        Self {
            tag: NativeValueTag::Unsigned,
            payload: v,
        }
    }
    pub fn pointer(v: usize) -> Self {
        Self {
            tag: NativeValueTag::Pointer,
            payload: v as u64,
        }
    }
    pub fn string_ptr(v: usize) -> Self {
        Self {
            tag: NativeValueTag::String,
            payload: v as u64,
        }
    }
    pub fn float_bits(v: u64) -> Self {
        Self {
            tag: NativeValueTag::Float,
            payload: v,
        }
    }
    pub fn boolean(v: bool) -> Self {
        Self {
            tag: NativeValueTag::Bool,
            payload: if v { 1 } else { 0 },
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct NativeTargetProfile {
    pub os: String,
    pub isa: String,
    pub object_format: String,
    pub abi: String,
    pub endianness: String,
    pub system_abi: String,
    pub pointer_size: usize,
    pub stack_alignment: usize,
    pub function_pointer_alignment: usize,
    pub index_bit_width: usize,
    pub legal_integer_widths: Vec<usize>,
}
pub fn native_windows_x64_target_profile() -> NativeTargetProfile {
    NativeTargetProfile {
        os: "windows".into(),
        isa: "x86_64".into(),
        object_format: "PE32+".into(),
        abi: "microsoft-x64".into(),
        endianness: "little".into(),
        system_abi: "kernel32".into(),
        pointer_size: 8,
        stack_alignment: 16,
        function_pointer_alignment: 8,
        index_bit_width: 64,
        legal_integer_widths: vec![8, 16, 32, 64],
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum NativeRepresentation {
    Scalar,
    Address,
    Pair,
    Descriptor,
    Aggregate,
    OwnedRegion,
    FunctionReference,
    ClosureValue,
}
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum NativeAbiContext {
    Value,
    Argument,
    Result,
    Capture,
}
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct NativeLayout {
    pub representation: NativeRepresentation,
    pub size_bits: usize,
    pub size_bytes: usize,
    pub abi_alignment: usize,
    pub preferred_alignment: usize,
    pub field_offsets: Vec<usize>,
    pub stride: usize,
    pub index_bit_width: usize,
    pub pointer_size: usize,
    pub stack_alignment: usize,
    pub endianness: String,
    pub memory_space: String,
}

pub fn solve_native_layout(
    profile: &NativeTargetProfile,
    ty: &Type,
    _context: NativeAbiContext,
) -> Result<NativeLayout, String> {
    if profile.pointer_size == 0 || profile.stack_alignment == 0 {
        return Err("invalid target profile".into());
    }
    let pointer = profile.pointer_size;
    let mut l = NativeLayout {
        representation: NativeRepresentation::Scalar,
        size_bits: 0,
        size_bytes: 0,
        abi_alignment: 1,
        preferred_alignment: 1,
        field_offsets: Vec::new(),
        stride: 0,
        index_bit_width: profile.index_bit_width,
        pointer_size: pointer,
        stack_alignment: profile.stack_alignment,
        endianness: profile.endianness.clone(),
        memory_space: "default".into(),
    };
    match ty {
        Type::Unit => {
            l.size_bits = pointer * 8;
            l.size_bytes = pointer;
            l.abi_alignment = pointer;
            l.preferred_alignment = pointer;
        }
        Type::Bool => {
            l.size_bits = 8;
            l.size_bytes = 1;
        }
        Type::Int | Type::UInt => {
            l.size_bits = profile.index_bit_width;
            l.size_bytes = profile.index_bit_width / 8;
            l.abi_alignment = l.size_bytes.min(pointer);
            l.preferred_alignment = l.abi_alignment;
        }
        Type::Int8 | Type::UInt8 => {
            l.size_bits = 8;
            l.size_bytes = 1;
        }
        Type::Int16 | Type::UInt16 => {
            l.size_bits = 16;
            l.size_bytes = 2;
            l.abi_alignment = 2.min(pointer);
            l.preferred_alignment = l.abi_alignment;
        }
        Type::Int32 | Type::UInt32 | Type::Float32 => {
            l.size_bits = 32;
            l.size_bytes = 4;
            l.abi_alignment = 4.min(pointer);
            l.preferred_alignment = l.abi_alignment;
        }
        Type::Int64 | Type::UInt64 | Type::Float64 => {
            l.size_bits = 64;
            l.size_bytes = 8;
            l.abi_alignment = 8.min(pointer);
            l.preferred_alignment = l.abi_alignment;
        }
        Type::String => {
            l.representation = NativeRepresentation::Pair;
            l.size_bits = pointer * 16;
            l.size_bytes = pointer * 2;
            l.abi_alignment = pointer;
            l.preferred_alignment = pointer;
            l.field_offsets = vec![0, pointer];
        }
        Type::Slice(_) | Type::Map(_, _) => {
            l.representation = NativeRepresentation::Descriptor;
            l.size_bits = pointer * 16;
            l.size_bytes = pointer * 2;
            l.abi_alignment = pointer;
            l.preferred_alignment = pointer;
            l.field_offsets = vec![0, pointer];
        }
        Type::Tuple(items) => {
            let mut offset = 0usize;
            let mut max_align = 1usize;
            l.representation = NativeRepresentation::Aggregate;
            for item in items {
                let field = solve_native_layout(profile, item, _context)?;
                let align = field.abi_alignment.max(1);
                offset = offset.div_ceil(align) * align;
                l.field_offsets.push(offset);
                offset += field.size_bytes;
                max_align = max_align.max(align);
            }
            offset = offset.div_ceil(max_align) * max_align;
            l.size_bytes = offset;
            l.size_bits = offset * 8;
            l.abi_alignment = max_align;
            l.preferred_alignment = max_align;
        }
        Type::Named(name) => {
            return Err(format!(
                "NATIVE_LAYOUT_GAP: named type {name:?} requires resolved semantic layout"
            ))
        }
    }
    l.stride = l.size_bytes;
    if l.preferred_alignment < l.abi_alignment {
        return Err("invalid layout alignment".into());
    }
    Ok(l)
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum NativeStorageDecision {
    Static,
    Stack,
    OwnedRegion,
    Unresolved,
}
#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct NativeStorageRequest {
    pub escapes: bool,
    pub program_static: bool,
    pub ownership_known: bool,
    pub alias_mutation_safe: bool,
    pub dynamic_size: bool,
    pub lifetime: String,
}
pub fn decide_native_storage(r: &NativeStorageRequest) -> NativeStorageDecision {
    if r.program_static && !r.escapes && !r.dynamic_size {
        return NativeStorageDecision::Static;
    }
    if !r.escapes && matches!(r.lifetime.as_str(), "frame" | "block") && r.alias_mutation_safe {
        return NativeStorageDecision::Stack;
    }
    if r.escapes && r.ownership_known && r.alias_mutation_safe {
        return NativeStorageDecision::OwnedRegion;
    }
    NativeStorageDecision::Unresolved
}
