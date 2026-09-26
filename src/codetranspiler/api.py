from __future__ import annotations

import json
from typing import Any, Dict, List

from .engine import CodeTranspilerError, request


_LANGUAGES = [
    {"id": "r", "name": "R", "extensions": [".R", ".r"]},
    {"id": "go", "name": "Go", "extensions": [".go"]},
    {"id": "rust", "name": "Rust", "extensions": [".rs"]},
    {"id": "cpp", "name": "C++", "extensions": [".cpp", ".cc", ".cxx", ".hpp"]},
    {"id": "c", "name": "C", "extensions": [".c", ".h"]},
    {"id": "python", "name": "Python", "extensions": [".py"]},
    {"id": "zig", "name": "Zig", "extensions": [".zig"]},
    {"id": "julia", "name": "Julia", "extensions": [".jl"]},
    {"id": "nim", "name": "Nim", "extensions": [".nim"]},
    {"id": "csharp", "name": "C#", "extensions": [".cs"]},
    {"id": "java", "name": "Java", "extensions": [".java"]},
    {"id": "kotlin", "name": "Kotlin", "extensions": [".kt"]},
    {"id": "swift", "name": "Swift", "extensions": [".swift"]},
    {"id": "se", "name": "Semantic", "extensions": [".se", ".sp"]},
]


def languages() -> List[Dict[str, Any]]:
    return [dict(x) for x in _LANGUAGES]


def transpile(source: str, target: str, code: str, *, timeout: int = 120) -> str:
    source = source.lower().strip()
    target = target.lower().strip()
    if source == target:
        return code
    if target in {"se", "semantic", "sp"}:
        if source in {"se", "semantic", "sp"}:
            return code
        return str(request(source, code=code, mode="semantic-sp", timeout=timeout).get("code", ""))
    if source in {"se", "semantic", "sp"}:
        return str(request("se", target, code, mode="from-semantic-sp", timeout=timeout).get("code", ""))
    return str(request(source, target, code, timeout=timeout).get("code", ""))


def semantic_json(source: str, code: str, *, timeout: int = 120) -> Dict[str, Any]:
    if source.lower().strip() in {"se", "semantic", "sp"}:
        # Convert readable Semantic source through a neutral backend path first.
        # JSON import/export remains available for ordinary source languages.
        raise CodeTranspilerError("semantic_json() currently expects a non-SE source; use transpile(source, 'se', code) for readable Semantic source")
    text = str(request(source, code=code, mode="semantic-document", timeout=timeout).get("code", ""))
    return json.loads(text)


def semantic_json_text(source: str, code: str, *, timeout: int = 120, indent: int = 2) -> str:
    return json.dumps(semantic_json(source, code, timeout=timeout), indent=indent, ensure_ascii=False)


def transpile_many(source: str, targets: List[str], code: str, *, timeout: int = 120) -> Dict[str, str]:
    # SE uses a different import/export boundary, so route mixed fanout calls
    # individually when Semantic is involved.
    if source.lower().strip() in {"se", "semantic", "sp"} or any(t.lower().strip() in {"se", "semantic", "sp"} for t in targets):
        return {target: transpile(source, target, code, timeout=timeout) for target in targets}
    result = request(source, code=code, mode="fanout", targets=targets, timeout=timeout)
    out: Dict[str, str] = {}
    for item in result.get("results", []):
        if item.get("error"):
            raise CodeTranspilerError(f"{item.get('id')}: {item['error']}")
        out[str(item.get("id"))] = str(item.get("code", ""))
    return out
