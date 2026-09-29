#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FrontendSpec {
    pub id: &'static str,
    pub aliases: &'static [&'static str],
    pub extensions: &'static [&'static str],
    pub capabilities: &'static [&'static str],
}
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BackendSpec {
    pub id: &'static str,
    pub aliases: &'static [&'static str],
    pub capabilities: &'static [&'static str],
}

const FRONTENDS: &[FrontendSpec] = &[
    FrontendSpec {
        id: "r",
        aliases: &["r"],
        extensions: &[".r", ".R"],
        capabilities: &[
            "core",
            "lazy_evaluation",
            "named_arguments",
            "one_based_index",
        ],
    },
    FrontendSpec {
        id: "go",
        aliases: &["go"],
        extensions: &[".go"],
        capabilities: &["core", "eager_evaluation"],
    },
    FrontendSpec {
        id: "python",
        aliases: &["python", "py"],
        extensions: &[".py"],
        capabilities: &["core", "eager_evaluation", "named_arguments"],
    },
    FrontendSpec {
        id: "rust",
        aliases: &["rust", "rs"],
        extensions: &[".rs"],
        capabilities: &["core", "eager_evaluation"],
    },
    FrontendSpec {
        id: "c",
        aliases: &["c"],
        extensions: &[".c", ".h"],
        capabilities: &["core", "eager_evaluation"],
    },
    FrontendSpec {
        id: "cpp",
        aliases: &["cpp", "c++"],
        extensions: &[".cpp", ".cc", ".cxx", ".hpp"],
        capabilities: &["core", "eager_evaluation"],
    },
    FrontendSpec {
        id: "zig",
        aliases: &["zig"],
        extensions: &[".zig"],
        capabilities: &["core", "eager_evaluation"],
    },
    FrontendSpec {
        id: "julia",
        aliases: &["julia", "jl"],
        extensions: &[".jl"],
        capabilities: &["core", "eager_evaluation"],
    },
    FrontendSpec {
        id: "se",
        aliases: &["se", "semantic", "semantic-exchange"],
        extensions: &[".se"],
        capabilities: &["core", "semantic_exchange"],
    },
    FrontendSpec {
        id: "nim",
        aliases: &["nim"],
        extensions: &[".nim"],
        capabilities: &["core", "eager_evaluation"],
    },
    FrontendSpec {
        id: "csharp",
        aliases: &["csharp", "c#", "cs"],
        extensions: &[".cs"],
        capabilities: &["core", "eager_evaluation"],
    },
    FrontendSpec {
        id: "java",
        aliases: &["java"],
        extensions: &[".java"],
        capabilities: &["core", "eager_evaluation"],
    },
    FrontendSpec {
        id: "kotlin",
        aliases: &["kotlin", "kt"],
        extensions: &[".kt", ".kts"],
        capabilities: &["core", "eager_evaluation"],
    },
    FrontendSpec {
        id: "swift",
        aliases: &["swift"],
        extensions: &[".swift"],
        capabilities: &["core", "eager_evaluation"],
    },
];

const BACKENDS: &[BackendSpec] = &[
    BackendSpec {
        id: "r",
        aliases: &["r"],
        capabilities: &["core"],
    },
    BackendSpec {
        id: "go",
        aliases: &["go"],
        capabilities: &["core"],
    },
    BackendSpec {
        id: "python",
        aliases: &["python", "py"],
        capabilities: &["core"],
    },
    BackendSpec {
        id: "rust",
        aliases: &["rust", "rs"],
        capabilities: &["core"],
    },
    BackendSpec {
        id: "c",
        aliases: &["c"],
        capabilities: &["core"],
    },
    BackendSpec {
        id: "cpp",
        aliases: &["cpp", "c++"],
        capabilities: &["core"],
    },
    BackendSpec {
        id: "zig",
        aliases: &["zig"],
        capabilities: &["core"],
    },
    BackendSpec {
        id: "julia",
        aliases: &["julia", "jl"],
        capabilities: &["core"],
    },
    BackendSpec {
        id: "se",
        aliases: &["se", "semantic", "semantic-exchange"],
        capabilities: &["core", "semantic_exchange"],
    },
    BackendSpec {
        id: "nim",
        aliases: &["nim"],
        capabilities: &["core"],
    },
    BackendSpec {
        id: "csharp",
        aliases: &["csharp", "c#", "cs"],
        capabilities: &["core"],
    },
    BackendSpec {
        id: "java",
        aliases: &["java"],
        capabilities: &["core"],
    },
    BackendSpec {
        id: "kotlin",
        aliases: &["kotlin", "kt"],
        capabilities: &["core"],
    },
    BackendSpec {
        id: "swift",
        aliases: &["swift"],
        capabilities: &["core"],
    },
];

pub fn frontends() -> &'static [FrontendSpec] {
    FRONTENDS
}
pub fn backends() -> &'static [BackendSpec] {
    BACKENDS
}
pub fn normalize_language(name: &str) -> String {
    let n = name.trim().to_ascii_lowercase();
    FRONTENDS
        .iter()
        .find(|s| s.aliases.iter().any(|a| *a == n))
        .map(|s| s.id.to_string())
        .unwrap_or(n)
}
pub fn has_frontend(name: &str) -> bool {
    let n = normalize_language(name);
    FRONTENDS.iter().any(|s| s.id == n)
}
pub fn has_backend(name: &str) -> bool {
    let n = normalize_language(name);
    BACKENDS.iter().any(|s| s.id == n)
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CapabilityStatus {
    Native,
    Lowering,
    Emulated,
    Unsupported,
}
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CapabilityResult {
    pub feature: String,
    pub backend: String,
    pub status: CapabilityStatus,
    pub reason: String,
}

pub fn backend_capability(feature: &str, backend: &str) -> CapabilityResult {
    let b = normalize_language(backend);
    if !has_backend(&b) {
        return CapabilityResult {
            feature: feature.into(),
            backend: b,
            status: CapabilityStatus::Unsupported,
            reason: "unknown backend".into(),
        };
    }
    let integer = feature.starts_with("integer.")
        || feature.starts_with("native.integer.")
        || feature.starts_with("fixed_width_integer.");
    if integer && ["go", "python", "c", "rust", "cpp", "java", "csharp"].contains(&b.as_str()) {
        return CapabilityResult {
            feature: feature.into(),
            backend: b,
            status: CapabilityStatus::Lowering,
            reason: "fixed-width integer operations with explicit wrap semantics".into(),
        };
    }
    if feature == "core" {
        return CapabilityResult {
            feature: feature.into(),
            backend: b,
            status: CapabilityStatus::Lowering,
            reason: "shared semantic core lowering".into(),
        };
    }
    CapabilityResult {
        feature: feature.into(),
        backend: b,
        status: CapabilityStatus::Unsupported,
        reason: "backend has no declared capability".into(),
    }
}
