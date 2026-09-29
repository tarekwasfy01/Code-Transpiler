"""Declarative frontend description ported from the Go backend registry."""
struct FrontendSpec
    id::String
    aliases::Vector{String}
    extensions::Vector{String}
    capabilities::Vector{String}
    dialects::Vector{String}
end

"""Declarative backend description ported from the Go backend registry."""
struct BackendSpec
    id::String
    aliases::Vector{String}
    capabilities::Vector{String}
    dialects::Vector{String}
end

@enum CapabilityStatus begin
    CapabilityNative
    CapabilityLowering
    CapabilityEmulated
    CapabilityUnsupported
end

struct CapabilityResult
    feature::String
    backend::String
    status::CapabilityStatus
    reason::String
end

const _FRONTEND_REGISTRY = FrontendSpec[
    FrontendSpec("se",      ["se", "semantic", "semantics", "semantic-exchange"], [".se"], ["core", "semantic_exchange", "one_based_index"], ["core", "se1"]),
    FrontendSpec("r",       ["r"],             [".r", ".R"],                 ["core", "lazy_evaluation", "named_arguments", "one_based_index"], ["core", "r"]),
    FrontendSpec("go",      ["go"],            [".go"],                       ["core", "eager_evaluation"], ["core"]),
    FrontendSpec("python",  ["python", "py"], [".py"],                       ["core", "eager_evaluation", "named_arguments"], ["core"]),
    FrontendSpec("rust",    ["rust", "rs"],   [".rs"],                       ["core", "eager_evaluation"], ["core"]),
    FrontendSpec("c",       ["c"],             [".c", ".h"],                 ["core", "eager_evaluation"], ["core"]),
    FrontendSpec("cpp",     ["cpp", "c++"],   [".cpp", ".cc", ".cxx", ".hpp"], ["core", "eager_evaluation"], ["core"]),
    FrontendSpec("zig",     ["zig"],           [".zig"],                      ["core", "eager_evaluation"], ["core"]),
    FrontendSpec("julia",   ["julia", "jl"], [".jl"],                       ["core", "eager_evaluation"], ["core"]),
    FrontendSpec("nim",     ["nim"],           [".nim"],                      ["core", "eager_evaluation"], ["core"]),
    FrontendSpec("csharp",  ["csharp", "c#", "cs"], [".cs"],              ["core", "eager_evaluation"], ["core"]),
    FrontendSpec("java",    ["java"],          [".java"],                     ["core", "eager_evaluation"], ["core"]),
    FrontendSpec("kotlin",  ["kotlin", "kt"], [".kt", ".kts"],             ["core", "eager_evaluation"], ["core"]),
    FrontendSpec("swift",   ["swift"],         [".swift"],                    ["core", "eager_evaluation"], ["core"]),
]

const _BACKEND_REGISTRY = BackendSpec[
    BackendSpec(spec.id, copy(spec.aliases), ["core"], ["core"]) for spec in _FRONTEND_REGISTRY
]

frontends() = deepcopy(_FRONTEND_REGISTRY)
backends() = deepcopy(_BACKEND_REGISTRY)

function normalize_language(name::AbstractString)
    needle = lowercase(strip(String(name)))
    for spec in _FRONTEND_REGISTRY
        needle in lowercase.(spec.aliases) && return spec.id
    end
    return needle
end

has_frontend(name::AbstractString) = any(s -> s.id == normalize_language(name), _FRONTEND_REGISTRY)
has_backend(name::AbstractString) = any(s -> s.id == normalize_language(name), _BACKEND_REGISTRY)

function backend_capabilities(name::AbstractString)
    id = normalize_language(name)
    for spec in _BACKEND_REGISTRY
        spec.id == id && return copy(spec.capabilities)
    end
    return String[]
end

supports_capability(caps, capability::AbstractString) = String(capability) in caps

function _exact_integer_capability(feature::AbstractString)
    f = String(feature)
    startswith(f, "integer.") || startswith(f, "native.integer.") || startswith(f, "fixed_width_integer.")
end

function backend_capability(feature::AbstractString, backend::AbstractString)
    f = String(feature)
    b = normalize_language(backend)
    integer_targets = Set(["go", "python", "c", "rust", "cpp", "java", "csharp"])
    scalar_targets = Set(["go", "python", "rust", "c", "cpp", "java", "csharp"])

    if _exact_integer_capability(f) && b in integer_targets
        return CapabilityResult(f, b, CapabilityLowering,
            "fixed-width integer operations with exact values and explicit wrap semantics")
    end
    if !has_backend(b)
        return CapabilityResult(f, b, CapabilityUnsupported, "unknown backend")
    end
    if f == "native.go.functions" && b in scalar_targets
        return CapabilityResult(f, b, CapabilityLowering, "native scalar helper functions")
    elseif f == "core"
        return CapabilityResult(f, b, CapabilityLowering, "shared semantic core lowering")
    elseif f == "native.go.scalar"
        return CapabilityResult(f, b, CapabilityLowering, "shared native scalar UAST lowering")
    elseif f in ("native.call.receiver.v1", "native.call.ordered_product.v1", "native.init.order.v1")
        return CapabilityResult(f, b, CapabilityLowering,
            "shared canonical call and initialization contract lowering")
    elseif supports_capability(backend_capabilities(b), f)
        return CapabilityResult(f, b, CapabilityNative, "")
    end
    return CapabilityResult(f, b, CapabilityUnsupported, "backend has no declared capability")
end

const _CPP_RESERVED = Set(split("alignas alignof and and_eq asm auto bitand bitor bool break case catch char class compl concept const consteval constexpr constinit const_cast continue co_await co_return co_yield decltype default delete do double dynamic_cast else enum explicit export extern false float for friend goto if inline int long mutable namespace new noexcept not not_eq nullptr operator or or_eq private protected public register reinterpret_cast requires return short signed sizeof static static_assert static_cast struct switch template this thread_local throw true try typedef typeid typename union unsigned using virtual void volatile wchar_t while xor xor_eq"))

"""Port of backend.cppIdent from Go."""
function cpp_ident(s::AbstractString)
    out = strip(String(s))
    out = replace(out, "." => "_", "\$" => "_", "@" => "_")
    isempty(out) && return "r_value"
    out in _CPP_RESERVED && return "r_" * out
    return out
end
