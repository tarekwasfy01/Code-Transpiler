use code_transpiler::*;

#[test]
fn registry_and_native_routes() {
    assert_eq!(languages().len(), 14);
    assert_eq!(
        routes(),
        vec![
            ("go", "rust"),
            ("go", "python"),
            ("go", "julia"),
            ("go", "c"),
            ("go", "cpp"),
            ("go", "se"),
            ("julia", "rust"),
            ("julia", "python"),
            ("julia", "c"),
            ("julia", "cpp"),
            ("julia", "se"),
            ("python", "rust"),
            ("python", "julia"),
            ("python", "c"),
            ("python", "cpp"),
            ("python", "se"),
            ("se", "rust"),
            ("se", "python"),
            ("se", "julia"),
            ("se", "c"),
            ("se", "cpp")
        ]
    );
    assert!(route_supported("go", "rust"));
    assert!(route_supported("python", "rust"));
    assert!(route_supported("julia", "rust"));
    assert!(route_supported("go", "se"));
    assert!(route_supported("se", "python"));
    assert_eq!(source_languages().len(), 14);
    assert!(source_languages().iter().any(|l| l.id == "python"));
    assert_eq!(
        targets_for_source("go"),
        vec!["rust", "python", "julia", "c", "cpp", "se"]
    );
    assert_eq!(
        targets_for_source("python"),
        vec!["rust", "julia", "c", "cpp", "se"]
    );
    assert!(has_frontend("py"));
    assert!(has_backend("rs"));
    assert_eq!(normalize_language(" C++ "), "cpp");
    assert_eq!(backend_status(), ("native-rust", true));
}

#[test]
fn exact_wrap() {
    let a = RExact::new(127, 8, true).unwrap();
    let b = RExact::new(1, 8, true).unwrap();
    match r_exact("integer.add", 8, true, "", &[a, b]).unwrap() {
        ExactValue::Integer(v) => assert_eq!(exact_value(v), -128),
        _ => panic!(),
    }
}

#[test]
fn native_go_ir_and_emitters() {
    let src = r#"package main
import "fmt"
func add(a int, b int) int {
    c := a + b
    return c
}
func main() {
    fmt.Println(add(2, 3))
}
"#;
    let p = parse_go(src).unwrap();
    assert_eq!(p.functions.len(), 2);
    let rust = transpile("go", "rust", src).unwrap();
    assert!(rust.contains("fn add(a: isize, b: isize) -> isize"));
    assert!(rust.contains("println!(\"{}\", add(2, 3));"));
    let py = transpile("go", "python", src).unwrap();
    assert!(py.contains("def add(a, b):"));
    let jl = transpile("go", "julia", src).unwrap();
    assert!(jl.contains("function add(a::Int, b::Int)::Int"));
    let c = transpile("go", "c", src).unwrap();
    assert!(c.contains("ptrdiff_t add(ptrdiff_t a, ptrdiff_t b)"));
}

#[test]
fn index_semantics_are_explicit() {
    let src = "package main\nfunc main() {\n for i := range xs {\n }\n}\n";
    let e = transpile("go", "rust", src).unwrap_err().to_string();
    assert!(e.contains("zero-based"));
}

#[test]
fn runtime_subset() {
    let v = r_call(
        "arithmetic",
        "__binary_+",
        vec![RValue::Num(2.0), RValue::Num(3.0)],
    )
    .unwrap();
    assert_eq!(v, RValue::Num(5.0));
    let v = r_call(
        "subset",
        "[",
        vec![RValue::Vec(vec![RValue::Num(7.0)]), RValue::Num(1.0)],
    )
    .unwrap();
    assert_eq!(v, RValue::Num(7.0));
}

#[test]
fn target_helpers() {
    assert_eq!(target_name("rust", "fn"), "__uast_fn");
    assert_eq!(target_inf("rust"), "RValue::Num(f64::INFINITY)");
    assert_eq!(
        emit_dispatch("rust", "__binary_+", &["x", "y"]).unwrap(),
        "r_call(\"arithmetic\", \"__binary_+\", vec![x, y])"
    );
}

#[test]
fn scalar_zero_values_and_python_operators() {
    let src =
        "package main\nfunc main() {\n var x int\n var ok bool\n if ok || x == 0 {\n  x++\n }\n}\n";
    let py = transpile("go", "python", src).unwrap();
    assert!(py.contains("x = 0"));
    assert!(py.contains("ok = False"));
    assert!(py.contains("if ok or x == 0:"));
}

#[test]
fn signature_binding_port() {
    let params = vec![
        SignatureParameter {
            name: "x".into(),
            passing: "positional_or_keyword".into(),
            has_default: false,
        },
        SignatureParameter {
            name: "y".into(),
            passing: "positional_or_keyword".into(),
            has_default: true,
        },
        SignatureParameter {
            name: "rest".into(),
            passing: "variadic_positional".into(),
            has_default: false,
        },
    ];
    let args = vec![
        SignatureArgument::default(),
        SignatureArgument::default(),
        SignatureArgument::default(),
    ];
    let b = bind_signature(&params, &args).unwrap();
    assert_eq!(b.argument_counts, vec![1, 1, 1]);
    assert_eq!(b.use_defaults, vec![0, 0, 0]);
}

#[test]
fn call_resolution_port() {
    let r = CallResolution {
        candidates: vec![
            CallCandidate {
                name: "exact".into(),
                declaration: "a".into(),
            },
            CallCandidate {
                name: "convert".into(),
                declaration: "b".into(),
            },
        ],
        obligations: vec!["callable".into()],
        selected: Some(0),
        required: Matrix::from_vec(2, 1, vec![1.0, 1.0]).unwrap(),
        satisfied: Matrix::from_vec(2, 1, vec![1.0, 1.0]).unwrap(),
        conversion_cost: Matrix::from_vec(2, 1, vec![0.0, 1.0]).unwrap(),
        priority: vec![0.0, 0.0],
    };
    assert_eq!(validate_call_resolution(&r, 1).unwrap(), 0);
}

#[test]
fn semantic_helpers_port() {
    let e = summarize_effect_axes(&[
        ("local.read".into(), vec![true, false]),
        ("call.unknown".into(), vec![true]),
    ]);
    assert!(e.unknown);
    assert!(!e.conservative_pure);
    assert_eq!(e.counts["local.read"], 1);
    assert_eq!(
        snapshot_iteration(&RValue::Num(3.0)),
        vec![RValue::Num(3.0)]
    );
    let input = StructuredConstructInput {
        family: "INDEX".into(),
        node_kind: "index".into(),
        roles: vec!["base".into(), "index".into()],
        ..Default::default()
    };
    let f = structured_input_fields(&input);
    assert_eq!(f.get("operand"), Some(&true));
    assert_eq!(f.get("index"), Some(&true));
}

#[test]
fn classic_for_and_slice_port() {
    let src = r#"package main
import "fmt"
func sum(xs []int) int {
    total := 0
    for i := 0; i < len(xs); i++ {
        total += xs[i]
    }
    return total
}
func main() {
    xs := []int{1, 2, 3}
    xs = append(xs, 4)
    fmt.Println(sum(xs))
}
"#;
    let rust = transpile("go", "rust", src).unwrap();
    assert!(rust.contains("let mut i = 0;"));
    assert!(rust.contains("while i < (xs.len() as isize)"));
    assert!(rust.contains("let mut xs: Vec<isize> = vec![1, 2, 3]"));
    assert!(rust.contains("__v.push(4)"));
    let py = transpile("go", "python", src).unwrap();
    assert!(py.contains("while i < len(xs):"));
    assert!(py.contains("xs = [1, 2, 3]"));
    assert!(py.contains("xs = xs + [4]"));
}

#[test]
fn native_abi_port() {
    let p = native_windows_x64_target_profile();
    let int = solve_native_layout(&p, &Type::Int, NativeAbiContext::Argument).unwrap();
    assert_eq!(int.size_bits, 64);
    let slice = solve_native_layout(
        &p,
        &Type::Slice(Box::new(Type::Int32)),
        NativeAbiContext::Value,
    )
    .unwrap();
    assert_eq!(slice.representation, NativeRepresentation::Descriptor);
    assert_eq!(slice.field_offsets, vec![0, 8]);
    let storage = decide_native_storage(&NativeStorageRequest {
        escapes: false,
        program_static: false,
        ownership_known: false,
        alias_mutation_safe: true,
        dynamic_size: false,
        lifetime: "frame".into(),
    });
    assert_eq!(storage, NativeStorageDecision::Stack);
    assert_eq!(NativeValue::boolean(true).payload, 1);
}

#[test]
fn exact_signature_contract_port() {
    let c = FunctionSignatureContract {
        binding: "exact_v1".into(),
        default_evaluation: "definition".into(),
        parameters: vec![SignatureParameter {
            name: "x".into(),
            passing: "positional_or_keyword".into(),
            has_default: false,
        }],
    };
    assert!(validate_signature_contract(&c, "eager_left_to_right").unwrap());
    assert!(validate_signature_contract(&c, "lazy").is_err());
}

#[test]
fn structured_missing_fields_port() {
    let input = StructuredConstructInput {
        family: "INDEX_SLICE".into(),
        node_kind: "index".into(),
        roles: vec!["base".into(), "index".into()],
        ..Default::default()
    };
    let missing = missing_structured_fields(&input);
    assert!(missing.contains(&"slice bounds"));
    assert!(missing.contains(&"binding"));
}

#[test]
fn classic_for_continue_fails_closed() {
    let src = "package main\nfunc main() {\n for i := 0; i < 3; i++ {\n  continue\n }\n}\n";
    let e = transpile("go", "rust", src).unwrap_err().to_string();
    assert!(e.contains("post-on-continue"));
}

#[test]
fn type_conversion_port() {
    let r = resolve_universal_type_conversion(&Type::Int32, &Type::Int32, "rust").unwrap();
    assert_eq!(r.id, "type.identity");
    let r = resolve_universal_type_conversion(
        &Type::Slice(Box::new(Type::UInt8)),
        &Type::Slice(Box::new(Type::UInt8)),
        "python",
    )
    .unwrap();
    assert_eq!(r.status, TypeConversionStatus::Exact);
    assert!(resolve_universal_type_conversion(&Type::Int32, &Type::UInt32, "rust").is_err());
}

#[test]
fn capability_matrix_port() {
    let m = semantic_capability_matrix(&[]);
    assert_eq!(m.targets.len(), 14);
    let rejected = m.rejected_targets(&["core".into()]).unwrap();
    assert_eq!(rejected.rows, 1);
    assert_eq!(rejected.cols, 14);
}

#[test]
fn multiple_returns_are_native_for_rust_python_julia() {
    let src = r#"package main
import "fmt"
func pair(a int) (int, int) {
    return a, a + 1
}
func main() {
    x, y := pair(4)
    fmt.Println(x + y)
}
"#;
    let rust = transpile("go", "rust", src).unwrap();
    assert!(rust.contains("fn pair(a: isize) -> (isize, isize)"));
    assert!(rust.contains("let (x, y) = pair(4);"));
    assert!(rust.contains("return (a, a + 1);"));
    let py = transpile("go", "python", src).unwrap();
    assert!(py.contains("x, y = pair(4)"));
    let jl = transpile("go", "julia", src).unwrap();
    assert!(jl.contains("function pair(a::Int)::Tuple{Int, Int}"));
    assert!(jl.contains("x, y = pair(4)"));
    assert!(transpile("go", "c", src)
        .unwrap_err()
        .to_string()
        .contains("multiple-return"));
}

#[test]
fn structs_and_keyed_composite_literals_are_native() {
    let src = r#"package main
import "fmt"
type Point struct {
    X int
    Y int
}
func main() {
    p := Point{X: 2, Y: 3}
    p.X += 4
    fmt.Println(p.X + p.Y)
}
"#;
    let p = parse_go(src).unwrap();
    assert_eq!(p.structs.len(), 1);
    assert_eq!(p.structs[0].fields.len(), 2);
    let rust = transpile("go", "rust", src).unwrap();
    assert!(rust.contains("struct Point"));
    assert!(rust.contains("Point { X: 2, Y: 3 }"));
    let py = transpile("go", "python", src).unwrap();
    assert!(py.contains("class Point:"));
    assert!(py.contains("Point(X=2, Y=3)"));
    let jl = transpile("go", "julia", src).unwrap();
    assert!(jl.contains("mutable struct Point"));
    assert!(jl.contains("Point(; X=2, Y=3)"));
}

#[test]
fn switch_is_lowered_with_single_evaluation() {
    let src = r#"package main
import "fmt"
func classify(x int) int {
    switch x {
    case 1, 2:
        return 10
    case 3:
        return 20
    default:
        return 30
    }
}
func main() {
    fmt.Println(classify(3))
}
"#;
    let rust = transpile("go", "rust", src).unwrap();
    assert!(rust.contains("let __code_transpiler_switch_value = x;"));
    assert!(
        rust.contains("__code_transpiler_switch_value == 1 || __code_transpiler_switch_value == 2")
    );
    let py = transpile("go", "python", src).unwrap();
    assert!(py.contains("__code_transpiler_switch_value = x"));
    assert!(py.contains(
        "if __code_transpiler_switch_value == 1 or __code_transpiler_switch_value == 2:"
    ));
    let jl = transpile("go", "julia", src).unwrap();
    assert!(jl.contains("__code_transpiler_switch_value = x"));
    assert!(jl
        .contains("if __code_transpiler_switch_value == 1 || __code_transpiler_switch_value == 2"));
}

#[test]
fn switch_break_and_fallthrough_fail_closed() {
    let with_break = "package main\nfunc main() {\n switch 1 {\n case 1:\n  break\n }\n}\n";
    assert!(transpile("go", "rust", with_break)
        .unwrap_err()
        .to_string()
        .contains("break inside switch"));
    let with_fallthrough =
        "package main\nfunc main() {\n switch 1 {\n case 1:\n  fallthrough\n case 2:\n }\n}\n";
    assert!(transpile("go", "rust", with_fallthrough)
        .unwrap_err()
        .to_string()
        .contains("fallthrough"));
}

#[test]
fn maps_preserve_missing_key_zero_value_for_native_targets() {
    let src = r#"package main
import "fmt"
func main() {
    m := map[string]int{"a": 2}
    fmt.Println(m["a"] + m["missing"])
}
"#;
    let rust = transpile("go", "rust", src).unwrap();
    assert!(rust.contains("BTreeMap::from"));
    assert!(rust.contains(".get(&\"missing\".to_string()).cloned().unwrap_or_else(|| 0)"));
    let py = transpile("go", "python", src).unwrap();
    assert!(py.contains("{\"a\": 2}"));
    assert!(py.contains("m.get(\"missing\", 0)") || py.contains("m.get('missing', 0)"));
    let jl = transpile("go", "julia", src).unwrap();
    assert!(jl.contains("Dict(\"a\" => 2)"));
    assert!(jl.contains("get(m, \"missing\", zero(Int))"));
}

#[test]
fn indexed_mutation_is_native_for_slices_and_maps() {
    let src = r#"package main
import "fmt"
func main() {
    xs := []int{1, 2}
    xs[0] = 4
    xs[1] += 3
    m := map[string]int{}
    m["x"] += 2
    m["y"] = 7
    fmt.Println(xs[0] + xs[1] + m["x"] + m["y"])
}
"#;
    let rust = transpile("go", "rust", src).unwrap();
    assert!(rust.contains("xs[(0) as usize] = 4;"));
    assert!(rust.contains("xs[(1) as usize] += 3;"));
    assert!(rust.contains(r#".entry("x".to_string()).or_insert_with(|| 0) += 2;"#));
    assert!(rust.contains(r#".insert("y".to_string(), 7);"#));

    let py = transpile("go", "python", src).unwrap();
    assert!(py.contains(r#"m["x"] = m.get("x", 0) + 2"#));
    assert!(py.contains(r#"m["y"] = 7"#));

    let jl = transpile("go", "julia", src).unwrap();
    assert!(jl.contains("xs[(0) + 1] = 4"));
    assert!(jl.contains(r#"m["x"] = get(m, "x", zero(Int)) + 2"#));

    let c_src = "package main\nfunc main() {\n xs := []int{1}\n xs[0] = 2\n}\n";
    assert!(transpile("go", "c", c_src).is_err());
}

#[test]
fn range_values_preserve_rust_ownership_and_map_value_semantics() {
    let slice_src = r#"package main
import "fmt"
func main() {
    xs := []int{1, 2}
    for _, v := range xs {
        fmt.Println(v)
    }
    fmt.Println(len(xs))
}
"#;
    let rust = transpile("go", "rust", slice_src).unwrap();
    assert!(rust.contains("for v in xs.iter().cloned()"));
    assert!(rust.contains("xs.len()"));

    let map_src = r#"package main
import "fmt"
func main() {
    m := map[string]int{"a": 7}
    for _, v := range m {
        fmt.Println(v)
    }
}
"#;
    let rust = transpile("go", "rust", map_src).unwrap();
    assert!(rust.contains("for v in m.values().cloned()"));
    let py = transpile("go", "python", map_src).unwrap();
    assert!(py.contains("for v in m.values():"));
    let jl = transpile("go", "julia", map_src).unwrap();
    assert!(jl.contains("for v in values(m)"));
}

#[test]
fn map_comma_ok_is_native_and_evaluates_key_once() {
    let src = r#"package main
import "fmt"
func main() {
    m := map[string]int{"a": 5}
    v, ok := m["a"]
    missing, found := m["missing"]
    fmt.Println(v, ok, missing, found)
}
"#;
    let rust = transpile("go", "rust", src).unwrap();
    assert!(rust.contains("let (v, ok) = { let __code_transpiler_key"));
    assert!(rust.contains("Some(__value) => (__value.clone(), true)"));
    assert!(rust.contains("None => (0, false)"));

    let py = transpile("go", "python", src).unwrap();
    assert!(py.contains("lambda __code_transpiler_key"));
    assert!(py.contains("__code_transpiler_key in m"));

    let jl = transpile("go", "julia", src).unwrap();
    assert!(jl.contains("haskey(m, __code_transpiler_key)"));
}

#[test]
fn semantic_exchange_is_a_real_target_and_source() {
    let src = r#"package main
import "fmt"
func add(a int, b int) int {
    return a + b
}
func main() {
    fmt.Println(add(2, 3))
}
"#;
    let se = transpile("go", "se", src).unwrap();
    assert!(se.starts_with("SE/1\nsource: go\ncanonical: julia\n"));
    assert!(se.contains("--- semantics-json ---"));
    assert!(se.contains("\"operation\":\"numeric.add\""));
    assert!(se.contains("--- julia-pivot ---"));
    let rust = transpile("se", "rust", &se).unwrap();
    assert!(rust.contains("fn add(a: isize, b: isize) -> isize"));
    assert!(rust.contains("println!(\"{}\", add(2, 3));"));
}

#[test]
fn julia_is_a_native_frontend() {
    let src = r#"function add(a::Int, b::Int)::Int
    c = a + b
    return c
end

function main()
    println(add(4, 5))
end

main()
"#;
    let rust = transpile("julia", "rust", src).unwrap();
    assert!(rust.contains("fn add(a: isize, b: isize) -> isize"));
    assert!(rust.contains("println!(\"{}\", add(4, 5));"));
    let py = transpile("julia", "python", src).unwrap();
    assert!(py.contains("def add(a, b):"));
    let se = transpile("julia", "se", src).unwrap();
    assert!(se.starts_with("SE/1\nsource: julia"));
}

#[test]
fn julia_one_based_index_is_normalized() {
    let src = r#"function firstplus(xs::Vector{Int})::Int
    return xs[1] + xs[2]
end
"#;
    let rust = transpile("julia", "rust", src).unwrap();
    assert!(rust.contains("xs[(1 - 1) as usize]") || rust.contains("xs[((1 - 1)) as usize]"));
}

#[test]
fn typed_python_is_a_native_frontend() {
    let src = r#"def add(a: int, b: int) -> int:
    c = a + b
    return c

def main() -> None:
    xs = [1, 2, 3]
    i = 0
    total = 0
    while i < len(xs):
        total += xs[i]
        i += 1
    print(add(total, 4))
"#;
    let pivot = python_to_julia_pivot(src).unwrap();
    assert!(pivot.contains("function add(a::Int, b::Int)::Int"));
    assert!(pivot.contains("xs[(i) + 1]"));
    let rust = transpile("python", "rust", src).unwrap();
    assert!(rust.contains("fn add(a: isize, b: isize) -> isize"));
    assert!(rust.contains("while i < (xs.len() as isize)"));
    let jl = transpile("python", "julia", src).unwrap();
    assert!(jl.contains("function add(a::Int, b::Int)::Int"));
    let se = transpile("python", "se", src).unwrap();
    assert!(se.starts_with("SE/1\nsource: python"));
}
