@enum PreservationMode begin
    PreservationDirect
    PreservationRewrite
    PreservationHelper
    PreservationEmulate
    PreservationRuntime
    PreservationError
end

struct PreservationRule
    capability::String
    target::String
    mode::PreservationMode
    preconditions::Vector{String}
    requirements::Vector{String}
    handler::String
    test::String
end

struct PreservationRegistry
    rules::Vector{PreservationRule}
end

@enum RequirementKind begin
    RequirementImport
    RequirementHelper
    RequirementEmulation
    RequirementRuntime
end

struct Requirement
    id::String
    kind::RequirementKind
    target::String
    dependencies::Vector{String}
    preconditions::Vector{String}
    tests::Vector{String}
    emit::String
end

struct RequirementRegistry
    rules::Dict{String,Requirement}
end

function solve(registry::PreservationRegistry, target::AbstractString, capability::AbstractString)
    t, c = normalize_language(target), String(capability)
    for mode in (PreservationDirect, PreservationRewrite, PreservationHelper, PreservationEmulate, PreservationRuntime)
        for rule in registry.rules
            rule.target == t && rule.capability == c && rule.mode == mode && return rule
        end
    end
    return nothing
end

function resolve(registry::RequirementRegistry, ids::AbstractVector{<:AbstractString})
    state = Dict{String,Int}()
    out = Requirement[]
    function visit(id::String)
        get(state, id, 0) == 1 && throw(ArgumentError("CYCLIC_REQUIREMENT_DEPENDENCY: $id"))
        get(state, id, 0) == 2 && return
        haskey(registry.rules, id) || throw(ArgumentError("unknown requirement $(repr(id))"))
        rule = registry.rules[id]
        state[id] = 1
        for dep in rule.dependencies
            visit(dep)
        end
        state[id] = 2
        push!(out, rule)
    end
    for id in sort!(unique(String.(ids)))
        visit(id)
    end
    return out
end

default_preservation_registry() = PreservationRegistry([
    PreservationRule("uast.core", target.id, PreservationDirect, String[], String[],
        "UniversalTargetEngine", "TestUniversalBackendUASTOnlyTargetMatrix")
    for target in _BACKEND_REGISTRY
])

default_requirement_registry() = RequirementRegistry(Dict{String,Requirement}())

const _TARGET_RESERVED_WORDS = Dict(
    "zig" => Set(split("var const fn pub struct enum type comptime if else for while return switch catch orelse error")),
    "cpp" => Set(split("auto class struct template typename namespace return if else for while switch case default const static void int double float char bool")),
    "c" => Set(split("auto struct typedef return if else for while switch case default const static void int double float char")),
    "csharp" => Set(split("var class struct namespace return if else for while switch case default new string int double bool")),
    "go" => Set(split("var const func type package import return if else for switch case default range go defer map struct interface")),
)

target_reserved_word(target::AbstractString, name::AbstractString) = String(name) in get(_TARGET_RESERVED_WORDS, normalize_language(target), Set{SubString{String}}())

"""Resolve a generated identifier using the same keyword-avoidance policy as the Go universal target projector."""
function target_name(target::AbstractString, preferred::AbstractString; generated_prefix="__uast_")
    name = String(preferred)
    startswith(name, '\0') && return name
    safe = replace(name, r"[^A-Za-z0-9_]" => "_")
    isempty(safe) && (safe = "value")
    occursin(r"^[0-9]", safe) && (safe = "_" * safe)
    target_reserved_word(target, safe) && return generated_prefix * safe
    return safe
end

function target_na(target::AbstractString)
    t = normalize_language(target)
    return get(Dict(
        "python"=>"float('nan')", "julia"=>"NaN", "nim"=>"rNum(NaN)", "go"=>"math.NaN()",
        "rust"=>"RValue::Num(f64::NAN)", "cpp"=>"RValue(std::numeric_limits<double>::quiet_NaN())",
        "c"=>"r_num(NAN)", "zig"=>"RValue{ .num = std.math.nan(f64) }",
        "csharp"=>"new R2.Value(double.NaN)", "java"=>"new RValue(Double.NaN)",
        "kotlin"=>"RValue.Num(Double.NaN)", "swift"=>"RValue.num(Double.nan)"), t, "NaN")
end

function target_inf(target::AbstractString)
    t = normalize_language(target)
    return get(Dict(
        "python"=>"float('inf')", "julia"=>"Inf", "nim"=>"rNum(Inf)", "go"=>"math.Inf(1)",
        "rust"=>"RValue::Num(f64::INFINITY)", "cpp"=>"RValue(std::numeric_limits<double>::infinity())",
        "c"=>"r_num(INFINITY)", "zig"=>"RValue{ .num = std.math.inf(f64) }",
        "csharp"=>"new R2.Value(double.PositiveInfinity)", "java"=>"new RValue(Double.POSITIVE_INFINITY)",
        "kotlin"=>"RValue.Num(Double.POSITIVE_INFINITY)", "swift"=>"RValue.num(Double.infinity)"), t, "Inf")
end

function _dispatch_kernel(name::AbstractString)
    n = String(name)
    if startswith(n, "__binary_")
        op = n[10:end]
        op in ("+", "-", "*", "/", "^", "**", "%%", "%/%") && return "arithmetic"
        op in ("==", "!=", "<", "<=", ">", ">=") && return "relational"
        op in ("&", "|", "&&", "||") && return "logical"
        return "language"
    elseif startswith(n, "__unary_")
        return "arithmetic"
    end
    return "runtime"
end

"""Port of the generic target dispatch renderer from targets.go for the built-in targets."""
function emit_dispatch(target::AbstractString, name::AbstractString, args::AbstractVector{<:AbstractString}; kernel=nothing)
    t, n = normalize_language(target), String(name)
    k = kernel === nothing ? _dispatch_kernel(n) : String(kernel)
    a = join(String.(args), ", ")
    q(x) = repr(String(x))
    if t == "go"
        return "rCall($(q(k)), $(q(n)), []any{$a})"
    elseif t == "rust"
        return "r_call($(q(k)), $(q(n)), vec![$a])"
    elseif t == "cpp"
        return "r_call($(q(k)), $(q(n)), {$a})"
    elseif t == "c"
        isempty(args) && return "r_call($(q(k)), $(q(n)), NULL, 0)"
        return "r_call($(q(k)), $(q(n)), (RValue[]){$a}, $(length(args)))"
    elseif t == "python"
        return "r_call($(q(k)), $(q(n)), [$a])"
    elseif t == "zig"
        return "rCall($(q(k)), $(q(n)), &[_]RValue{$a})"
    elseif t == "julia"
        return "r_call($(q(k)), $(q(n)), Any[$a])"
    elseif t == "nim"
        return "rCall($(q(k)), $(q(n)), @[$a])"
    elseif t == "csharp"
        return "R2.Call($(q(k)), $(q(n)), new object[]{$a})"
    elseif t == "java"
        return "R2.rCall($(q(k)), $(q(n)), new Object[]{$a})"
    elseif t == "kotlin"
        return "rCall($(q(k)), $(q(n)), arrayOf($a))"
    elseif t == "swift"
        return "rCall($(q(k)), $(q(n)), [$a])"
    end
    throw(ArgumentError("unknown target: $target"))
end
