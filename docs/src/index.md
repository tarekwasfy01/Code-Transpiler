# CodeTranspiler.jl

`CodeTranspiler.jl` is a binary-free native Julia transpiler with frontends and target emitters for 13 languages. Non-identity translations use Julia as a semantic pivot.

```julia
using CodeTranspiler
println(transpile("python", "rust", "def add(a: int, b: int) -> int:\n    return a + b\n"))
```

Use `gui()` for the local browser interface with all language selectors.
