"""Bootstrap helper: transpile Code Transpiler's own Go sources to Python.

Normal mode first tries a complete Go file. If that fails or times out, the file
is split into smaller top-level declaration chunks (with package/import preamble
retained) and each chunk is sent through the existing Go->Python projector.
This makes large compiler packages incrementally portable instead of treating a
whole file as all-or-nothing.
"""
from __future__ import annotations

import argparse
import json
import re
import subprocess
from pathlib import Path


def convert_text(engine: Path, code: str, ident: str, timeout: int) -> tuple[str, str]:
    request = [{"id": ident, "source": "go", "target": "python", "code": code}]
    try:
        p = subprocess.run(
            [str(engine)], input=json.dumps(request).encode("utf-8"),
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout
        )
    except subprocess.TimeoutExpired:
        return "", "timeout"
    if p.returncode:
        return "", p.stderr.decode(errors="replace").strip() or f"exit {p.returncode}"
    try:
        item = json.loads(p.stdout.decode("utf-8"))[0]
    except Exception as exc:
        return "", f"invalid engine JSON: {exc}"
    if item.get("error"):
        return "", str(item["error"])
    out = str(item.get("code", ""))
    if not out.strip():
        return "", "empty output"
    return out, ""


def convert(engine: Path, source: Path, timeout: int) -> tuple[str, str]:
    return convert_text(engine, source.read_text(encoding="utf-8", errors="replace"), str(source), timeout)


def _strip_comments_and_strings(line: str) -> str:
    # Only for brace counting. It deliberately stays conservative.
    line = re.sub(r'"(?:\\.|[^"\\])*"', '""', line)
    line = re.sub(r'`[^`]*`', '``', line)
    line = re.sub(r"//.*$", "", line)
    return line


def split_go_source(text: str, max_lines: int = 220) -> tuple[str, list[str]]:
    """Return preamble and top-level declaration chunks.

    The preamble contains package/import declarations. Chunks are split only at
    brace depth zero, so functions/types are never cut in the middle. Oversized
    groups of simple declarations are split at safe blank-line boundaries.
    """
    lines = text.splitlines()
    preamble: list[str] = []
    body_start = 0
    depth = 0
    in_import_block = False

    for i, line in enumerate(lines):
        s = line.strip()
        if i == 0 and s.startswith("package "):
            preamble.append(line)
            body_start = i + 1
            continue
        if s.startswith("import ("):
            in_import_block = True
            preamble.append(line)
            body_start = i + 1
            continue
        if in_import_block:
            preamble.append(line)
            body_start = i + 1
            if s == ")":
                in_import_block = False
            continue
        if s.startswith("import ") and depth == 0:
            preamble.append(line)
            body_start = i + 1
            continue
        if not s and i <= body_start + 1:
            preamble.append(line)
            body_start = i + 1
            continue
        break

    chunks: list[str] = []
    current: list[str] = []
    depth = 0
    for line in lines[body_start:]:
        current.append(line)
        clean = _strip_comments_and_strings(line)
        depth += clean.count("{") - clean.count("}")
        safe_boundary = depth == 0 and (not line.strip() or len(current) >= max_lines)
        if safe_boundary and any(x.strip() for x in current):
            chunks.append("\n".join(current).strip() + "\n")
            current = []
    if any(x.strip() for x in current):
        chunks.append("\n".join(current).strip() + "\n")
    return "\n".join(preamble).rstrip() + "\n\n", chunks


def convert_chunked(engine: Path, source: Path, timeout: int, max_lines: int) -> tuple[list[str], list[dict]]:
    text = source.read_text(encoding="utf-8", errors="replace")
    preamble, chunks = split_go_source(text, max_lines=max_lines)
    converted: list[str] = []
    details: list[dict] = []
    for idx, chunk in enumerate(chunks, 1):
        # Each part is valid enough for the parser because it gets the original
        # package/import context. The output stays separate to avoid unsafe
        # textual merging of independently projected modules.
        code, err = convert_text(engine, preamble + chunk, f"{source}#part{idx}", timeout)
        if code:
            converted.append(code)
            details.append({"part": idx, "status": "converted", "chars": len(code)})
        else:
            details.append({"part": idx, "status": "manual", "reason": err})
    return converted, details


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("repo", type=Path)
    ap.add_argument("engine", type=Path)
    ap.add_argument("out", type=Path)
    ap.add_argument("--timeout", type=int, default=20)
    ap.add_argument("--limit", type=int, default=0)
    ap.add_argument("--chunk-lines", type=int, default=220)
    ns = ap.parse_args()

    files = [p for p in ns.repo.rglob("*.go") if not p.name.endswith("_test.go")]
    if ns.limit:
        files = files[:ns.limit]

    report = []
    for path in files:
        rel = path.relative_to(ns.repo)
        code, err = convert(ns.engine, path, ns.timeout)
        dst = ns.out / rel.with_suffix(".py")
        if code:
            dst.parent.mkdir(parents=True, exist_ok=True)
            dst.write_text(code, encoding="utf-8")
            item = {"file": str(rel), "status": "converted", "chars": len(code), "mode": "whole-file"}
        else:
            parts, details = convert_chunked(ns.engine, path, ns.timeout, ns.chunk_lines)
            part_dir = ns.out / rel.parent / (rel.stem + "_parts")
            for i, part in enumerate(parts, 1):
                part_dir.mkdir(parents=True, exist_ok=True)
                (part_dir / f"part_{i:03d}.py").write_text(part, encoding="utf-8")
            ok = sum(1 for x in details if x["status"] == "converted")
            item = {
                "file": str(rel), "status": "chunked" if ok else "manual",
                "whole_file_reason": err, "converted_parts": ok,
                "total_parts": len(details), "parts": details,
            }
        report.append(item)
        print(item)

    ns.out.mkdir(parents=True, exist_ok=True)
    (ns.out / "self_transpile_report.json").write_text(json.dumps(report, indent=2), encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
