# Semantic Programming (`.sp`)

`.sp` is the readable, versioned transport form of the existing
`SemanticProgram`/`UniversalASTDocument`. It does not add a persistent IR.

The canonical envelope is:

```text
sp 1
program {
    schema = 1
    source_language = "go"
    field.evaluation = "eager_left_to_right"
    field.universal_ast = { ... }
}
```

Each canonical top-level semantic field is emitted as a deterministic
`field.<name>` entry. Values preserve the complete schema, including unknown
versioned extensions, while the envelope remains readable and diffable.
The legacy `semantic_json_base64` field is accepted only when importing older
files and is never emitted by the current writer.

CLI:

* `semantic-export -source go input.go -format sp -o program.sp`
* `semantic-transpile -target rust program.sp -o output.rs`
* `semantic-convert program.json -o program.sp`
* `semantic-format program.sp -o formatted.sp`
* `semantic-validate program.sp`
* `semantic-info program.sp`

The optional `.spz` form is an independent `SPZ1` lossless block-compressed
`.sp` document (no external compression dependency). Use
`semantic-export -format spz`, `semantic-convert`, or `semantic-transpile`
with a `.spz` input; decompression always returns to the same canonical SP
parser.

Malformed versions, duplicate/unknown fields, invalid strings and truncated
payloads are rejected before any target emission.
