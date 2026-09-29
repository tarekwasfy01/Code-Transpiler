using Test
using CodeTranspiler

@testset "CodeTranspiler registry" begin
    @test length(languages()) == 14
    @test length(routes()) == 182
    @test length(frontends()) == 14
    @test length(backends()) == 14
    @test normalize_language(" PY ") == "python"
    @test has_frontend("c++")
    @test has_backend("jl")
    @test backend_capability("core", "go").status == CapabilityLowering
    @test cpp_ident("class") == "r_class"
    @test cpp_ident("a.b\$c@d") == "a_b_c_d"
    @test target_reserved_word("go", "func")
    @test target_name("go", "func") == "__uast_func"
    @test target_na("julia") == "NaN"
    @test target_inf("rust") == "RValue::Num(f64::INFINITY)"
    @test emit_dispatch("julia", "__binary_+", ["x", "y"]) == "r_call(\"arithmetic\", \"__binary_+\", Any[x, y])"
    @test solve(default_preservation_registry(), "go", "uast.core") !== nothing
    @test exact_value(r_exact("integer.add", 8, true, "", Any[RExact(127, 8, true), RExact(1, 8, true)])) == -128
    @test r_exact("integer.less", 8, true, "", Any[RExact(-1, 8, true), RExact(1, 8, true)]) === true
end

@testset "Native Go -> Julia subset" begin
    src = """package main
import "fmt"

func add(a int, b int) int {
    c := a + b
    return c
}

func main() {
    fmt.Println(add(2, 3))
}
"""
    jl = native_go_to_julia(src)
    @test occursin("function add(a::Int, b::Int)::Int", jl)
    @test occursin("c = a + b", jl)
    @test occursin("println(add(2, 3))", jl)
    @test transpile("go", "julia", src) == jl
    @test occursin("struct X end", native_go_to_julia("package main\ntype X struct {}\n"))
end

@testset "Fallback and identity" begin
    sample = "println(\"hello\")\n"
    @test transpile("julia", "julia", sample) == sample
    @test backend_status().available isa Bool
end


@testset "Native Go containers, structs and loops" begin
    src = """package main
import "fmt"

type Point struct {
    X int
    Y int
}

func sum(xs []int) int {
    total := 0
    for i, v := range xs {
        total += i + v
    }
    return total
}

func first(xs []int) int {
    return xs[0]
}

func makevals() []int {
    xs := []int{10, 20, 30}
    xs = append(xs, 40)
    return xs
}

func zeros(n int) []int {
    xs := make([]int, n)
    return xs
}

func main() {
    p := Point{2, 3}
    fmt.Println(p.X + p.Y)
    fmt.Println(sum([]int{10, 20, 30}))
}
"""
    jl = native_go_to_julia(src)
    @test occursin("mutable struct Point", jl)
    @test occursin("Point() = Point(zero(Int), zero(Int))", jl)
    @test occursin("i = __go_idx - 1", jl)
    @test occursin("xs[(0) + 1]", jl)
    @test occursin("push!(xs, 40)", jl)
    @test occursin("fill(zero(Int), n)", jl)
end

@testset "Binary-free behavior" begin
    @test backend_path() === nothing
    @test backend_status() == (available=true, path=nothing, version="native-julia/multilang-pivot")
    @test occursin("print(1)", transpile("python", "julia", "print(1)"))
    @test transpile("go", "julia", "package main\nfunc one() int {\n return 1\n}\n") |> x -> occursin("return 1", x)
end

@testset "Native Go execution semantics" begin
    src = """package main

type Point struct {
    X int
}

func (p Point) Get() int {
    return p.X
}

func add3(a, b int, c int) int {
    return a + b + c
}

func divi(a int, b int) int {
    return a / b
}

func concat(a string, b string) string {
    return a + b
}

func usemap() int {
    m := map[string]int{"a": 4, "b": 5}
    return m["a"] + m["b"]
}

func usearray() int {
    a := [3]int{7, 8}
    return a[2]
}

func usemethod() int {
    p := Point{9}
    return p.Get()
}
"""
    jl = transpile("go", "julia", src)
    m = Module(:NativeGoExecutionTest)
    Core.eval(m, :(using Base))
    Base.include_string(m, jl)
    @test Core.eval(m, :(add3(1, 2, 3))) == 6
    @test Core.eval(m, :(divi(7, 2))) == 3
    @test Core.eval(m, :(concat("a", "b"))) == "ab"
    @test Core.eval(m, :(usemap())) == 9
    @test Core.eval(m, :(usearray())) == 0
    @test Core.eval(m, :(usemethod())) == 9

    one = transpile("go", "julia", "package main\nimport \"fmt\"\nfunc main() { fmt.Println(\"hello\") }\n")
    @test occursin("println(\"hello\")", one)
end


@testset "All native language frontends -> Julia" begin
    samples = Dict(
        "r" => "add <- function(a, b) {\n return(a + b)\n}\n",
        "go" => "package main\nfunc add(a int, b int) int {\n return a + b\n}\n",
        "python" => "def add(a: int, b: int) -> int:\n    return a + b\n",
        "rust" => "fn add(a: i64, b: i64) -> i64 {\n return a + b;\n}\n",
        "c" => "long long add(long long a, long long b) {\n return a + b;\n}\n",
        "cpp" => "long long add(long long a, long long b) {\n return a + b;\n}\n",
        "zig" => "fn add(a: i64, b: i64) i64 {\n return a + b;\n}\n",
        "nim" => "proc add(a: int, b: int): int =\n  return a + b\n",
        "csharp" => "class X {\n static long add(long a, long b) {\n return a + b;\n }\n}\n",
        "java" => "class X {\n static long add(long a, long b) {\n return a + b;\n }\n}\n",
        "kotlin" => "fun add(a: Long, b: Long): Long {\n return a + b\n}\n",
        "swift" => "func add(_ a: Int, _ b: Int) -> Int {\n return a + b\n}\n",
        "julia" => "function add(a::Int,b::Int)::Int\n return a+b\nend\n",
    )
    for lang in keys(samples)
        jl = transpile(lang, "julia", samples[lang])
        m = Module(Symbol("Frontend_", lang))
        Core.eval(m, :(using Base))
        Base.include_string(m, jl)
        @test Core.eval(m, :(add(2,3))) == 5
    end
end

@testset "Julia -> all native targets" begin
    jl = "function add(a::Int64, b::Int64)::Int64\n    c = a + b\n    return c\nend\n"
    for lang in (l.id for l in languages())
        generated = transpile("julia", lang, jl)
        @test !isempty(strip(generated))
        lang != "julia" && @test occursin("add", generated)
    end
    @test occursin("package main", transpile("julia","go",jl))
    @test occursin("long long add", transpile("julia","c",jl))
    @test occursin("def add", transpile("julia","python",jl))
    @test occursin("public final class Transpiled", transpile("julia","java",jl))
end

@testset "Cross-language pivot routes" begin
    py = "def add(a: int, b: int) -> int:\n    return a + b\n"
    @test occursin("fn add", transpile("python","rust",py))
    @test occursin("func add", transpile("python","go",py))
    @test occursin("long long add", transpile("python","c",py))
    @test native_route_supported("csharp","swift")
    @test all(native_route_supported(a.id,b.id) for a in languages() for b in languages())
end

@testset "C arrays and zero-based indexing" begin
    csrc = """int first() {
 int xs[] = {10, 20, 30};
 return xs[0];
}
"""
    jl = transpile("c","julia",csrc)
    @test occursin("xs[(0) + 1]", jl)
    m=Module(:CArrayTest); Core.eval(m, :(using Base)); Base.include_string(m,jl)
    @test Core.eval(m, :(first())) == 10
end


@testset "C counted loop semantics" begin
    csrc = """int sum3() {
 int xs[] = {10, 20, 30};
 int total = 0;
 for (int i = 0; i < 3; i++) {
   total += xs[i];
 }
 return total;
}
"""
    jl=transpile("c","julia",csrc)
    m=Module(:CLoopTest); Core.eval(m, :(using Base)); Base.include_string(m,jl)
    @test Core.eval(m, :(sum3())) == 60
end

@testset "GUI exposes all native languages" begin
    page = CodeTranspiler._gui_page()
    @test all(occursin("value=\"$(l.id)\"", page) for l in languages())
    @test length(collect(eachmatch(r"value=\"se\"", page))) == 2
    @test occursin("14 Sprachen", page)
end

@testset "Semantic output API" begin
    py = "def add(a: int, b: int) -> int:\n    c = a + b\n    print(c)\n    return c\n"
    p = semantic_program("python", py)
    @test p.source_language == "python"
    @test p.index_base == 1
    @test length(p.functions) == 1
    @test p.functions[1].name == "add"
    @test any(op -> op.op == "integer.add", p.functions[1].operations)
    @test "io.stdout" in p.functions[1].effects
    text = semantic_output("python", py; target="c")
    @test occursin("SemanticProgram", text)
    @test occursin("integer.add", text)
    @test occursin("Python integers may exceed", text)
    json = semantic_output("python", py; format=:json)
    @test startswith(json, "{")
    @test occursin("\"source_language\":\"python\"", json)
    result = transpile_with_semantics("python", "go", py)
    @test occursin("package main", result.code)
    @test result.program.source_language == "python"
    @test occursin("target: go", result.semantics)
end

@testset "GUI semantic pane" begin
    page = CodeTranspiler._gui_page(semantics="SemanticProgram\n  source: go")
    @test occursin("Semantik", page)
    @test occursin("SemanticProgram", page)
end

@testset "Python indentation and operator semantics" begin
    py = """def classify(x: int) -> int:
    if x > 3:
        return 10
    elif x == 3:
        return 5
    else:
        return 1

def sum3() -> int:
    xs = [10, 20, 30]
    total = 0
    for i in range(3):
        total = total + xs[i]
    return total

def ops() -> int:
    return 7 // 2 + 2 ** 3
"""
    jl=transpile("python","julia",py)
    m=Module(:PythonSemanticRegression); Core.eval(m, :(using Base)); Base.include_string(m,jl)
    @test Core.eval(m, :(classify(4))) == 10
    @test Core.eval(m, :(classify(3))) == 5
    @test Core.eval(m, :(classify(0))) == 1
    @test Core.eval(m, :(sum3())) == 60
    @test Core.eval(m, :(ops())) == 11
    @test occursin("xs[(i) + 1]",jl)
end

@testset "Target collection semantic projection" begin
    jl = "function f()::Int64\n xs = [10,20]\n push!(xs, 30)\n return length(xs) + xs[3]\nend\n"
    cpp = transpile("julia","cpp",jl)
    @test occursin("std::vector<long long>", cpp)
    @test occursin("push_back(30)", cpp)
    @test occursin("((long long)xs.size())", cpp)
    go = transpile("julia","go",jl)
    @test occursin("append(xs, 30)", go)
    @test occursin("int64(len(xs))", go)
    java = transpile("julia","java",jl)
    @test occursin("ArrayList<Long>", java)
    @test occursin("xs.add(30L)", java)
    @test occursin("xs.get", java)
    kotlin = transpile("julia","kotlin",jl)
    @test occursin("MutableList<Long>", kotlin)
    @test occursin("xs.add(30L)", kotlin)
    swift = transpile("julia","swift",jl)
    @test occursin("Int64(xs.count)", swift)
    @test occursin("xs.append(30)", swift)
    @test_throws NativeTranspileError transpile("julia","c",jl)
    @test_throws NativeTranspileError transpile("julia","zig",jl)
end

@testset "Index projection is not double shifted" begin
    jl = "function f()::Int64\n xs = [10,20,30]\n i = 0\n return xs[(i) + 1]\nend\n"
    @test occursin("return xs[i]", transpile("julia","c",jl))
    @test occursin("return xs[i]", transpile("julia","go",jl))
    @test occursin("return xs[i]", transpile("julia","python",jl))
    @test !occursin("i) - 1", transpile("julia","go",jl))
    @test occursin("as usize", transpile("julia","rust",jl))
    @test occursin("toInt()", transpile("julia","kotlin",jl))
    @test occursin("Int(i)", transpile("julia","swift",jl))
end

@testset "Semantic index bases and collection effects" begin
    py = "def f() -> int:\n    xs = [1, 2]\n    xs.append(3)\n    print(len(xs))\n    return xs[0]\n"
    p = semantic_program("python", py)
    @test p.source_index_base == 0
    @test p.index_base == 1
    out = semantic_output("python", py; target="julia")
    @test occursin("source_index_base: 0", out)
    @test occursin("canonical_index_base: 1", out)
    json = semantic_output("python", py; format=:json)
    @test occursin("\"source_index_base\":0", json)
end


@testset "SE is a real input/output language" begin
    py = "def add(a: int, b: int) -> int:\n    return a + b\n"
    se = transpile("python", "se", py)
    @test startswith(se, "SE/1\n")
    @test occursin("--- semantics-json ---", se)
    @test occursin("--- julia-pivot ---", se)
    @test occursin("\"source_language\":\"python\"", se)
    @test occursin("function add", se)
    jl = transpile("se", "julia", se)
    @test occursin("function add", jl)
    m=Module(:SERoundTrip); Core.eval(m, :(using Base)); Base.include_string(m,jl)
    @test Core.eval(m, :(add(2,3))) == 5
    go = transpile("se", "go", se)
    @test occursin("func add", go)
    se2 = transpile("julia", "se", "function f(x::Int)::Int\n return x + 1\nend\n")
    @test occursin("source: julia", se2)
    @test normalize_language("semantic") == "se"
    @test native_route_supported("se", "rust")
    @test native_route_supported("python", "se")
    @test_throws NativeTranspileError transpile("se", "julia", "not se")
end
