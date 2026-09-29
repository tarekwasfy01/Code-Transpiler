# Native Rust port status — v7

This crate is a pure-Rust incremental port. Self-transpiled material is treated
as reference input only; code admitted to `src/` is manually repaired and tested.
The Julia-native package is now also used as a porting reference because it
contains repaired semantic-pivot logic that is easier to transfer safely than
some of the original Go backend.

## Native source frontends

### Go
Native subset includes functions, scalar types, slices, maps, structs, multiple
returns, assignments/mutation, indexed mutation, map comma-ok, if/while,
classic for loops under documented constraints, range-value iteration, switch,
printing, and exact index-base projection.

### Julia
Native subset includes typed functions, scalar expressions, arrays, indexing,
assignments/mutation, if/elseif/else, while, foreach over collections, returns,
printing, and mutable structs emitted by the neutral IR. Julia's one-based index
semantics are normalized into the IR before target emission.

### Python
Native typed subset includes annotated functions, scalar expressions, lists,
zero-based indexing, assignment/mutation, if/elif/else, while, returns and
printing. Python `for/range` is still rejected until its iteration/index contract
is fully ported.

## Semantic Exchange

`se` / `semantic` / `semantic-exchange` is a real selectable language.

`SE/1` documents contain:

1. deterministic semantic JSON,
2. a canonical Julia pivot,
3. an explicit end marker.

Current native paths include `Go -> SE`, `Julia -> SE`, `Python -> SE`, and
`SE -> Rust/Python/Julia/C/C++`.

## Current route groups

- Go -> Rust, Python, Julia, C, C++, SE
- Julia -> Rust, Python, C, C++, SE
- Python -> Rust, Julia, C, C++, SE
- SE -> Rust, Python, Julia, C, C++

Identity routes are also accepted.

## Next chunks

1. Python `for/range` and iteration normalization
2. Julia range loops
3. Rust source frontend using the same neutral IR
4. closures/function values
5. methods/receivers/interfaces
6. additional brace-language frontends from the repaired Julia pivot code
