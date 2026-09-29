# crates.io checklist

Completed locally:

- [x] `cargo fmt --all --check`
- [x] `cargo test --all-targets` — 24/24 integration tests pass
- [x] `cargo clippy --all-targets --all-features -- -D warnings`
- [x] `cargo package --allow-dirty` — isolated package verification build passes
- [x] no Go fallback binary/DLL/subprocess path
- [x] MIT license and README included

Before the real publish:

- [ ] put the source in the final public repository
- [ ] add the final `repository` / `homepage` metadata if desired
- [ ] rerun CI on the public repository
- [ ] `cargo publish --dry-run` with crates.io network access
- [ ] `cargo publish` with the authorized crates.io account/token

This execution environment cannot currently resolve/contact `index.crates.io`,
so the online dry-run/publish step must happen where crates.io is reachable.
