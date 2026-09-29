# Native Go -> Julia lowering used by the registry-only package.
# The translator is intentionally explicit: unsupported constructs raise an
# error instead of falling through to an external binary.

struct NativeTranspileError <: Exception
    message::String
end
Base.showerror(io::IO, e::NativeTranspileError) = print(io, e.message)

mutable struct _GoState
    vars::Dict{String,String}
    structs::Dict{String,Vector{Tuple{String,String}}}
    current_struct::Union{Nothing,String}
    struct_fields::Vector{Tuple{String,String}}
    needs_printf::Bool
end
_GoState() = _GoState(Dict{String,String}(), Dict{String,Vector{Tuple{String,String}}}(), nothing, Tuple{String,String}[], false)

const _GO_TYPE_MAP = Dict(
    "int"=>"Int", "int8"=>"Int8", "int16"=>"Int16", "int32"=>"Int32", "int64"=>"Int64",
    "uint"=>"UInt", "uint8"=>"UInt8", "uint16"=>"UInt16", "uint32"=>"UInt32", "uint64"=>"UInt64",
    "uintptr"=>"UInt", "float32"=>"Float32", "float64"=>"Float64", "bool"=>"Bool",
    "string"=>"String", "byte"=>"UInt8", "rune"=>"Char", "any"=>"Any", "interface{}"=>"Any",
)

function _go_type(t::AbstractString)
    s = strip(String(t))
    startswith(s, "[]") && return "Vector{$(_go_type(s[3:end]))}"
    m = match(r"^\[(\d+)\](.+)$", s)
    m !== nothing && return "Vector{$(_go_type(m.captures[2]))}"
    m = match(r"^map\[(.+)\](.+)$", s)
    m !== nothing && return "Dict{$(_go_type(m.captures[1])),$(_go_type(m.captures[2]))}"
    startswith(s, "*") && return "Union{Nothing,$(_go_type(s[2:end]))}"
    return get(_GO_TYPE_MAP, s, s)
end

function _go_zero(t::AbstractString)
    s = strip(String(t))
    jt = _go_type(s)
    startswith(s, "[]") && return "$(_go_type(s[3:end]))[]"
    occursin(r"^\[\d+\]", s) && begin
        m = match(r"^\[(\d+)\](.+)$", s); return "fill($(_go_zero(m.captures[2])), $(m.captures[1]))"
    end
    startswith(s, "map[") && return "$jt()"
    startswith(s, "*") && return "nothing"
    s == "string" && return "\"\""
    s == "bool" && return "false"
    s == "rune" && return "Char(0)"
    s in keys(_GO_TYPE_MAP) && s ∉ ("string", "bool", "any", "interface{}") && return "zero($jt)"
    return "$jt()"
end

function _split_top_level(s::AbstractString, delim::Char=',')
    out = String[]; buf = IOBuffer(); depth = 0; qchar = '\0'; esc = false
    for c in String(s)
        if qchar != '\0'
            print(buf, c)
            if esc
                esc = false
            elseif c == '\\'
                esc = true
            elseif c == qchar
                qchar = '\0'
            end
        elseif c == '"' || c == '\''
            qchar = c; print(buf, c)
        elseif c in ('(', '[', '{')
            depth += 1; print(buf, c)
        elseif c in (')', ']', '}')
            depth -= 1; print(buf, c)
        elseif c == delim && depth == 0
            push!(out, strip(String(take!(buf))))
        else
            print(buf, c)
        end
    end
    push!(out, strip(String(take!(buf))))
    return out
end

function _remember_params!(state::_GoState, params::AbstractString)
    isempty(strip(params)) && return
    for raw in _split_top_level(params)
        item = strip(raw)
        m = match(r"^([A-Za-z_]\w*)\s+(.+)$", item)
        m === nothing && continue
        state.vars[m.captures[1]] = strip(m.captures[2])
    end
end

function _parse_go_params(params::AbstractString, state::_GoState)
    isempty(strip(params)) && return ""
    items = _split_top_level(params)
    out = String[]
    pending = String[]
    for raw in items
        item = strip(raw)
        m = match(r"^([A-Za-z_]\w*)\s+(.+)$", item)
        if m === nothing
            match(r"^[A-Za-z_]\w*$", item) === nothing && throw(NativeTranspileError("unsupported Go parameter syntax: $item"))
            push!(pending, item)
            continue
        end
        name, typ = m.captures
        typ = strip(typ)
        names = [pending; String(name)]
        empty!(pending)
        for n in names
            state.vars[n] = typ
            push!(out, "$(n)::$(_go_type(typ))")
        end
    end
    isempty(pending) || throw(NativeTranspileError("Go parameter names without a type: $(join(pending, ", "))"))
    join(out, ", ")
end

function _go_return_annotation(ret::AbstractString)
    r = strip(ret)
    isempty(r) && return ""
    startswith(r, "(") && return "" # Julia naturally supports tuple returns; names are handled by body when present.
    return "::$(_go_type(r))"
end

function _rewrite_composite_literals(s::String)
    # []T{a,b} -> Julia typed vector literals for built-in scalar types.
    for (gt, jt) in _GO_TYPE_MAP
        gt in ("any", "interface{}") && continue
        pat = Regex("\\[\\]" * gt * "\\s*\\{([^{}]*)\\}")
        s = replace(s, pat => SubstitutionString("$(jt)[\\1]"))
    end
    # Named unkeyed struct literal: Point{1,2} -> Point(1,2).
    s = replace(s, r"\b([A-Z][A-Za-z0-9_]*)\s*\{([^{}:]*)\}" => s"\1(\2)")
    return s
end

function _rewrite_indexing(s::String, state::_GoState)
    # Rewrite simple Go 0-based indexing only for variables whose type is known.
    for (name, typ) in state.vars
        escaped = replace(name, r"([.^$|()\[\]{}*+?\\])" => s"\\\1")
        if startswith(typ, "[]") || occursin(r"^\[\d+\]", typ)
            # Slices first, because the scalar-index regex intentionally excludes ':'.
            s = replace(s, Regex("\\b" * escaped * "\\[([^]:]+):([^]]+)\\]") => SubstitutionString("$(name)[\\1+1:\\2]"))
            s = replace(s, Regex("\\b" * escaped * "\\[:([^]]+)\\]") => SubstitutionString("$(name)[1:\\1]"))
            s = replace(s, Regex("\\b" * escaped * "\\[([^]:]+):\\]") => SubstitutionString("$(name)[\\1+1:end]"))
            s = replace(s, Regex("\\b" * escaped * "\\[([^]:]+)\\]") => SubstitutionString("$(name)[(\\1) + 1]"))
        elseif typ == "string"
            s = replace(s, Regex("\\b" * escaped * "\\[([^]:]+)\\]") => SubstitutionString("codeunit($(name), (\\1) + 1)"))
        end
    end
    return s
end


function _go_is_integer_type(t::AbstractString)
    s = strip(String(t))
    return s in ("int","int8","int16","int32","int64","uint","uint8","uint16","uint32","uint64","uintptr","byte","rune")
end

function _go_simple_typeof(token::AbstractString, state::_GoState)
    t = strip(String(token))
    haskey(state.vars, t) && return state.vars[t]
    occursin(r"^[+-]?\d+$", t) && return "int"
    occursin(r"^[+-]?(?:\d+\.\d*|\d*\.\d+)(?:[eE][+-]?\d+)?$", t) && return "float64"
    startswith(t, "\"") && endswith(t, "\"") && return "string"
    return ""
end

function _rewrite_typed_operators(s::String, state::_GoState)
    # Integer division in Go truncates toward zero; Julia's / does not.
    m = match(r"^\s*([^/]+?)\s+/\s+([^/]+?)\s*$", s)
    if m !== nothing
        lt = _go_simple_typeof(m.captures[1], state); rt = _go_simple_typeof(m.captures[2], state)
        if _go_is_integer_type(lt) && _go_is_integer_type(rt)
            return "$(strip(m.captures[1])) ÷ $(strip(m.captures[2]))"
        end
    end
    # Go concatenates strings with +; Julia uses *.
    m = match(r"^\s*(.+?)\s+\+\s+(.+?)\s*$", s)
    if m !== nothing
        lt = _go_simple_typeof(m.captures[1], state); rt = _go_simple_typeof(m.captures[2], state)
        if lt == "string" && rt == "string"
            return "$(strip(m.captures[1])) * $(strip(m.captures[2]))"
        end
    end
    return s
end

function _go_expr(expr::AbstractString, state::_GoState)
    s = strip(String(expr))
    s = replace(s, r"\bnil\b" => "nothing")
    s = replace(s, r"\blen\s*\(" => "length(", r"\bcap\s*\(" => "length(")
    s = replace(s, r"\bfmt\.Println\s*\(" => "println(", r"\bfmt\.Print\s*\(" => "print(")
    if occursin(r"\bfmt\.(Sprintf|Printf)\s*\(", s)
        state.needs_printf = true
        s = replace(s, r"\bfmt\.Sprintf\s*\(" => "Printf.@sprintf(")
        s = replace(s, r"\bfmt\.Printf\s*\(" => "Printf.@printf(")
    end
    s = replace(s, r"\bstrings\.ToUpper\s*\(" => "uppercase(", r"\bstrings\.ToLower\s*\(" => "lowercase(")
    s = replace(s, r"\bstrings\.Contains\s*\(" => "occursin(")
    s = replace(s, r"\bmath\.Abs\s*\(" => "abs(", r"\bmath\.Sqrt\s*\(" => "sqrt(")
    s = replace(s, r"\berrors\.New\s*\(" => "ErrorException(")
    if occursin(r"\bfmt\.Errorf\s*\(", s)
        state.needs_printf = true
        s = replace(s, r"\bfmt\.Errorf\s*\(" => "ErrorException(Printf.@sprintf(")
        endswith(s, ")") && (s *= ")")
    end
    # Go method calls become ordinary Julia functions with receiver first.
    s = replace(s, r"\b([a-zA-Z_]\w*)\.([A-Z][A-Za-z0-9_]*)\s*\(\s*\)" => s"\2(\1)")
    s = replace(s, r"\b([a-zA-Z_]\w*)\.([A-Z][A-Za-z0-9_]*)\s*\(" => s"\2(\1, ")
    # Built-in scalar conversions.
    for (gt, jt) in _GO_TYPE_MAP
        gt in ("any", "interface{}") && continue
        s = replace(s, Regex("\\b" * gt * "\\s*\\(") => "$(jt)(")
    end
    # Whole-expression map composite literal.
    mm = match(r"^map\[([^]]+)\]([A-Za-z_]\w*)\s*\{(.*)\}$", s)
    if mm !== nothing
        kt, vt, body = mm.captures
        entries = String[]
        for item in _split_top_level(body)
            isempty(strip(item)) && continue
            pair = split(item, ':'; limit=2)
            length(pair) == 2 || throw(NativeTranspileError("unsupported map literal entry: $item"))
            push!(entries, "$(strip(pair[1])) => $(_go_expr(strip(pair[2]), state))")
        end
        return "Dict{$(_go_type(kt)),$(_go_type(vt))}(" * join(entries, ", ") * ")"
    end
    # Whole-expression fixed array literal; represented as a Julia Vector.
    mm = match(r"^\[(\d+)\]([A-Za-z_]\w*)\s*\{(.*)\}$", s)
    if mm !== nothing
        n, typ, body = mm.captures
        vals = [strip(x) for x in _split_top_level(body) if !isempty(strip(x))]
        length(vals) <= parse(Int, n) || throw(NativeTranspileError("too many values for Go array [$n]$typ"))
        while length(vals) < parse(Int, n); push!(vals, _go_zero(typ)); end
        return "$(_go_type(typ))[" * join(vals, ", ") * "]"
    end

    # make(...) common forms. Handle whole-expression forms explicitly so
    # type conversion is reliable on all supported Julia versions.
    m = match(r"^make\(\[\]([A-Za-z_]\w*),\s*0,\s*([^()]+)\)$", s)
    m !== nothing && return "sizehint!($(_go_type(m.captures[1]))[], $(m.captures[2]))"
    m = match(r"^make\(\[\]([A-Za-z_]\w*),\s*([^,()]+)\)$", s)
    m !== nothing && return "fill($(_go_zero(m.captures[1])), $(m.captures[2]))"
    m = match(r"^make\(map\[([^]]+)\]([^,)]+)\)$", s)
    m !== nothing && return "Dict{$(_go_type(m.captures[1])),$(_go_type(strip(m.captures[2])))}()"

    # append(slice, value) mutates in Julia; for the common `x = append(x,v)` case
    # assignment lowering strips the redundant assignment separately.
    s = replace(s, r"\bappend\s*\(([^,]+),\s*(.+)\)$" => s"push!(\1, \2)")
    s = _rewrite_composite_literals(s)
    s = _rewrite_indexing(s, state)
    s = replace(s, " &^ " => " & ~")
    s = _rewrite_typed_operators(s, state)
    return s
end

function _record_decl_type!(state::_GoState, name::AbstractString, typ::AbstractString)
    state.vars[String(name)] = strip(String(typ))
end

function _struct_constructor(name::String, fields::Vector{Tuple{String,String}})
    isempty(fields) && return "$name() = $name()" # never emitted; empty struct handled specially
    vals = join((_go_zero(t) for (_, t) in fields), ", ")
    return "$name() = $name($vals)"
end

function _native_go_line(line::AbstractString, indent::Int, state::_GoState)
    s = strip(String(line))
    isempty(s) && return (String[], indent)
    startswith(s, "//") && return ([repeat("    ", indent) * "#" * s[3:end]], indent)
    startswith(s, "package ") && return (String[], indent)
    startswith(s, "import ") && return (String[], indent)

    # Struct fields are parsed before ordinary statements.
    if state.current_struct !== nothing
        if s == "}"
            name = state.current_struct::String
            fields = copy(state.struct_fields)
            state.structs[name] = fields
            state.current_struct = nothing
            empty!(state.struct_fields)
            out = [repeat("    ", indent-1) * "end"]
            if !isempty(fields)
                push!(out, repeat("    ", indent-1) * _struct_constructor(name, fields))
            end
            return (out, indent-1)
        end
        m = match(r"^([A-Za-z_]\w*)\s+(.+?)(?:\s+`.*`)?$", s)
        m === nothing && throw(NativeTranspileError("unsupported struct field syntax: $s"))
        fname, ftype = m.captures
        push!(state.struct_fields, (fname, strip(ftype)))
        return ([repeat("    ", indent) * "$(fname)::$(_go_type(ftype))"], indent)
    end

    m = match(r"^type\s+([A-Za-z_]\w*)\s+struct\s*\{$", s)
    if m !== nothing
        state.current_struct = m.captures[1]
        empty!(state.struct_fields)
        return ([repeat("    ", indent) * "mutable struct $(m.captures[1])"], indent+1)
    end
    # Empty struct in one line.
    m = match(r"^type\s+([A-Za-z_]\w*)\s+struct\s*\{\s*\}$", s)
    m !== nothing && return ([repeat("    ", indent) * "struct $(m.captures[1]) end"], indent)

    if s == "}"
        indent -= 1
        indent < 0 && throw(NativeTranspileError("unbalanced closing brace"))
        return ([repeat("    ", indent) * "end"], indent)
    elseif startswith(s, "} else if ") && endswith(s, "{")
        indent -= 1
        cond = strip(s[length("} else if ")+1:end-1])
        return ([repeat("    ", indent) * "elseif " * _go_expr(cond, state)], indent+1)
    elseif s == "} else {"
        indent -= 1
        return ([repeat("    ", indent) * "else"], indent+1)
    end

    # Methods become ordinary Julia functions with the receiver as first argument.
    m = match(r"^func\s*\((\w+)\s+([^\)]+)\)\s*([A-Za-z_]\w*)\s*\((.*)\)\s*([^\{]*)\{$", s)
    if m !== nothing
        recv, recvtyp, name, params, ret = m.captures
        empty!(state.vars); state.vars[recv] = strip(recvtyp)
        parsed = _parse_go_params(params, state)
        allparams = isempty(parsed) ? "$(recv)::$(_go_type(recvtyp))" : "$(recv)::$(_go_type(recvtyp)), $parsed"
        return ([repeat("    ", indent) * "function $(name)($allparams)$(_go_return_annotation(ret))"], indent+1)
    end

    m = match(r"^func\s+([A-Za-z_]\w*)\s*\((.*)\)\s*([^\{]*)\{$", s)
    if m !== nothing
        name, params, ret = m.captures
        empty!(state.vars)
        signature = "function $(name)($(_parse_go_params(params, state)))$(_go_return_annotation(ret))"
        return ([repeat("    ", indent) * signature], indent+1)
    end

    m = match(r"^if\s+(.+)\s*\{$", s)
    m !== nothing && return ([repeat("    ", indent) * "if " * _go_expr(m.captures[1], state)], indent+1)

    # range loops; preserve Go's 0-based index value for slices/arrays.
    m = match(r"^for\s+([A-Za-z_]\w*)\s*:=\s*range\s+(.+)\s*\{$", s)
    if m !== nothing
        idx, seq = m.captures
        return ([repeat("    ", indent) * "for __go_idx in eachindex($(_go_expr(seq,state)))",
                 repeat("    ", indent+1) * "$(idx) = __go_idx - 1"], indent+1)
    end
    m = match(r"^for\s+_,\s*([A-Za-z_]\w*)\s*:=\s*range\s+(.+)\s*\{$", s)
    m !== nothing && return ([repeat("    ", indent) * "for $(m.captures[1]) in $(_go_expr(m.captures[2],state))"], indent+1)
    m = match(r"^for\s+([A-Za-z_]\w*),\s*([A-Za-z_]\w*)\s*:=\s*range\s+(.+)\s*\{$", s)
    if m !== nothing
        k, v, seq = m.captures
        seqs = strip(seq); typ = get(state.vars, seqs, "")
        if startswith(typ, "map[")
            return ([repeat("    ", indent) * "for ($(k), $(v)) in $(_go_expr(seq,state))"], indent+1)
        else
            return ([repeat("    ", indent) * "for (__go_idx, $(v)) in enumerate($(_go_expr(seq,state)))",
                     repeat("    ", indent+1) * "$(k) = __go_idx - 1"], indent+1)
        end
    end

    # Common counted loops.
    m = match(r"^for\s+([A-Za-z_]\w*)\s*:=\s*(.+);\s*\1\s*<\s*(.+);\s*\1\+\+\s*\{$", s)
    if m !== nothing
        i, start, stop = m.captures
        return ([repeat("    ", indent) * "for $(i) in $(_go_expr(start,state)):($(_go_expr(stop,state)) - 1)"], indent+1)
    end
    m = match(r"^for\s+([A-Za-z_]\w*)\s*:=\s*(.+);\s*\1\s*<=\s*(.+);\s*\1\+\+\s*\{$", s)
    if m !== nothing
        i, start, stop = m.captures
        return ([repeat("    ", indent) * "for $(i) in $(_go_expr(start,state)):$(_go_expr(stop,state))"], indent+1)
    end
    s == "for {" && return ([repeat("    ", indent) * "while true"], indent+1)
    m = match(r"^for\s+(.+)\s*\{$", s)
    m !== nothing && return ([repeat("    ", indent) * "while " * _go_expr(m.captures[1],state)], indent+1)

    s == "break" && return ([repeat("    ", indent) * "break"], indent)
    s == "continue" && return ([repeat("    ", indent) * "continue"], indent)

    m = match(r"^return(?:\s+(.*))?$", s)
    if m !== nothing
        value = m.captures[1]
        return ([repeat("    ", indent) * (value === nothing ? "return" : "return " * _go_expr(value,state))], indent)
    end

    # const declarations
    m = match(r"^const\s+([A-Za-z_]\w*)(?:\s+([^=]+))?\s*=\s*(.+)$", s)
    if m !== nothing
        name, typ, value = m.captures
        typ !== nothing && _record_decl_type!(state, name, strip(typ))
        return ([repeat("    ", indent) * "const $(name) = $(_go_expr(value,state))"], indent)
    end

    m = match(r"^var\s+([A-Za-z_]\w*)\s+([^=]+?)(?:\s*=\s*(.+))?$", s)
    if m !== nothing
        name, typ, value = m.captures
        typ = strip(typ); _record_decl_type!(state, name, typ)
        rhs = value === nothing ? _go_zero(typ) : _go_expr(value,state)
        return ([repeat("    ", indent) * "$(name)::$(_go_type(typ)) = $(rhs)"], indent)
    end

    # Multiple short declaration/assignment maps naturally to Julia tuple assignment.
    m = match(r"^([A-Za-z_]\w*(?:\s*,\s*[A-Za-z_]\w*)+)\s*:=\s*(.+)$", s)
    m !== nothing && return ([repeat("    ", indent) * "$(m.captures[1]) = $(_go_expr(m.captures[2],state))"], indent)

    m = match(r"^([A-Za-z_]\w*)\s*:=\s*(.+)$", s)
    if m !== nothing
        name, rhs = m.captures
        # Infer a few container types so later indexing/range lowering is correct.
        r = strip(rhs)
        mm = match(r"^make\((\[\][^,\)]+|map\[[^]]+\][^,\)]+)", r)
        mm !== nothing && _record_decl_type!(state, name, mm.captures[1])
        mm = match(r"^(\[\][A-Za-z_]\w*)\s*\{", r)
        mm !== nothing && _record_decl_type!(state, name, mm.captures[1])
        mm = match(r"^(map\[[^]]+\][A-Za-z_]\w*)\s*\{", r)
        mm !== nothing && _record_decl_type!(state, name, mm.captures[1])
        mm = match(r"^(\[\d+\][A-Za-z_]\w*)\s*\{", r)
        mm !== nothing && _record_decl_type!(state, name, mm.captures[1])
        mm = match(r"^([A-Z][A-Za-z0-9_]*)\s*\{", r)
        mm !== nothing && haskey(state.structs, mm.captures[1]) && _record_decl_type!(state, name, mm.captures[1])
        return ([repeat("    ", indent) * "$(name) = $(_go_expr(rhs,state))"], indent)
    end

    # x = append(x,v) -> push!(x,v), avoiding needless assignment.
    m = match(r"^([A-Za-z_]\w*)\s*=\s*append\(\1,\s*(.+)\)$", s)
    m !== nothing && return ([repeat("    ", indent) * "push!($(m.captures[1]), $(_go_expr(m.captures[2],state)))"], indent)

    # ordinary assignment, field assignment, indexed assignment, calls
    if occursin(r"^[A-Za-z_][\w\.\[\]]*\s*(=|\+=|-=|\*=|/=)", s) || occursin(r"^[A-Za-z_][\w\.]*\s*\(.*\)$", s)
        return ([repeat("    ", indent) * _go_expr(s,state)], indent)
    end
    m = match(r"^([A-Za-z_]\w*)\+\+$", s)
    m !== nothing && return ([repeat("    ", indent) * "$(m.captures[1]) += 1"], indent)
    m = match(r"^([A-Za-z_]\w*)--$", s)
    m !== nothing && return ([repeat("    ", indent) * "$(m.captures[1]) -= 1"], indent)

    throw(NativeTranspileError("unsupported Go syntax in native path: $s"))
end

"""
    native_go_to_julia(code) -> String

Translate Go directly in Julia. The native frontend supports functions, methods,
structs, scalar and container variables, zero values, common `for`/`range`
forms, slices/maps, basic indexing, and common fmt/strings/math calls. Unsupported
syntax raises `NativeTranspileError`; no external executable is used.
"""
function native_go_to_julia(code::AbstractString)
    text = replace(String(code), "\r\n" => "\n")
    out = String["# Generated by CodeTranspiler.jl native Go frontend"]
    indent = 0; import_depth = 0; state = _GoState()
    worklines = String[]
    for rawline in split(text, '\n'; keepempty=true)
        line = String(rawline)
        stripped = strip(line)
        m = match(r"^(func\s+[^{}]+\{)\s*(.*?)\s*\}$", stripped)
        if m !== nothing && !isempty(strip(m.captures[2]))
            push!(worklines, m.captures[1])
            for stmt in split(m.captures[2], ';')
                !isempty(strip(stmt)) && push!(worklines, strip(stmt))
            end
            push!(worklines, "}")
        else
            push!(worklines, line)
        end
    end
    for line in worklines
        s = strip(line)
        if startswith(s, "import (")
            import_depth = 1; continue
        elseif import_depth > 0
            s == ")" && (import_depth = 0)
            continue
        end
        lines, indent = _native_go_line(line, indent, state)
        append!(out, lines)
    end
    state.current_struct === nothing || throw(NativeTranspileError("unclosed Go struct: $(state.current_struct)"))
    indent == 0 || throw(NativeTranspileError("unbalanced Go braces: $indent block(s) still open"))
    if state.needs_printf
        insert!(out, 2, "using Printf")
    end
    return join(out, "\n") * "\n"
end
