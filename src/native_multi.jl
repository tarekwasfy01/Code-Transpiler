# Multi-language native frontends/targets built around Julia as the semantic pivot.
# These translators intentionally support a conservative common subset and raise
# NativeTranspileError when a construct cannot be lowered safely.

const _JULIA_TYPE_TO = Dict(
    "go" => Dict("Int"=>"int", "Int64"=>"int64", "Int32"=>"int32", "Float64"=>"float64", "Float32"=>"float32", "Bool"=>"bool", "String"=>"string", "Any"=>"any"),
    "r" => Dict("Int"=>"integer", "Int64"=>"integer", "Float64"=>"numeric", "Float32"=>"numeric", "Bool"=>"logical", "String"=>"character", "Any"=>"ANY"),
    "python" => Dict("Int"=>"int", "Int64"=>"int", "Float64"=>"float", "Float32"=>"float", "Bool"=>"bool", "String"=>"str", "Any"=>"object"),
    "rust" => Dict("Int"=>"i64", "Int64"=>"i64", "Int32"=>"i32", "Float64"=>"f64", "Float32"=>"f32", "Bool"=>"bool", "String"=>"String", "Any"=>"Box<dyn std::any::Any>"),
    "c" => Dict("Int"=>"long long", "Int64"=>"long long", "Int32"=>"int", "Float64"=>"double", "Float32"=>"float", "Bool"=>"bool", "String"=>"const char*", "Any"=>"void*"),
    "cpp" => Dict("Int"=>"long long", "Int64"=>"long long", "Int32"=>"int", "Float64"=>"double", "Float32"=>"float", "Bool"=>"bool", "String"=>"std::string", "Any"=>"std::any"),
    "zig" => Dict("Int"=>"i64", "Int64"=>"i64", "Int32"=>"i32", "Float64"=>"f64", "Float32"=>"f32", "Bool"=>"bool", "String"=>"[]const u8", "Any"=>"anytype"),
    "nim" => Dict("Int"=>"int", "Int64"=>"int64", "Int32"=>"int32", "Float64"=>"float64", "Float32"=>"float32", "Bool"=>"bool", "String"=>"string", "Any"=>"RootRef"),
    "csharp" => Dict("Int"=>"long", "Int64"=>"long", "Int32"=>"int", "Float64"=>"double", "Float32"=>"float", "Bool"=>"bool", "String"=>"string", "Any"=>"object"),
    "java" => Dict("Int"=>"long", "Int64"=>"long", "Int32"=>"int", "Float64"=>"double", "Float32"=>"float", "Bool"=>"boolean", "String"=>"String", "Any"=>"Object"),
    "kotlin" => Dict("Int"=>"Long", "Int64"=>"Long", "Int32"=>"Int", "Float64"=>"Double", "Float32"=>"Float", "Bool"=>"Boolean", "String"=>"String", "Any"=>"Any"),
    "swift" => Dict("Int"=>"Int", "Int64"=>"Int64", "Int32"=>"Int32", "Float64"=>"Double", "Float32"=>"Float", "Bool"=>"Bool", "String"=>"String", "Any"=>"Any"),
)

function _src_type_to_julia(lang::String, t::AbstractString)
    s = strip(String(t))
    s = replace(s, "const "=>"", "mut "=>"", "?"=>"")
    maps = Dict(
        "python"=>Dict("int"=>"Int", "float"=>"Float64", "bool"=>"Bool", "str"=>"String", "object"=>"Any", "None"=>"Nothing"),
        "rust"=>Dict("i8"=>"Int8", "i16"=>"Int16", "i32"=>"Int32", "i64"=>"Int64", "isize"=>"Int", "u8"=>"UInt8", "u16"=>"UInt16", "u32"=>"UInt32", "u64"=>"UInt64", "usize"=>"UInt", "f32"=>"Float32", "f64"=>"Float64", "bool"=>"Bool", "String"=>"String", "&str"=>"String"),
        "c"=>Dict("int"=>"Int32", "long"=>"Int", "long long"=>"Int64", "float"=>"Float32", "double"=>"Float64", "bool"=>"Bool", "char*"=>"String", "const char*"=>"String", "void"=>"Nothing"),
        "cpp"=>Dict("int"=>"Int32", "long"=>"Int", "long long"=>"Int64", "float"=>"Float32", "double"=>"Float64", "bool"=>"Bool", "std::string"=>"String", "string"=>"String", "void"=>"Nothing"),
        "zig"=>Dict("i8"=>"Int8", "i16"=>"Int16", "i32"=>"Int32", "i64"=>"Int64", "isize"=>"Int", "u8"=>"UInt8", "u16"=>"UInt16", "u32"=>"UInt32", "u64"=>"UInt64", "usize"=>"UInt", "f32"=>"Float32", "f64"=>"Float64", "bool"=>"Bool", "[]const u8"=>"String", "void"=>"Nothing"),
        "nim"=>Dict("int"=>"Int", "int64"=>"Int64", "int32"=>"Int32", "float"=>"Float64", "float64"=>"Float64", "float32"=>"Float32", "bool"=>"Bool", "string"=>"String"),
        "csharp"=>Dict("int"=>"Int32", "long"=>"Int64", "float"=>"Float32", "double"=>"Float64", "bool"=>"Bool", "string"=>"String", "object"=>"Any", "void"=>"Nothing"),
        "java"=>Dict("int"=>"Int32", "long"=>"Int64", "float"=>"Float32", "double"=>"Float64", "boolean"=>"Bool", "String"=>"String", "Object"=>"Any", "void"=>"Nothing"),
        "kotlin"=>Dict("Int"=>"Int32", "Long"=>"Int64", "Float"=>"Float32", "Double"=>"Float64", "Boolean"=>"Bool", "String"=>"String", "Any"=>"Any", "Unit"=>"Nothing"),
        "swift"=>Dict("Int"=>"Int", "Int64"=>"Int64", "Int32"=>"Int32", "Float"=>"Float32", "Double"=>"Float64", "Bool"=>"Bool", "String"=>"String", "Void"=>"Nothing"),
        "r"=>Dict("integer"=>"Int", "numeric"=>"Float64", "double"=>"Float64", "logical"=>"Bool", "character"=>"String"),
    )
    get(get(maps, lang, Dict{String,String}()), s, s)
end

function _typed_params_to_julia(lang::String, params::AbstractString)
    isempty(strip(params)) && return ""
    out = String[]
    for raw in _split_top_level(params)
        p = strip(raw)
        isempty(p) && continue
        p = replace(p, r"\b(final|const|mut|var|let|ref|in|out)\b\s*" => "")
        if lang in ("rust", "zig", "nim", "kotlin")
            m = match(r"^([A-Za-z_]\w*)\s*:\s*(.+)$", p)
            if m !== nothing
                push!(out, "$(m.captures[1])::$(_src_type_to_julia(lang, m.captures[2]))")
                continue
            end
        elseif lang == "swift"
            m = match(r"^(?:_\s+)?([A-Za-z_]\w*)\s*:\s*(.+)$", p)
            if m !== nothing
                push!(out, "$(m.captures[1])::$(_src_type_to_julia(lang, m.captures[2]))")
                continue
            end
        elseif lang in ("c", "cpp", "csharp", "java")
            m = match(r"^(.+?)\s+([A-Za-z_]\w*)$", p)
            if m !== nothing
                push!(out, "$(m.captures[2])::$(_src_type_to_julia(lang, m.captures[1]))")
                continue
            end
        elseif lang == "python"
            m = match(r"^([A-Za-z_]\w*)\s*:\s*(.+)$", p)
            if m !== nothing
                push!(out, "$(m.captures[1])::$(_src_type_to_julia(lang, m.captures[2]))")
                continue
            end
        end
        m = match(r"^([A-Za-z_]\w*)$", p)
        m !== nothing || throw(NativeTranspileError("unsupported $lang parameter syntax: $p"))
        push!(out, p)
    end
    join(out, ", ")
end

function _common_expr_to_julia(lang::String, expr::AbstractString)
    s = strip(String(expr))
    # literals/logical operators
    s = replace(s, r"\bTrue\b"=>"true", r"\bFalse\b"=>"false", r"\bNone\b"=>"nothing", r"\bNULL\b"=>"nothing", r"\bnull\b"=>"nothing", r"\bnil\b"=>"nothing")
    s = replace(s, "&&"=>" && ", "||"=>" || ")
    lang == "python" && (s = replace(s, "**"=>"^", "//"=>"÷", r"\band\b"=>"&&", r"\bor\b"=>"||", r"\bnot\b\s*"=>"!"))
    lang == "r" && (s = replace(s, "TRUE"=>"true", "FALSE"=>"false"))
    # common length/printing helpers
    s = replace(s, r"\blen\s*\("=>"length(", r"\.len\(\)"=>" |> length")
    s = replace(s, r"\bMath\.sqrt\s*\("=>"sqrt(", r"\bMath\.abs\s*\("=>"abs(")
    s = replace(s, r"\bstd::sqrt\s*\("=>"sqrt(", r"\bstd::abs\s*\("=>"abs(")
    return strip(s)
end

function _brace_source_to_julia(lang::String, code::String)
    out = String[]
    depth = 0
    wrapper_depth = 0
    zero_based_arrays = Set{String}()
    for raw in split(replace(code, "\r\n"=>"\n"), '\n')
        line = strip(raw)
        isempty(line) && continue
        startswith(line, "//") && continue
        startswith(line, "#include") && continue
        startswith(line, "using namespace") && continue
        # drop simple class/module wrappers rather than inventing object semantics
        if occursin(r"^(public\s+)?(class|namespace|object)\s+\w+\s*\{$", line)
            depth += 1
            wrapper_depth += 1
            continue
        end
        if line == "}"
            if depth <= wrapper_depth
                depth -= 1
                wrapper_depth = max(wrapper_depth - 1, 0)
                continue
            end
            depth -= 1
            depth < 0 && continue
            push!(out, "end")
            continue
        end
        line = replace(line, r";\s*$"=>"")
        if lang in ("c","cpp")
            am = match(r"^(?:const\s+)?(?:int|long|long long|float|double|bool)\s+([A-Za-z_]\w*)\s*\[(?:\d*)\]\s*=\s*\{(.*)\}$", line)
            if am !== nothing
                name, body = String.(am.captures); push!(zero_based_arrays, name)
                push!(out, "$name = [" * join(strip.(_split_top_level(body)), ", ") * "]")
                continue
            end
        end
        # function declarations
        m = nothing
        if lang in ("c", "cpp")
            m = match(r"^(?:static\s+|inline\s+|extern\s+)?([A-Za-z_][\w:\s<>*&]*)\s+([A-Za-z_]\w*)\s*\((.*)\)\s*\{$", line)
            if m !== nothing
                ret,name,params = m.captures
                push!(out, "function $name($(_typed_params_to_julia(lang, params)))::$(_src_type_to_julia(lang, strip(ret)))")
                depth += 1; continue
            end
        elseif lang in ("csharp", "java")
            m = match(r"^(?:(?:public|private|protected|internal)\s+)?(?:static\s+)?(?:final\s+)?([A-Za-z_][\w<>\[\]]*)\s+([A-Za-z_]\w*)\s*\((.*)\)\s*\{$", line)
            if m !== nothing
                ret,name,params = m.captures
                ann = _src_type_to_julia(lang, ret); suffix = ann == "Nothing" ? "" : "::$ann"
                push!(out, "function $name($(_typed_params_to_julia(lang, params)))$suffix")
                depth += 1; continue
            end
        elseif lang == "rust"
            m = match(r"^(?:pub\s+)?fn\s+([A-Za-z_]\w*)\s*\((.*)\)\s*(?:->\s*([^\{]+))?\s*\{$", line)
            if m !== nothing
                name,params,ret = m.captures; suffix = ret === nothing ? "" : "::$(_src_type_to_julia(lang, ret))"
                push!(out, "function $name($(_typed_params_to_julia(lang, params)))$suffix")
                depth += 1; continue
            end
        elseif lang == "zig"
            m = match(r"^(?:pub\s+)?fn\s+([A-Za-z_]\w*)\s*\((.*)\)\s*([^\{]*)\{$", line)
            if m !== nothing
                name,params,ret = m.captures; ret = strip(ret); suffix = isempty(ret) || ret == "void" ? "" : "::$(_src_type_to_julia(lang, ret))"
                push!(out, "function $name($(_typed_params_to_julia(lang, params)))$suffix")
                depth += 1; continue
            end
        elseif lang == "kotlin"
            m = match(r"^fun\s+([A-Za-z_]\w*)\s*\((.*)\)\s*(?::\s*([^\{]+))?\s*\{$", line)
            if m !== nothing
                name,params,ret = m.captures; suffix = ret === nothing ? "" : "::$(_src_type_to_julia(lang, ret))"
                push!(out, "function $name($(_typed_params_to_julia(lang, params)))$suffix")
                depth += 1; continue
            end
        elseif lang == "swift"
            m = match(r"^func\s+([A-Za-z_]\w*)\s*\((.*)\)\s*(?:->\s*([^\{]+))?\s*\{$", line)
            if m !== nothing
                name,params,ret = m.captures; suffix = ret === nothing ? "" : "::$(_src_type_to_julia(lang, ret))"
                push!(out, "function $name($(_typed_params_to_julia(lang, params)))$suffix")
                depth += 1; continue
            end
        end
        # branches/loops
        m = match(r"^if\s*\((.*)\)\s*\{$", line); m === nothing && (m = match(r"^if\s+(.+)\s*\{$", line))
        if m !== nothing; push!(out, "if $(_common_expr_to_julia(lang,m.captures[1]))"); depth+=1; continue; end
        m = match(r"^(?:else\s+)?if\s*\((.*)\)\s*\{$", line)
        if m !== nothing; push!(out, "elseif $(_common_expr_to_julia(lang,m.captures[1]))"); continue; end
        if occursin(r"^}\s*else\s*{$", line) || line in ("else {", "else{"); push!(out,"else"); continue; end
        # Common counted/range loops. All represented with source-language index semantics.
        m = match(r"^for\s*\(\s*(?:int|long|var|let)?\s*([A-Za-z_]\w*)\s*=\s*([^;]+);\s*\1\s*<\s*([^;]+);\s*(?:\1\+\+|\+\+\1)\s*\)\s*\{$", line)
        if m !== nothing; push!(out,"for $(m.captures[1]) in $(strip(m.captures[2])):($(strip(m.captures[3])) - 1)"); depth+=1; continue; end
        m = match(r"^for\s+([A-Za-z_]\w*)\s+in\s+([^.]*)\.\.([^=].*)\s*\{$", line)
        if m !== nothing; push!(out,"for $(m.captures[1]) in $(strip(m.captures[2])):($(strip(m.captures[3])) - 1)"); depth+=1; continue; end
        m = match(r"^for\s*\(\s*([A-Za-z_]\w*)\s+in\s+([^ ]+)\s+until\s+([^\)]+)\)\s*\{$", line)
        if m !== nothing; push!(out,"for $(m.captures[1]) in $(strip(m.captures[2])):($(strip(m.captures[3])) - 1)"); depth+=1; continue; end
        m = match(r"^for\s+([A-Za-z_]\w*)\s+in\s+([^.]*)\.\.<([^ ]+)\s*\{$", line)
        if m !== nothing; push!(out,"for $(m.captures[1]) in $(strip(m.captures[2])):($(strip(m.captures[3])) - 1)"); depth+=1; continue; end
        m = match(r"^while\s*\((.*)\)\s*\{$", line); m === nothing && (m = match(r"^while\s+(.+)\s*\{$", line))
        if m !== nothing; push!(out,"while $(_common_expr_to_julia(lang,m.captures[1]))"); depth+=1; continue; end
        if lang in ("c","cpp")
            for name in zero_based_arrays
                esc=replace(name,r"([.^$|()\[\]{}*+?\\])"=>s"\\\1")
                line=replace(line,Regex("\\b"*esc*"\\[([^]\\[]+)\\]")=>SubstitutionString(name*"[(\\1) + 1]"))
            end
        end
        # return / declarations
        m = match(r"^return(?:\s+(.+))?$", line)
        if m !== nothing; push!(out, m.captures[1] === nothing ? "return" : "return $(_common_expr_to_julia(lang,m.captures[1]))"); continue; end
        line = replace(line, r"^(?:let\s+mut|let|const|var)\s+"=>"")
        line = replace(line, r"^(?:final\s+)?(?:int|long|float|double|bool|boolean|string|String|auto|i\d+|u\d+|usize|isize|f32|f64)\s+([A-Za-z_]\w*)\s*="=>s"\1 =")
        # print APIs
        line = replace(line, r"^System\.out\.println\s*\("=>"println(", r"^Console\.WriteLine\s*\("=>"println(", r"^std::cout\s*<<\s*(.+)\s*<<\s*std::endl$"=>s"println(\1)")
        line = replace(line, r"^println!\s*\("=>"println(", r"^std\.debug\.print\s*\("=>"print(")
        if lang in ("c","cpp")
            pm=match(Regex(raw"""^printf\s*\(\s*"[^"]*%[diufgslc][^"]*"\s*,\s*(.+)\)$"""), line)
            pm!==nothing && (line="println("*String(pm.captures[1])*")")
            line=replace(line,r"^puts\s*\("=>"println(")
            line=replace(line,r"^([A-Za-z_]\w*)\+\+$"=>s"\1 += 1", r"^([A-Za-z_]\w*)--$"=>s"\1 -= 1")
        end
        push!(out, _common_expr_to_julia(lang, line))
    end
    while depth > 0; push!(out,"end"); depth -= 1; end
    return join(out, "\n") * "\n"
end

function _indent_source_to_julia(lang::String, code::String)
    out = String[]
    # (header indentation, kind). A block ends when the next non-continuation
    # statement is at or left of the header indentation.
    blocks = Tuple{Int,Symbol}[]
    zero_based_arrays = Set{String}()
    lines = split(replace(code,"\r\n"=>"\n"),'\n')

    for raw in lines
        isempty(strip(raw)) && continue
        startswith(strip(raw), "#") && continue
        n = length(raw) - length(lstrip(raw))
        line = strip(raw)
        continuation = startswith(line,"elif ") || line == "else:"

        while !isempty(blocks)
            bi,bk = last(blocks)
            if n < bi || (n == bi && !(continuation && bk == :if))
                pop!(blocks); push!(out,"end")
            else
                break
            end
        end

        opened_kind = nothing
        if lang == "python"
            m = match(r"^def\s+([A-Za-z_]\w*)\s*\((.*)\)\s*(?:->\s*([^:]+))?:$", line)
            if m !== nothing
                name,params,ret=m.captures; suffix=ret===nothing ? "" : "::$(_src_type_to_julia(lang,ret))"
                push!(out,"function $name($(_typed_params_to_julia(lang,params)))$suffix"); opened_kind=:function
            elseif (m=match(r"^if\s+(.+):$",line)) !== nothing
                push!(out,"if $(_common_expr_to_julia(lang,m.captures[1]))"); opened_kind=:if
            elseif (m=match(r"^elif\s+(.+):$",line)) !== nothing
                isempty(blocks) || last(blocks)[2] == :if || throw(NativeTranspileError("python elif without matching if"))
                push!(out,"elseif $(_common_expr_to_julia(lang,m.captures[1]))")
            elseif line == "else:"
                isempty(blocks) || last(blocks)[2] == :if || throw(NativeTranspileError("python else without matching if"))
                push!(out,"else")
            elseif (m=match(r"^while\s+(.+):$",line)) !== nothing
                push!(out,"while $(_common_expr_to_julia(lang,m.captures[1]))"); opened_kind=:while
            elseif (m=match(r"^for\s+([A-Za-z_]\w*)\s+in\s+range\(([^,()]+)\):$",line)) !== nothing
                push!(out,"for $(m.captures[1]) in 0:($(strip(m.captures[2])) - 1)"); opened_kind=:for
            elseif (m=match(r"^for\s+([A-Za-z_]\w*)\s+in\s+range\(([^,]+),\s*([^,\)]+)\):$",line)) !== nothing
                push!(out,"for $(m.captures[1]) in $(strip(m.captures[2])):($(strip(m.captures[3])) - 1)"); opened_kind=:for
            elseif (m=match(r"^for\s+([A-Za-z_]\w*)\s+in\s+(.+):$",line)) !== nothing
                push!(out,"for $(m.captures[1]) in $(_common_expr_to_julia(lang,m.captures[2]))"); opened_kind=:for
            elseif (m=match(r"^return(?:\s+(.+))?$",line)) !== nothing
                expr=m.captures[1]
                if expr===nothing
                    push!(out,"return")
                else
                    e=String(expr)
                    for name in zero_based_arrays
                        esc=replace(name,r"([.^$|()\[\]{}*+?\\])"=>s"\\\1")
                        e=replace(e,Regex("\\b"*esc*"\\[([^]\\[]+)\\]")=>SubstitutionString(name*"[(\\1) + 1]"))
                    end
                    push!(out,"return $(_common_expr_to_julia(lang,e))")
                end
            else
                # Track ordinary list literals so zero-based Python indexing can
                # be normalized when collection identity is known.
                am=match(r"^([A-Za-z_]\w*)\s*=\s*\[(.*)\]$",line)
                am !== nothing && push!(zero_based_arrays,String(am.captures[1]))
                for name in zero_based_arrays
                    esc=replace(name,r"([.^$|()\[\]{}*+?\\])"=>s"\\\1")
                    line=replace(line,Regex("\\b"*esc*"\\[([^]\\[]+)\\]")=>SubstitutionString(name*"[(\\1) + 1]"))
                end
                push!(out,_common_expr_to_julia(lang,line))
            end
        else # nim
            m=match(r"^proc\s+([A-Za-z_]\w*)\s*\((.*)\)\s*(?::\s*([^=]+))?\s*=\s*$",line)
            if m !== nothing
                name,params,ret=m.captures; suffix=ret===nothing ? "" : "::$(_src_type_to_julia(lang,ret))"
                push!(out,"function $name($(_typed_params_to_julia(lang,params)))$suffix"); opened_kind=:function
            elseif (m=match(r"^if\s+(.+):$",line)) !== nothing
                push!(out,"if $(_common_expr_to_julia(lang,m.captures[1]))"); opened_kind=:if
            elseif (m=match(r"^elif\s+(.+):$",line)) !== nothing
                push!(out,"elseif $(_common_expr_to_julia(lang,m.captures[1]))")
            elseif line == "else:"
                push!(out,"else")
            elseif (m=match(r"^while\s+(.+):$",line)) !== nothing
                push!(out,"while $(_common_expr_to_julia(lang,m.captures[1]))"); opened_kind=:while
            elseif (m=match(r"^return(?:\s+(.+))?$",line)) !== nothing
                push!(out,m.captures[1]===nothing ? "return" : "return $(_common_expr_to_julia(lang,m.captures[1]))")
            else
                line=replace(line,r"^(let|var|const)\s+"=>"")
                line=replace(line,r"^echo\s+"=>"println(")
                startswith(line,"println(") && !endswith(line,")") && (line *= ")")
                push!(out,_common_expr_to_julia(lang,line))
            end
        end
        opened_kind !== nothing && push!(blocks,(n,opened_kind))
    end
    while !isempty(blocks); pop!(blocks); push!(out,"end"); end
    join(out,"\n")*"\n"
end

function _r_to_julia(code::String)
    out=String[]; depth=0
    for raw in split(replace(code,"\r\n"=>"\n"),'\n')
        line=strip(raw); isempty(line)&&continue; startswith(line,"#")&&continue
        if line=="}"; push!(out,"end"); depth=max(depth-1,0); continue; end
        m=match(r"^([A-Za-z_.]\w*)\s*<-\s*function\s*\((.*)\)\s*\{$",line)
        if m!==nothing; push!(out,"function $(m.captures[1])($(m.captures[2]))"); depth+=1; continue; end
        m=match(r"^if\s*\((.*)\)\s*\{$",line); if m!==nothing; push!(out,"if $(_common_expr_to_julia("r",m.captures[1]))"); depth+=1; continue; end
        line=replace(line,"<-"=>"=")
        line=replace(line,r"^print\s*\("=>"println(")
        push!(out,_common_expr_to_julia("r",line))
    end
    while depth>0; push!(out,"end"); depth-=1; end
    join(out,"\n")*"\n"
end

function native_to_julia(source::AbstractString, code::AbstractString)
    lang = normalize_language(source); text=String(code)
    lang=="julia" && return text
    lang=="se" && return semantic_exchange_pivot(text)
    lang=="go" && return native_go_to_julia(text)
    lang=="python" && return _indent_source_to_julia("python",text)
    lang=="nim" && return _indent_source_to_julia("nim",text)
    lang=="r" && return _r_to_julia(text)
    lang in ("rust","c","cpp","zig","csharp","java","kotlin","swift") && return _brace_source_to_julia(lang,text)
    throw(NativeTranspileError("native frontend not implemented: $lang"))
end

function _jtype_to(lang::String,t::AbstractString)
    jt=strip(String(t))
    vm=match(r"^Vector\{(.+)\}$",jt)
    if vm !== nothing
        elem=_jtype_to(lang,vm.captures[1])
        if lang == "java"
            boxed = vm.captures[1] in ("Int","Int64") ? "Long" : vm.captures[1] == "Int32" ? "Integer" : vm.captures[1] == "Float64" ? "Double" : vm.captures[1] == "Float32" ? "Float" : vm.captures[1] == "Bool" ? "Boolean" : elem
            return "java.util.ArrayList<$boxed>"
        end
        return lang=="go" ? "[]$elem" : lang=="rust" ? "Vec<$elem>" : lang=="c" ? "$elem*" : lang=="cpp" ? "std::vector<$elem>" : lang=="python" ? "list[$elem]" : lang=="r" ? "vector" : lang=="zig" ? "[]$elem" : lang=="nim" ? "seq[$elem]" : lang=="csharp" ? "List<$elem>" : lang=="kotlin" ? "MutableList<$elem>" : lang=="swift" ? "[$elem]" : "Any"
    end
    return get(get(_JULIA_TYPE_TO,lang,Dict{String,String}()),jt,get(get(_JULIA_TYPE_TO,lang,Dict{String,String}()),"Any","Any"))
end

function _parse_julia_signature(line::AbstractString)
    m=match(r"^function\s+([A-Za-z_]\w*)\s*\((.*)\)\s*(?:::\s*([^\s]+))?$",strip(line)); m===nothing && return nothing
    name,raw,ret=m.captures; params=Tuple{String,String}[]
    for p in _split_top_level(raw)
        isempty(strip(p))&&continue
        mm=match(r"^([A-Za-z_]\w*)\s*(?:::\s*(.+))?$",strip(p)); mm===nothing && throw(NativeTranspileError("unsupported Julia parameter: $p"))
        push!(params,(mm.captures[1], mm.captures[2]===nothing ? "Any" : strip(mm.captures[2])))
    end
    return (name=name,params=params,ret=ret===nothing ? "Any" : strip(ret))
end


function _target_index_expr(lang::String, idx::AbstractString)
    i = strip(String(idx))
    lang == "rust" && return "(($i) as usize)"
    lang == "java" && return "((int)($i))"
    lang == "csharp" && return "((int)($i))"
    lang == "kotlin" && return "(($i).toInt())"
    lang == "swift" && return "Int($i)"
    lang == "zig" && return "@as(usize, @intCast($i))"
    lang == "nim" && return "int($i)"
    return i
end

function _target_index_access(lang::String, name::String, idx::AbstractString)
    i = _target_index_expr(lang, idx)
    lang == "java" && return "$name.get($i)"
    return "$name[$i]"
end

function _target_expr(lang::String,s::AbstractString,vartypes::Dict{String,String}=Dict{String,String}())
    x=strip(String(s))
    if lang=="python"; x=replace(x,"true"=>"True","false"=>"False","nothing"=>"None","&&"=>"and","||"=>"or")
    elseif lang=="r"; x=replace(x,"true"=>"TRUE","false"=>"FALSE","nothing"=>"NULL","&&"=>"&&","||"=>"||")
    elseif lang in ("c","cpp","csharp","java","kotlin","rust","zig","swift","nim","go"); x=replace(x,"true"=>"true","false"=>"false","nothing"=> (lang=="swift" ? "nil" : lang in ("c","cpp") ? "NULL" : lang=="go" ? "nil" : "null"))
    end
    # collection length uses target-native spelling.
    length_re = r"\blength\(([A-Za-z_]\w*)\)"
    for mm in collect(eachmatch(length_re, x))
        n=String(mm.captures[1])
        repl = lang=="python" ? "len($n)" : lang=="r" ? "length($n)" : lang=="go" ? "int64(len($n))" : lang=="rust" ? "($n.len() as i64)" : lang=="swift" ? "Int64($n.count)" : lang=="java" ? "$n.size()" : lang=="csharp" ? "$n.Count" : lang=="kotlin" ? "$n.size" : lang=="nim" ? "$n.len" : lang=="zig" ? "@as(i64, @intCast($n.len))" : lang=="cpp" ? "((long long)$n.size())" : lang=="c" ? "((long long)(sizeof($n) / sizeof($n[0])))" : "$n.len"
        x=replace(x,String(mm.match)=>repl; count=1)
    end
    # Julia pivot is one-based. Frontends from zero-based languages encode a
    # normalized access as container[(source_index) + 1]. For known vectors we
    # can recover source/target indexing exactly instead of textual guessing.
    if lang in _ZERO_BASED_LANGS
        # Protect normalized source-index accesses before converting any direct
        # one-based Julia pivot accesses. This prevents a recovered `xs[i]`
        # from being shifted a second time to `xs[i-1]`.
        protected = Pair{String,String}[]
        serial = 0
        for (name,jt) in vartypes
            startswith(jt,"Vector{") || continue
            esc=replace(name,r"([.^$|()\[\]{}*+?\\])"=>s"\\\1")
            normalized = Regex("\\b"*esc*"\\[\\(([^]\\[]+)\\)\\s*\\+\\s*1\\]")
            for mm in collect(eachmatch(normalized, x))
                serial += 1
                token = "__CT_INDEX_$(serial)__"
                push!(protected, token => _target_index_access(lang, name, String(mm.captures[1])))
                x = replace(x, String(mm.match) => token; count=1)
            end
            direct = Regex("\\b"*esc*"\\[([A-Za-z_]\\w*|\\d+)\\]")
            for mm in collect(eachmatch(direct, x))
                full=String(mm.match); idx=String(mm.captures[1])
                x=replace(x, full => _target_index_access(lang, name, "(" * idx * ") - 1"); count=1)
            end
        end
        for (token,replacement) in protected
            x=replace(x,token=>replacement)
        end
    end
    return x
end

function _infer_julia_expr_type(expr::AbstractString, vartypes::Dict{String,String})
    e=strip(String(expr))
    haskey(vartypes,e) && return vartypes[e]
    if startswith(e,"[") && endswith(e,"]")
        body=strip(e[2:end-1])
        isempty(body) && return "Vector{Any}"
        ts=[_infer_julia_expr_type(strip(v),vartypes) for v in _split_top_level(body)]
        t=all(==(first(ts)),ts) ? first(ts) : "Any"
        return "Vector{$t}"
    end
    occursin(r"^[+-]?\d+$",e) && return "Int64"
    occursin(r"^[+-]?(?:\d+\.\d*|\d*\.\d+)(?:[eE][+-]?\d+)?$",e) && return "Float64"
    (e=="true" || e=="false") && return "Bool"
    startswith(e,"\"") && endswith(e,"\"") && return "String"
    for op in ("+","-","*","/","÷","%")
        parts=split(e,op; limit=2)
        if length(parts)==2
            a=strip(parts[1]); b=strip(parts[2]); ta=get(vartypes,a,_infer_julia_expr_type(a,vartypes)); tb=get(vartypes,b,_infer_julia_expr_type(b,vartypes))
            ta=="String" && tb=="String" && op in ("+","*") && return "String"
            ta=="Float64" || tb=="Float64" ? (return "Float64") : (ta!="Any" && return ta)
        end
    end
    return "Any"
end

function _target_literal(lang::String, value::AbstractString, jtype::AbstractString)
    v=strip(String(value)); t=String(jtype)
    if lang in ("java","kotlin") && t in ("Int","Int64") && occursin(r"^[+-]?\d+$",v)
        return v * "L"
    elseif lang in ("java","kotlin") && t == "Float32" && occursin(r"^[+-]?(?:\d+\.\d*|\d*\.\d+)$",v)
        return v * "f"
    end
    return v
end

function _declare_target_assignment(lang::String, name::String, expr::String, jtype::String)
    t=_jtype_to(lang,jtype)
    vm=match(r"^Vector\{(.+)\}$",jtype)
    if vm !== nothing && startswith(strip(expr),"[") && endswith(strip(expr),"]")
        body=strip(expr)[2:end-1]; elem=_jtype_to(lang,vm.captures[1]); vals=isempty(strip(body)) ? String[] : [_target_literal(lang,v,vm.captures[1]) for v in _split_top_level(body)]; joined=join(vals,", ")
        return lang=="go" ? "$name := []$elem{$joined}" :
               lang=="rust" ? "let mut $name: Vec<$elem> = vec![$joined]" :
               lang=="c" ? "$elem $name[] = {$joined}" :
               lang=="cpp" ? "std::vector<$elem> $name = {$joined}" :
               lang=="python" ? "$name = [$joined]" :
               lang=="r" ? "$name = c($joined)" :
               lang=="zig" ? "var $name = [_]$elem{$joined}" :
               lang=="nim" ? "var $name: seq[$elem] = @[$joined]" :
               lang=="csharp" ? "List<$elem> $name = new List<$elem> {$joined}" :
               lang=="java" ? "$(_jtype_to(lang,jtype)) $name = new java.util.ArrayList<>(java.util.Arrays.asList($joined))" :
               lang=="kotlin" ? "var $name: MutableList<$elem> = mutableListOf($joined)" :
               lang=="swift" ? "var $name: [$elem] = [$joined]" : "$name = [$joined]"
    end
    if lang=="go"; return "$name := $expr"
    elseif lang=="rust"; return "let mut $name: $t = $expr"
    elseif lang in ("c","cpp","csharp","java"); return "$t $name = $expr"
    elseif lang=="zig"; return "var $name: $t = $expr"
    elseif lang=="nim"; return "var $name: $t = $expr"
    elseif lang=="kotlin"; return "var $name: $t = $expr"
    elseif lang=="swift"; return "var $name: $t = $expr"
    end
    return "$name = $expr"
end

function _target_append(lang::String,name::String,value::String,jtype::String="Any")
    vm=match(r"^Vector\{(.+)\}$",jtype); elemtype=vm===nothing ? jtype : String(vm.captures[1])
    value=_target_literal(lang,value,elemtype)
    lang=="go" && return "$name = append($name, $value)"
    lang=="rust" && return "$name.push($value)"
    lang=="cpp" && return "$name.push_back($value)"
    lang=="python" && return "$name.append($value)"
    lang=="r" && return "$name = c($name, $value)"
    lang=="csharp" && return "$name.Add($value)"
    lang=="java" && return "$name.add($value)"
    lang=="kotlin" && return "$name.add($value)"
    lang=="swift" && return "$name.append($value)"
    lang=="nim" && return "$name.add($value)"
    lang=="zig" && throw(NativeTranspileError("Julia Vector push! -> Zig requires allocator-aware ArrayList lowering; unsupported in binary-free core"))
    lang=="c" && throw(NativeTranspileError("Julia Vector push! -> C requires explicit capacity/allocation semantics; unsupported in binary-free core"))
    return "push!($name, $value)"
end

function _target_print(lang::String,inner::String,vartypes::Dict{String,String})
    typ=_infer_julia_expr_type(inner,vartypes)
    if lang=="python"; return "print($inner)"
    elseif lang=="r"; return "print($inner)"
    elseif lang=="go"; return "fmt.Println($inner)"
    elseif lang=="rust"; return "println!(\"{}\", $inner)"
    elseif lang=="c"
        return typ=="String" ? "printf(\"%s\\n\", $inner)" : typ in ("Float32","Float64") ? "printf(\"%g\\n\", (double)($inner))" : "printf(\"%lld\\n\", (long long)($inner))"
    elseif lang=="cpp"; return "std::cout << $inner << std::endl"
    elseif lang=="csharp"; return "Console.WriteLine($inner)"
    elseif lang=="java"; return "System.out.println($inner)"
    elseif lang=="kotlin"; return "println($inner)"
    elseif lang=="swift"; return "print($inner)"
    elseif lang=="nim"; return "echo $inner"
    elseif lang=="zig"; return "std.debug.print(\"{}\\n\", .{$inner})"
    end
    return "println($inner)"
end

function _target_prologue(lang::String)
    lang=="go" && return ["package main", ""]
    lang=="c" && return ["#include <stdbool.h>", "#include <stdio.h>", ""]
    lang=="cpp" && return ["#include <any>", "#include <iostream>", "#include <string>", "#include <vector>", ""]
    lang=="zig" && return String[]
    lang=="csharp" && return ["using System;", "using System.Collections.Generic;", "", "public static class Transpiled {"]
    lang=="java" && return ["public final class Transpiled {"]
    return String[]
end

function _target_epilogue(lang::String)
    lang in ("csharp","java") && return ["}"]
    return String[]
end

function _emit_julia_to(lang::String, code::String)
    lang=="julia" && return code
    lines=split(replace(code,"\r\n"=>"\n"),'\n'); out=_target_prologue(lang); stack=Symbol[]; indent=(lang in ("csharp","java") ? 1 : 0); declared=Set{String}(); vartypes=Dict{String,String}()
    emit(s)=push!(out, repeat("    ",indent)*s)
    for raw in lines
        line=strip(raw); isempty(line)&&continue; startswith(line,"#") && (emit(lang=="python" ? "#"*line[2:end] : "//"*line[2:end]); continue)
        sig=_parse_julia_signature(line)
        if sig!==nothing
            params=sig.params; ret=sig.ret
            empty!(declared); empty!(vartypes)
            for (n,t) in params; push!(declared,n); vartypes[n]=t; end
            if lang=="python"
                p=join(["$(n): $(_jtype_to(lang,t))" for (n,t) in params],", "); emit("def $(sig.name)($p) -> $(_jtype_to(lang,ret)):"); indent+=1
            elseif lang=="go"
                p=join(["$n $(_jtype_to(lang,t))" for (n,t) in params],", "); emit("func $(sig.name)($p) $(_jtype_to(lang,ret)) {"); indent+=1
            elseif lang=="r"
                emit("$(sig.name) <- function($(join(first.(params),", "))) {"); indent+=1
            elseif lang=="rust"
                p=join(["$n: $(_jtype_to(lang,t))" for (n,t) in params],", "); emit("fn $(sig.name)($p) -> $(_jtype_to(lang,ret)) {"); indent+=1
            elseif lang in ("c","cpp")
                p=join(["$(_jtype_to(lang,t)) $n" for (n,t) in params],", "); emit("$(_jtype_to(lang,ret)) $(sig.name)($p) {"); indent+=1
            elseif lang=="zig"
                p=join(["$n: $(_jtype_to(lang,t))" for (n,t) in params],", "); emit("fn $(sig.name)($p) $(_jtype_to(lang,ret)) {"); indent+=1
            elseif lang=="nim"
                p=join(["$n: $(_jtype_to(lang,t))" for (n,t) in params],", "); emit("proc $(sig.name)($p): $(_jtype_to(lang,ret)) ="); indent+=1
            elseif lang in ("csharp","java")
                p=join(["$(_jtype_to(lang,t)) $n" for (n,t) in params],", "); emit("static $(_jtype_to(lang,ret)) $(sig.name)($p) {"); indent+=1
            elseif lang=="kotlin"
                p=join(["$n: $(_jtype_to(lang,t))" for (n,t) in params],", "); emit("fun $(sig.name)($p): $(_jtype_to(lang,ret)) {"); indent+=1
            elseif lang=="swift"
                p=join(["_ $n: $(_jtype_to(lang,t))" for (n,t) in params],", "); emit("func $(sig.name)($p) -> $(_jtype_to(lang,ret)) {"); indent+=1
            end
            push!(stack,:block); continue
        end
        if line=="end"
            isempty(stack) && continue
            indent=max(indent-1,0); pop!(stack)
            if lang ∉ ("python","nim"); emit("}"); end
            continue
        end
        m=match(r"^if\s+(.+)$",line)
        if m!==nothing
            cond=_target_expr(lang,m.captures[1],vartypes);
            if lang in ("python","nim"); emit("if $cond:"); else emit("if ($cond) {"); end
            indent+=1; push!(stack,:block); continue
        end
        m=match(r"^elseif\s+(.+)$",line)
        if m!==nothing
            cond=_target_expr(lang,m.captures[1],vartypes); indent=max(indent-1,0)
            if lang=="python"; emit("elif $cond:") elseif lang=="nim"; emit("elif $cond:") else emit("} else if ($cond) {") end
            indent+=1; continue
        end
        if line=="else"
            indent=max(indent-1,0); if lang in ("python","nim"); emit("else:") else emit("} else {") end; indent+=1; continue
        end
        m=match(r"^for\s+([A-Za-z_]\w*)\s+in\s+(.+):\((.+)\s*-\s*1\)$",line)
        if m!==nothing
            v,a,b=String.(m.captures); a=_target_expr(lang,a,vartypes); b=_target_expr(lang,b,vartypes)
            if lang=="python"; emit("for $v in range($a, $b):")
            elseif lang=="r"; emit("for ($v in $a:($b - 1)) {")
            elseif lang in ("c","cpp"); emit("for (long long $v = $a; $v < $b; ++$v) {")
            elseif lang=="go"; emit("for $v := $a; $v < $b; $v++ {")
            elseif lang=="rust"; emit("for $v in $a..$b {")
            elseif lang=="zig"; emit("for ($a..$b) |$v| {")
            elseif lang=="nim"; emit("for $v in $a ..< $b:")
            elseif lang in ("csharp","java"); emit("for (long $v = $a; $v < $b; $v++) {")
            elseif lang=="kotlin"; emit("for ($v in $a until $b) {")
            elseif lang=="swift"; emit("for $v in $a..<$b {")
            end
            indent+=1; push!(stack,:block); continue
        end
        m=match(r"^while\s+(.+)$",line)
        if m!==nothing
            cond=_target_expr(lang,m.captures[1],vartypes); if lang in ("python","nim"); emit("while $cond:") else emit("while ($cond) {") end; indent+=1; push!(stack,:block); continue
        end
        stmt=_target_expr(lang,line,vartypes)
        pm=match(r"^push!\(\s*([A-Za-z_]\w*)\s*,\s*(.+)\)$",stmt)
        if pm !== nothing
            name=String(pm.captures[1]); stmt=_target_append(lang,name,String(pm.captures[2]), get(vartypes,name,"Any"))
        end
        am=match(r"^([A-Za-z_]\w*)\s*=\s*(.+)$",stmt)
        if am !== nothing
            n=String(am.captures[1]); ex=String(am.captures[2])
            if !(n in declared)
                jt=_infer_julia_expr_type(ex,vartypes); vartypes[n]=jt; push!(declared,n)
                stmt=_declare_target_assignment(lang,n,ex,jt)
            end
        end
        if startswith(stmt,"println(")
            inner=stmt[9:end-1]
            stmt=_target_print(lang,inner,vartypes)
        end
        if lang in ("python","r","nim"); emit(stmt) elseif lang=="go"; emit(stmt) else emit(stmt * ";") end
    end
    while !isempty(stack)
        indent=max(indent-1,0); pop!(stack); lang ∉ ("python","nim") && emit("}")
    end
    if lang=="go" && any(occursin("fmt.", x) for x in out)
        insert!(out, 2, "import \"fmt\"")
    elseif lang=="zig" && any(occursin("std.", x) for x in out)
        insert!(out, 1, "const std = @import(\"std\");")
        insert!(out, 2, "")
    end
    append!(out,_target_epilogue(lang))
    return join(out,"\n")*"\n"
end

function native_from_julia(target::AbstractString, code::AbstractString)
    lang=normalize_language(target)
    lang=="se" && return semantic_exchange("julia", String(code); pivot=String(code))
    _emit_julia_to(lang,String(code))
end

"""Return true when a route has a native implementation through the Julia pivot."""
native_route_supported(source::AbstractString,target::AbstractString) = has_frontend(source) && has_backend(target)
