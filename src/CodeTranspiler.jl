module CodeTranspiler

using Sockets

export Language, languages, routes, transpile, transpile_with_semantics, transpile_file, backend_path,
       backend_status, gui, serve_gui, stop_gui,
       FrontendSpec, BackendSpec, CapabilityStatus, CapabilityResult,
       CapabilityNative, CapabilityLowering, CapabilityEmulated, CapabilityUnsupported,
       frontends, backends, normalize_language, has_frontend, has_backend,
       backend_capabilities, supports_capability, backend_capability, cpp_ident,
       NativeTranspileError, native_go_to_julia, native_to_julia, native_from_julia, native_route_supported,
       SemanticProgram, SemanticFunctionInfo, SemanticBindingInfo, SemanticOperationInfo, semantic_program, semantic_output, semantic_exchange, semantic_exchange_pivot,
       PreservationMode, PreservationRule, PreservationRegistry, RequirementKind,
       Requirement, RequirementRegistry, solve, resolve, default_preservation_registry,
       default_requirement_registry, target_reserved_word, target_name, target_na,
       target_inf, emit_dispatch, RExact, exact_value, r_exact

include("native_registry.jl")
include("native_go.jl")
include("native_multi.jl")
include("native_target.jl")
include("semantic.jl")
include("exact_runtime.jl")
include("core.jl")
include("gui.jl")

end # module
