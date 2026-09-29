use crate::{emit, go_ir};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct NativeTranspileError(pub String);
impl std::fmt::Display for NativeTranspileError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.0)
    }
}
impl std::error::Error for NativeTranspileError {}

pub fn native_go_to_target(code: &str, target: &str) -> Result<String, NativeTranspileError> {
    let program = go_ir::parse_go(code).map_err(|e| NativeTranspileError(e.to_string()))?;
    emit::emit(&program, target).map_err(|e| NativeTranspileError(e.to_string()))
}

pub fn native_go_to_rust(code: &str) -> Result<String, NativeTranspileError> {
    native_go_to_target(code, "rust")
}
