//! Pure-Rust incremental port of Code-Transpiler.
//!
//! No Go executable, DLL, external compiler process, or `CODETRANSPILER_BACKEND`
//! is used. The native core currently parses a conservative Go subset into a
//! neutral IR and emits Rust, Python, Julia, C, and C++ while failing closed for
//! syntax whose semantics are not yet implemented.

mod call_resolution;
mod capability_matrix;
mod emit;
mod exact;
mod go_ir;
mod gui;
mod ir;
mod julia_ir;
mod matrix;
mod native_abi;
mod native_go;
mod python_frontend;
mod registry;
mod runtime;
mod semantic;
mod semantic_exchange;
mod signature;
mod target;
mod type_conversion;

pub use call_resolution::{
    validate_call_resolution, CallCandidate, CallResolution, EXACT_CALL_RESOLUTION_CAPABILITY,
};
pub use capability_matrix::{semantic_capability_matrix, CapabilityMatrix};
pub use exact::{exact_value, r_exact, ExactError, ExactValue, RExact};
pub use go_ir::{parse_expr as parse_go_expr, parse_go, GoParseError};
pub use gui::{gui, serve_gui, GuiHandle};
pub use ir::{Expr, Function, Param, Program, Stmt, Type};
pub use julia_ir::{parse_julia, parse_julia_expr, JuliaParseError};
pub use matrix::{Matrix, SparseMatrix};
pub use native_abi::{
    decide_native_storage, native_windows_x64_target_profile, solve_native_layout,
    NativeAbiContext, NativeLayout, NativeRepresentation, NativeStorageDecision,
    NativeStorageRequest, NativeTargetProfile, NativeValue, NativeValueTag,
};
pub use native_go::{native_go_to_rust, native_go_to_target, NativeTranspileError};
pub use python_frontend::{parse_python, python_to_julia_pivot, PythonParseError};
pub use registry::{
    backend_capability, backends, frontends, has_backend, has_frontend, normalize_language,
    BackendSpec, CapabilityResult, CapabilityStatus, FrontendSpec,
};
pub use runtime::{r_bind, r_call, RValue, RuntimeError};
pub use semantic::{
    missing_structured_fields, snapshot_iteration, structured_input_fields, summarize_effect_axes,
    SemanticEffectSummary, StructuredConstructInput,
};
pub use semantic_exchange::{
    semantic_exchange, semantic_exchange_pivot, semantic_json, semantic_program, semantic_text,
    SemanticBindingInfo, SemanticFunctionInfo, SemanticOperationInfo, SemanticProgram, SE_MAGIC,
};
pub use signature::{
    bind_signature, validate_signature_contract, FunctionSignatureContract, SignatureArgument,
    SignatureBinding, SignatureParameter, EXACT_SIGNATURE_CAPABILITY,
};
pub use target::{
    emit_dispatch, target_inf, target_na, target_name, PreservationMode, PreservationRegistry,
    PreservationRule, Requirement, RequirementKind, RequirementRegistry,
};
pub use type_conversion::{
    resolve_universal_type_conversion, universal_type_conversion_registry, LoweringExactness,
    TypeConversionRecipe, TypeConversionStatus,
};

use std::fs;
use std::io;
use std::path::{Path, PathBuf};
use std::process::Command;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Language {
    pub id: &'static str,
    pub name: &'static str,
    pub extensions: &'static [&'static str],
    pub aliases: &'static [&'static str],
}

const LANGUAGES: &[Language] = &[
    Language {
        id: "r",
        name: "R",
        extensions: &[".R", ".r"],
        aliases: &[],
    },
    Language {
        id: "go",
        name: "Go",
        extensions: &[".go"],
        aliases: &[],
    },
    Language {
        id: "rust",
        name: "Rust",
        extensions: &[".rs"],
        aliases: &["rs"],
    },
    Language {
        id: "cpp",
        name: "C++",
        extensions: &[".cpp", ".cc", ".cxx"],
        aliases: &["c++"],
    },
    Language {
        id: "c",
        name: "C",
        extensions: &[".c", ".h"],
        aliases: &[],
    },
    Language {
        id: "python",
        name: "Python",
        extensions: &[".py"],
        aliases: &["py"],
    },
    Language {
        id: "zig",
        name: "Zig",
        extensions: &[".zig"],
        aliases: &[],
    },
    Language {
        id: "julia",
        name: "Julia",
        extensions: &[".jl"],
        aliases: &["jl"],
    },
    Language {
        id: "se",
        name: "Semantic Exchange",
        extensions: &[".se"],
        aliases: &["semantic", "semantic-exchange"],
    },
    Language {
        id: "nim",
        name: "Nim",
        extensions: &[".nim"],
        aliases: &[],
    },
    Language {
        id: "csharp",
        name: "C#",
        extensions: &[".cs"],
        aliases: &["c#", "cs"],
    },
    Language {
        id: "java",
        name: "Java",
        extensions: &[".java"],
        aliases: &[],
    },
    Language {
        id: "kotlin",
        name: "Kotlin",
        extensions: &[".kt", ".kts"],
        aliases: &["kt"],
    },
    Language {
        id: "swift",
        name: "Swift",
        extensions: &[".swift"],
        aliases: &[],
    },
];

const NATIVE_TARGETS_FROM_GO: &[&str] = &["rust", "python", "julia", "c", "cpp", "se"];
const NATIVE_TARGETS_FROM_JULIA: &[&str] = &["rust", "python", "c", "cpp", "se"];
const NATIVE_TARGETS_FROM_PYTHON: &[&str] = &["rust", "julia", "c", "cpp", "se"];
const NATIVE_TARGETS_FROM_SE: &[&str] = &["rust", "python", "julia", "c", "cpp"];

pub fn languages() -> &'static [Language] {
    LANGUAGES
}

/// Routes implemented without an external fallback.
pub fn routes() -> Vec<(&'static str, &'static str)> {
    let mut out = Vec::new();
    out.extend(NATIVE_TARGETS_FROM_GO.iter().map(|t| ("go", *t)));
    out.extend(NATIVE_TARGETS_FROM_JULIA.iter().map(|t| ("julia", *t)));
    out.extend(NATIVE_TARGETS_FROM_PYTHON.iter().map(|t| ("python", *t)));
    out.extend(NATIVE_TARGETS_FROM_SE.iter().map(|t| ("se", *t)));
    out
}

pub fn targets_for_source(source: &str) -> Vec<&'static str> {
    match canonical_language(source) {
        Ok("go") => NATIVE_TARGETS_FROM_GO.to_vec(),
        Ok("julia") => NATIVE_TARGETS_FROM_JULIA.to_vec(),
        Ok("python") => NATIVE_TARGETS_FROM_PYTHON.to_vec(),
        Ok("se") => NATIVE_TARGETS_FROM_SE.to_vec(),
        _ => Vec::new(),
    }
}

pub fn source_languages() -> Vec<&'static Language> {
    LANGUAGES.iter().collect()
}
pub fn route_supported(source: &str, target: &str) -> bool {
    match (canonical_language(source), canonical_language(target)) {
        (Ok(a), Ok(b)) if a == b => true,
        (Ok("go"), Ok(b)) => NATIVE_TARGETS_FROM_GO.contains(&b),
        (Ok("julia"), Ok(b)) => NATIVE_TARGETS_FROM_JULIA.contains(&b),
        (Ok("python"), Ok(b)) => NATIVE_TARGETS_FROM_PYTHON.contains(&b),
        (Ok("se"), Ok(b)) => NATIVE_TARGETS_FROM_SE.contains(&b),
        _ => false,
    }
}

#[derive(Debug)]
pub enum TranspileError {
    UnsupportedLanguage(String),
    UnsupportedRoute { source: String, target: String },
    Native(String),
    Io(io::Error),
}
impl std::fmt::Display for TranspileError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::UnsupportedLanguage(x) => write!(f, "unsupported language: {x}"),
            Self::UnsupportedRoute { source, target } => {
                write!(f, "native route not implemented: {source} -> {target}")
            }
            Self::Native(x) => write!(f, "native transpilation failed: {x}"),
            Self::Io(e) => write!(f, "I/O error: {e}"),
        }
    }
}
impl std::error::Error for TranspileError {}
impl From<io::Error> for TranspileError {
    fn from(value: io::Error) -> Self {
        Self::Io(value)
    }
}

fn canonical_language(id: &str) -> Result<&'static str, TranspileError> {
    let needle = id.trim().to_ascii_lowercase();
    for lang in LANGUAGES {
        if needle == lang.id || lang.aliases.iter().any(|a| needle == *a) {
            return Ok(lang.id);
        }
    }
    Err(TranspileError::UnsupportedLanguage(id.to_string()))
}

pub fn backend_status() -> (&'static str, bool) {
    ("native-rust", true)
}

pub fn transpile(source: &str, target: &str, code: &str) -> Result<String, TranspileError> {
    let src = canonical_language(source)?;
    let dst = canonical_language(target)?;
    if src == dst {
        return Ok(code.to_string());
    }
    if src == "go" && NATIVE_TARGETS_FROM_GO.contains(&dst) {
        if dst == "se" {
            let program = parse_go(code).map_err(|e| TranspileError::Native(e.to_string()))?;
            return semantic_exchange("go", &program).map_err(TranspileError::Native);
        }
        return native_go_to_target(code, dst).map_err(|e| TranspileError::Native(e.to_string()));
    }
    if src == "julia" && NATIVE_TARGETS_FROM_JULIA.contains(&dst) {
        let program = parse_julia(code).map_err(|e| TranspileError::Native(e.to_string()))?;
        if dst == "se" {
            return semantic_exchange("julia", &program).map_err(TranspileError::Native);
        }
        return crate::emit::emit(&program, dst).map_err(|e| TranspileError::Native(e.to_string()));
    }
    if src == "python" && NATIVE_TARGETS_FROM_PYTHON.contains(&dst) {
        let program = parse_python(code).map_err(|e| TranspileError::Native(e.to_string()))?;
        if dst == "se" {
            return semantic_exchange("python", &program).map_err(TranspileError::Native);
        }
        return crate::emit::emit(&program, dst).map_err(|e| TranspileError::Native(e.to_string()));
    }
    if src == "se" && NATIVE_TARGETS_FROM_SE.contains(&dst) {
        let pivot = semantic_exchange_pivot(code).map_err(TranspileError::Native)?;
        if dst == "julia" {
            return Ok(pivot);
        }
        let program = parse_julia(&pivot).map_err(|e| TranspileError::Native(e.to_string()))?;
        return crate::emit::emit(&program, dst).map_err(|e| TranspileError::Native(e.to_string()));
    }
    Err(TranspileError::UnsupportedRoute {
        source: src.to_string(),
        target: dst.to_string(),
    })
}

pub fn transpile_file(
    input: impl AsRef<Path>,
    output: impl AsRef<Path>,
    source: Option<&str>,
    target: &str,
) -> Result<PathBuf, TranspileError> {
    let input = input.as_ref();
    let src_owned;
    let src = if let Some(s) = source {
        s
    } else {
        src_owned = language_from_path(input)?.to_string();
        &src_owned
    };
    let generated = transpile(src, target, &fs::read_to_string(input)?)?;
    if let Some(parent) = output.as_ref().parent() {
        fs::create_dir_all(parent)?;
    }
    fs::write(output.as_ref(), generated)?;
    Ok(output.as_ref().to_path_buf())
}

fn language_from_path(path: &Path) -> Result<&'static str, TranspileError> {
    let name = path.to_string_lossy().to_ascii_lowercase();
    for lang in LANGUAGES {
        for ext in lang.extensions {
            if name.ends_with(&ext.to_ascii_lowercase()) {
                return Ok(lang.id);
            }
        }
    }
    Err(TranspileError::UnsupportedLanguage(name))
}

pub(crate) fn open_browser(url: &str) -> io::Result<()> {
    let mut cmd = if cfg!(windows) {
        let mut c = Command::new("cmd");
        c.args(["/C", "start", "", url]);
        c
    } else if cfg!(target_os = "macos") {
        let mut c = Command::new("open");
        c.arg(url);
        c
    } else {
        let mut c = Command::new("xdg-open");
        c.arg(url);
        c
    };
    cmd.spawn().map(|_| ())
}
