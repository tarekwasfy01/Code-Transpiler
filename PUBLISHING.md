# Publishing

The crate is pure Rust and packages successfully as `code-transpiler 0.1.0`.

Verified locally:

```bash
cargo fmt --all --check
cargo test --all-targets
cargo clippy --all-targets --all-features -- -D warnings
cargo package --allow-dirty
```

`cargo package` performed Cargo's isolated verification build successfully.

For the final crates.io release, from the public repository run:

```bash
cargo publish --dry-run
cargo publish
```

The online publish step requires crates.io connectivity and an authorized
account/token. Published crate versions are immutable, so inspect
`cargo package --list` and the generated `.crate` before publishing.
