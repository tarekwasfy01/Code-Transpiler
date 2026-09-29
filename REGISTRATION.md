# Registration checklist — General / JuliaHub

This package edition is source-only and binary-free.

1. Put the package in a public repository named `CodeTranspiler.jl`.
2. Commit `Project.toml`, `LICENSE`, `README.md`, `src/`, `test/`, and docs.
3. Run the test suite on the Julia versions and operating systems you claim to
   support. It has been exercised locally with Julia 1.13.1.
4. Add CI (normally GitHub Actions) for at least the supported Julia range.
5. Keep the top-level MIT `LICENSE` file.
6. Update repository/documentation links to the final public URL.
7. Trigger Julia Registrator from the package repository, commonly with
   `@JuliaRegistrator register`.
8. Add TagBot so accepted General versions receive matching tags/releases.

JuliaHub consumes packages registered in Julia's ecosystem; a clean General
registration and usable package documentation are the main publication path.
