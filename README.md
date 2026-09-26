# Code Transpiler Python

Python package facade for the Code Transpiler / Semantic Programming Language engine.

This bootstrap release provides:

- Python API: `transpile()`, `semantic_json()`, `languages()`
- CLI: `code-transpiler`
- Tkinter GUI: `code-transpiler-gui`
- Bundled headless Code Transpiler engine for Windows x86-64 and Linux x86-64
- A self-transpile helper that attempts Go -> Python conversion file-by-file

The bundled engine is built from the repository's own headless `tools/matrix-audit` entry point. This keeps the Python package usable while the very large compiler implementation is progressively ported from Go to Python. The GUI has no Gio dependency.

## Install locally

```powershell
py -m pip install .
```

## Python API

```python
from codetranspiler import transpile

source = 'x = 2\nprint(x + 3)\n'
print(transpile("python", "go", source))
```

## CLI

```powershell
code-transpiler transpile --source python --target go input.py
code-transpiler languages
code-transpiler semantic-json --source python input.py
code-transpiler gui
```

## GUI

```powershell
code-transpiler-gui
```

The GUI uses only Python's standard-library `tkinter`.


## License and third-party notices

Code Transpiler Python is licensed under the MIT License.

Copyright (c) 2026 Tarek Wasfy

The distribution includes the project license and the notices/licenses required for bundled or derived components under `licenses/`, including Py2Many and Tree-sitter notices, plus the original `THIRD_PARTY_NOTICES` from the Go project. These files are also packaged inside the wheel so they remain available after installation.
