"""Description of a language supported by the Code Transpiler backend."""
struct Language
    id::String
    name::String
    extensions::Vector{String}
    aliases::Vector{String}
end

const _LANGUAGES = Language[
    Language("se",      "SE (Semantic Exchange)", [".se"], ["semantic", "semantics", "semantic-exchange"]),
    Language("r",       "R",      [".R", ".r"],              String[]),
    Language("go",      "Go",     [".go"],                    String[]),
    Language("rust",    "Rust",   [".rs"],                    ["rs"]),
    Language("cpp",     "C++",    [".cpp", ".cc", ".cxx"],  ["c++"]),
    Language("c",       "C",      [".c", ".h"],              String[]),
    Language("python",  "Python", [".py"],                    ["py"]),
    Language("zig",     "Zig",    [".zig"],                   String[]),
    Language("julia",   "Julia",  [".jl"],                    ["jl"]),
    Language("nim",     "Nim",    [".nim"],                   String[]),
    Language("csharp",  "C#",     [".cs"],                    ["c#", "cs"]),
    Language("java",    "Java",   [".java"],                  String[]),
    Language("kotlin",  "Kotlin", [".kt", ".kts"],           ["kt"]),
    Language("swift",   "Swift",  [".swift"],                 String[]),
]

languages() = copy(_LANGUAGES)
routes() = [(a.id, b.id) for a in _LANGUAGES for b in _LANGUAGES if a.id != b.id]

function _canonical_language(id::AbstractString)
    needle = lowercase(strip(String(id)))
    for lang in _LANGUAGES
        needle == lang.id && return lang.id
        needle in lowercase.(lang.aliases) && return lang.id
    end
    throw(ArgumentError("unsupported language: $(id)"))
end

function _extension(id::AbstractString)
    lang = only(filter(l -> l.id == _canonical_language(id), _LANGUAGES))
    return first(lang.extensions)
end

"""
    backend_path() -> Union{String,Nothing}

Compatibility API for the old backend-aware package. This binary-free edition
always returns `nothing`; transpilation never invokes an external executable.
"""
function backend_path()
    return nothing
end

function backend_status()
    return (available=true, path=nothing, version="native-julia/multilang-pivot")
end

function _manual_identity(source::String, target::String, code::String)
    source == target || return nothing
    return code
end

"""
    transpile(source, target, code; backend=nothing) -> String

Transpile source text using the native Julia implementation. Identity routes are
returned unchanged. Non-identity routes use the native Julia semantic pivot: the source frontend lowers to Julia and the target emitter projects that supported subset to the requested language. Unsupported syntax raises an explicit error; no external executable is used.
"""
function transpile(source::AbstractString, target::AbstractString, code::AbstractString; backend=nothing)
    src = _canonical_language(source)
    dst = _canonical_language(target)
    text = String(code)

    identity = _manual_identity(src, dst, text)
    identity !== nothing && return identity

    backend === nothing || throw(ArgumentError("this binary-free package does not use external backends"))
    pivot = native_to_julia(src, text)
    if dst == "julia"
        return pivot
    elseif dst == "se"
        return semantic_exchange(src, text; pivot=pivot)
    end
    return native_from_julia(dst, pivot)
end

"""
    transpile_with_semantics(source, target, code)

Return generated target code together with the canonical Julia pivot and the
semantic analysis. Useful for IDEs and the bundled GUI.
"""
function transpile_with_semantics(source::AbstractString, target::AbstractString, code::AbstractString; backend=nothing)
    backend === nothing || throw(ArgumentError("this binary-free package does not use external backends"))
    src = _canonical_language(source)
    dst = _canonical_language(target)
    text = String(code)
    pivot = src == "julia" ? text : native_to_julia(src, text)
    generated = src == dst ? text : (dst == "julia" ? pivot : (dst == "se" ? semantic_exchange(src, text; pivot=pivot) : native_from_julia(dst, pivot)))
    analysis_src = src == "se" ? "julia" : src
    analysis_text = src == "se" ? pivot : text
    program = semantic_program(analysis_src, analysis_text)
    semantics = semantic_output(analysis_src, analysis_text; target=dst)
    return (code=generated, pivot=pivot, program=program, semantics=semantics)
end

"""
    transpile_file(input, output; source=nothing, target)

Transpile one file. The source language is inferred from the extension unless
`source` is supplied explicitly.
"""
function transpile_file(input::AbstractString, output::AbstractString; source=nothing, target::AbstractString)
    inpath = abspath(String(input))
    outpath = abspath(String(output))
    src = source === nothing ? _language_from_path(inpath) : _canonical_language(source)
    generated = transpile(src, target, read(inpath, String))
    mkpath(dirname(outpath))
    write(outpath, generated)
    return outpath
end

function _language_from_path(path::AbstractString)
    lower = lowercase(path)
    for lang in _LANGUAGES
        for ext in lang.extensions
            endswith(lower, lowercase(ext)) && return lang.id
        end
    end
    throw(ArgumentError("cannot infer source language from file extension: $path"))
end
