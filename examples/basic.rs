fn main() -> Result<(), Box<dyn std::error::Error>> {
    let go = r#"package main
func add(a int, b int) int {
    return a + b
}
"#;
    println!("{}", code_transpiler::transpile("go", "rust", go)?);
    Ok(())
}
