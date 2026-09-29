"""One symbol/binding discovered in the semantic program."""
struct SemanticBindingInfo
    name::String
    kind::String
    type::String
    scope::String
    mutable::Bool
end

"""One normalized operation discovered while analysing the Julia semantic pivot."""
struct SemanticOperationInfo
    op::String
    result_type::String
    scope::String
    source::String
    semantics::Dict{String,String}
end

"""Summary of one function in a `SemanticProgram`."""
struct SemanticFunctionInfo
    name::String
    parameters::Vector{Pair{String,String}}
    return_type::String
    bindings::Vector{SemanticBindingInfo}
    operations::Vector{SemanticOperationInfo}
    effects::Vector{String}
    control_flow::Vector{String}
end

"""
Language-neutral semantic view produced by the native frontends.

This is deliberately conservative: facts are recorded only when the current
frontend can establish them. Unknown facts remain `"unknown"` instead of being
guessed from target-language syntax.
"""
struct SemanticProgram
    source_language::String
    evaluation::String
    value_model::String
    source_index_base::Int
    index_base::Int
    functions::Vector{SemanticFunctionInfo}
    globals::Vector{SemanticBindingInfo}
    features::Vector{String}
    effects::Vector{String}
    warnings::Vector{String}
    pivot::String
end

const _ZERO_BASED_LANGS = Set(["go","rust","c","cpp","python","zig","nim","csharp","java","kotlin","swift"])

function _semantic_eval_model(lang::String)
    lang == "r" && return "lazy_demand"
    return "eager_left_to_right"
end

function _semantic_value_model(lang::String)
    lang == "python" && return "dynamic_arbitrary_precision_integer"
    lang == "r" && return "dynamic_vectorized"
    lang in ("c","cpp") && return "static_native_machine_values"
    lang == "rust" && return "static_owned_values"
    lang == "go" && return "static_go_values"
    return "mixed_static_dynamic"
end

function _semantic_op(expr::AbstractString, vartypes::Dict{String,String})
    e = strip(String(expr))
    if occursin(r"^length\s*\(", e)
        return SemanticOperationInfo("collection.length", "Int", "", e, Dict("result"=>"canonical_machine_integer"))
    elseif occursin(r"^push!\s*\(", e)
        return SemanticOperationInfo("collection.append", "Nothing", "", e, Dict("mutation"=>"in_place_at_pivot"))
    elseif occursin(r"^(?:println|print)\s*\(", e)
        return SemanticOperationInfo("io.stdout", "Nothing", "", e, Dict("effect"=>"stdout"))
    end
    # Ordered longest first so comparisons are not mistaken for assignments.
    candidates = [
        ("&&", "logical.and"), ("||", "logical.or"),
        ("==", "comparison.equal"), ("!=", "comparison.not_equal"),
        ("<=", "comparison.less_equal"), (">=", "comparison.greater_equal"),
        ("<", "comparison.less"), (">", "comparison.greater"),
        ("÷", "integer.divide"), ("%", "numeric.remainder"),
        ("+", "numeric.add"), ("-", "numeric.subtract"),
        ("*", "numeric.multiply"), ("/", "numeric.divide"),
    ]
    for (token, opname) in candidates
        occursin(token, e) || continue
        typ = _infer_julia_expr_type(e, vartypes)
        op = opname
        if token == "+" && typ == "String"
            op = "string.concat"
        elseif token == "+" && startswith(typ, "Int")
            op = "integer.add"
        elseif token == "-" && startswith(typ, "Int")
            op = "integer.subtract"
        elseif token == "*" && startswith(typ, "Int")
            op = "integer.multiply"
        end
        sem = Dict{String,String}("evaluation_order"=>"left_to_right")
        startswith(op, "integer.") && (sem["overflow"] = "source_language")
        return SemanticOperationInfo(op, typ, "", e, sem)
    end
    if occursin(r"\b[A-Za-z_]\w*\s*\[", e)
        return SemanticOperationInfo("collection.index", "Any", "", e,
            Dict("pivot_index_base"=>"1", "bounds"=>"checked_by_pivot"))
    end
    m = match(r"^([A-Za-z_]\w*)\s*\(", e)
    m !== nothing && return SemanticOperationInfo("function.call", "Any", "", e,
        Dict("dispatch"=>"source_language", "evaluation_order"=>"left_to_right"))
    return nothing
end

function _semantic_target_warnings(target::Union{Nothing,String}, p::SemanticProgram)
    target === nothing && return String[]
    t = normalize_language(target)
    w = String[]
    if t in _ZERO_BASED_LANGS && any("indexing" == f for f in p.features)
        push!(w, "target '$t' uses zero-based collection indexing; index projection must preserve collection kind and source index semantics")
    end
    if p.source_language == "python" && t in ("c","cpp","go","rust","java","csharp","kotlin","swift","zig","nim")
        push!(w, "Python integers may exceed the fixed-width integer domain of target '$t'")
    end
    if p.source_language == "r" && t != "r"
        push!(w, "R lazy arguments, NA/NULL and vector recycling are only partially modeled in the current native frontend")
    end
    if p.source_language == "rust" && t != "rust"
        push!(w, "Rust ownership/borrowing/lifetime guarantees are not yet represented completely")
    end
    if p.source_language in ("c","cpp") && t ∉ ("c","cpp")
        push!(w, "C/C++ pointer aliasing, undefined behavior and ABI-sensitive semantics are not yet represented completely")
    end
    unique(w)
end

"""
    semantic_program(source, code) -> SemanticProgram

Lower `code` with the native frontend and return a deterministic semantic
summary of the supported meaning. This API never invokes an external binary.
"""
function semantic_program(source::AbstractString, code::AbstractString)
    lang = normalize_language(source)
    pivot = native_to_julia(lang, String(code))
    lines = split(replace(pivot, "\r\n"=>"\n"), '\n')

    functions = SemanticFunctionInfo[]
    globals = SemanticBindingInfo[]
    features = Set{String}()
    global_effects = Set{String}()
    warnings = String[]

    current_name = nothing
    current_params = Pair{String,String}[]
    current_ret = "Any"
    current_bindings = SemanticBindingInfo[]
    current_ops = SemanticOperationInfo[]
    current_effects = Set{String}()
    current_control = String[]
    vartypes = Dict{String,String}()
    depth = 0

    function finish_function!()
        current_name === nothing && return
        push!(functions, SemanticFunctionInfo(String(current_name), copy(current_params), current_ret,
            copy(current_bindings), copy(current_ops), sort!(collect(current_effects)), copy(current_control)))
    end

    for raw in lines
        line = strip(raw)
        isempty(line) && continue
        startswith(line, "#") && continue
        sig = _parse_julia_signature(line)
        if sig !== nothing
            current_name !== nothing && finish_function!()
            current_name = sig.name
            empty!(current_params); empty!(current_bindings); empty!(current_ops); empty!(current_effects); empty!(current_control); empty!(vartypes)
            for (n,t) in sig.params
                push!(current_params, n=>t); vartypes[n]=t
                push!(current_bindings, SemanticBindingInfo(n,"parameter",t,String(current_name),false))
            end
            current_ret = sig.ret
            depth = 1
            push!(features,"functions")
            continue
        end
        if line == "end"
            depth -= 1
            if current_name !== nothing && depth <= 0
                finish_function!(); current_name=nothing; depth=0
            end
            continue
        end
        scope = current_name === nothing ? "global" : String(current_name)
        target_bindings = current_name === nothing ? globals : current_bindings
        target_effects = current_name === nothing ? global_effects : current_effects
        target_ops = current_name === nothing ? nothing : current_ops
        target_control = current_name === nothing ? nothing : current_control

        if startswith(line,"if ")
            push!(features,"branching"); target_control !== nothing && push!(target_control,"if")
            depth += 1
            op = _semantic_op(line[4:end],vartypes); op !== nothing && target_ops !== nothing && push!(target_ops, SemanticOperationInfo(op.op,op.result_type,scope,op.source,op.semantics))
            continue
        elseif startswith(line,"elseif ")
            target_control !== nothing && push!(target_control,"elseif")
            continue
        elseif line == "else"
            target_control !== nothing && push!(target_control,"else")
            continue
        elseif startswith(line,"while ")
            push!(features,"loops"); target_control !== nothing && push!(target_control,"while"); depth += 1
            continue
        elseif startswith(line,"for ")
            push!(features,"loops"); target_control !== nothing && push!(target_control,"for"); depth += 1
            occursin("enumerate(",line) && push!(features,"indexing")
            continue
        end

        if occursin("println(",line) || occursin("print(",line)
            push!(target_effects,"io.stdout")
        end
        occursin("push!(",line) && (push!(target_effects,"mutation.collection"); push!(features,"mutable_collections"))
        occursin("Dict(",line) && push!(features,"maps")
        (occursin("[",line) && occursin("]",line)) && push!(features,"indexing")
        occursin("mutable struct ",line) && push!(features,"mutable_structs")

        am = match(r"^([A-Za-z_]\w*)\s*=\s*(.+)$", line)
        if am !== nothing
            n=String(am.captures[1]); ex=String(am.captures[2])
            typ=_infer_julia_expr_type(ex,vartypes); typ == "Any" && haskey(vartypes,n) && (typ=vartypes[n])
            mutable = haskey(vartypes,n)
            vartypes[n]=typ
            if !any(b->b.name==n && b.scope==scope,target_bindings)
                push!(target_bindings,SemanticBindingInfo(n,"local",typ,scope,mutable))
            end
            op=_semantic_op(ex,vartypes); op !== nothing && target_ops !== nothing && push!(target_ops,SemanticOperationInfo(op.op,op.result_type,scope,op.source,op.semantics))
            continue
        end
        rm = match(r"^return(?:\s+(.+))?$",line)
        if rm !== nothing && rm.captures[1] !== nothing
            ex=String(rm.captures[1]); op=_semantic_op(ex,vartypes); op !== nothing && target_ops !== nothing && push!(target_ops,SemanticOperationInfo(op.op,op.result_type,scope,op.source,op.semantics))
            inferred=_infer_julia_expr_type(ex,vartypes)
            current_ret == "Any" && inferred != "Any" && (current_ret=inferred)
            continue
        end
        op=_semantic_op(line,vartypes); op !== nothing && target_ops !== nothing && push!(target_ops,SemanticOperationInfo(op.op,op.result_type,scope,op.source,op.semantics))
    end
    current_name !== nothing && finish_function!()

    lang in _ZERO_BASED_LANGS && ("indexing" in features) && push!(warnings,"source uses zero-based indexing; native frontend normalized supported collection indexes to the one-based semantic pivot")
    lang == "r" && push!(warnings,"R lazy evaluation and vector semantics are only partially represented by the current frontend")
    lang == "rust" && push!(warnings,"ownership, borrowing and lifetimes are not yet fully represented")
    lang in ("c","cpp") && push!(warnings,"pointer aliasing, undefined behavior, preprocessor/ABI semantics remain explicit unsupported areas")

    source_index_base = lang in _ZERO_BASED_LANGS ? 0 : 1
    return SemanticProgram(lang,_semantic_eval_model(lang),_semantic_value_model(lang),source_index_base,1,functions,globals,
        sort!(collect(features)),sort!(collect(global_effects)),unique(warnings),pivot)
end

function _json_escape(s::AbstractString)
    replace(String(s), '\\'=>"\\\\", '"'=>"\\\"", '\n'=>"\\n", '\r'=>"\\r", '\t'=>"\\t")
end
_json_string(s) = "\"" * _json_escape(string(s)) * "\""
function _json_value(x)
    x === nothing && return "null"
    x isa Bool && return x ? "true" : "false"
    x isa Number && return string(x)
    x isa AbstractString && return _json_string(x)
    x isa Pair && return "{"*_json_string(first(x))*":"*_json_value(last(x))*"}"
    x isa AbstractVector && return "["*join((_json_value(v) for v in x),",")*"]"
    x isa Dict && return "{"*join((_json_string(k)*":"*_json_value(v) for (k,v) in sort!(collect(x); by=x->string(first(x)))),",")*"}"
    if x isa SemanticBindingInfo
        return _json_value(Dict("name"=>x.name,"kind"=>x.kind,"type"=>x.type,"scope"=>x.scope,"mutable"=>x.mutable))
    elseif x isa SemanticOperationInfo
        return _json_value(Dict("operation"=>x.op,"result_type"=>x.result_type,"scope"=>x.scope,"source"=>x.source,"semantics"=>x.semantics))
    elseif x isa SemanticFunctionInfo
        return _json_value(Dict("name"=>x.name,"parameters"=>[Dict("name"=>first(p),"type"=>last(p)) for p in x.parameters],"return_type"=>x.return_type,"bindings"=>x.bindings,"operations"=>x.operations,"effects"=>x.effects,"control_flow"=>x.control_flow))
    elseif x isa SemanticProgram
        return _json_value(Dict("source_language"=>x.source_language,"evaluation"=>x.evaluation,"value_model"=>x.value_model,"source_index_base"=>x.source_index_base,"index_base"=>x.index_base,"functions"=>x.functions,"globals"=>x.globals,"features"=>x.features,"effects"=>x.effects,"warnings"=>x.warnings))
    end
    return _json_string(string(x))
end

"""
    semantic_output(source, code; target=nothing, format=:text) -> String

Render semantic analysis. `format=:json` returns deterministic JSON suitable for
machine consumption; the default human-readable form is used by the GUI.
"""
function semantic_output(source::AbstractString, code::AbstractString; target=nothing, format::Symbol=:text)
    p=semantic_program(source,code)
    target_warnings=_semantic_target_warnings(target===nothing ? nothing : String(target),p)
    if format == :json
        base=Dict("source_language"=>p.source_language,"evaluation"=>p.evaluation,"value_model"=>p.value_model,"source_index_base"=>p.source_index_base,"index_base"=>p.index_base,"functions"=>p.functions,"globals"=>p.globals,"features"=>p.features,"effects"=>p.effects,"warnings"=>vcat(p.warnings,target_warnings))
        target !== nothing && (base["target_language"]=normalize_language(String(target)))
        return _json_value(base)*"\n"
    elseif format != :text
        throw(ArgumentError("semantic output format must be :text or :json"))
    end
    io=IOBuffer()
    println(io,"SemanticProgram")
    println(io,"  source: ",p.source_language)
    target !== nothing && println(io,"  target: ",normalize_language(String(target)))
    println(io,"  evaluation: ",p.evaluation)
    println(io,"  value_model: ",p.value_model)
    println(io,"  source_index_base: ",p.source_index_base)
    println(io,"  canonical_index_base: ",p.index_base)
    println(io,"  features: ",isempty(p.features) ? "none" : join(p.features,", "))
    for f in p.functions
        params=join(("$(first(x))::$(last(x))" for x in f.parameters),", ")
        println(io,"\nfunction ",f.name,"(",params,") -> ",f.return_type)
        !isempty(f.control_flow) && println(io,"  control: ",join(f.control_flow," -> "))
        !isempty(f.effects) && println(io,"  effects: ",join(f.effects,", "))
        if !isempty(f.bindings)
            println(io,"  bindings:")
            for b in f.bindings
                println(io,"    - ",b.name," : ",b.type," [",b.kind,b.mutable ? ", mutable" : "","]")
            end
        end
        if !isempty(f.operations)
            println(io,"  operations:")
            for op in f.operations
                extras=isempty(op.semantics) ? "" : " {"*join(("$k=$v" for (k,v) in sort!(collect(op.semantics))),", ")*"}"
                println(io,"    - ",op.op," -> ",op.result_type,": ",op.source,extras)
            end
        end
    end
    allwarn=unique(vcat(p.warnings,target_warnings))
    if !isempty(allwarn)
        println(io,"\nwarnings:")
        for w in allwarn; println(io,"  - ",w); end
    end
    return String(take!(io))
end


const _SE_MAGIC = "SE/1"
const _SE_SEMANTICS_MARKER = "--- semantics-json ---"
const _SE_PIVOT_MARKER = "--- julia-pivot ---"
const _SE_END_MARKER = "--- end ---"

"""
    semantic_exchange(source, code; pivot=nothing) -> String

Emit the binary-free Semantic Exchange (`SE/1`) format. The document contains
semantic JSON plus the canonical Julia pivot, making SE usable as both output
and input.
"""
function semantic_exchange(source::AbstractString, code::AbstractString; pivot=nothing)
    src = normalize_language(source)
    src == "se" && return String(code)
    pv = pivot === nothing ? (src == "julia" ? String(code) : native_to_julia(src, String(code))) : String(pivot)
    sem = semantic_output(src, String(code); format=:json)
    io = IOBuffer()
    println(io, _SE_MAGIC)
    println(io, "source: ", src)
    println(io, "canonical: julia")
    println(io, _SE_SEMANTICS_MARKER)
    print(io, sem)
    endswith(sem, "\n") || println(io)
    println(io, _SE_PIVOT_MARKER)
    print(io, pv)
    endswith(pv, "\n") || println(io)
    println(io, _SE_END_MARKER)
    return String(take!(io))
end

"""
    semantic_exchange_pivot(text) -> String

Read an `SE/1` document and return its canonical Julia pivot.
"""
function semantic_exchange_pivot(text::AbstractString)
    s = replace(String(text), "\r\n" => "\n")
    startswith(s, _SE_MAGIC * "\n") || throw(NativeTranspileError("invalid SE input: expected SE/1 header"))
    pm = findfirst(_SE_PIVOT_MARKER * "\n", s)
    pm === nothing && throw(NativeTranspileError("invalid SE input: missing Julia pivot section"))
    startidx = last(pm) + 1
    em = findnext("\n" * _SE_END_MARKER, s, startidx)
    em === nothing && throw(NativeTranspileError("invalid SE input: missing end marker"))
    pivot = s[startidx:first(em)-1]
    isempty(strip(pivot)) && throw(NativeTranspileError("invalid SE input: empty Julia pivot"))
    return endswith(pivot, "\n") ? pivot : pivot * "\n"
end
