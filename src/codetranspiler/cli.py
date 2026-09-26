from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from importlib.resources import files

from . import CodeTranspilerError, languages, semantic_json_text, transpile


def _read(path: str | None) -> str:
    if not path or path == "-":
        return sys.stdin.read()
    return Path(path).read_text(encoding="utf-8")


def _write(path: str | None, text: str) -> None:
    if path:
        Path(path).write_text(text, encoding="utf-8")
    else:
        sys.stdout.write(text)
        if text and not text.endswith("\n"):
            sys.stdout.write("\n")


def build_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(prog="code-transpiler")
    sub = p.add_subparsers(dest="command", required=True)

    t = sub.add_parser("transpile", help="transpile source code")
    t.add_argument("input", nargs="?", default="-")
    t.add_argument("--source", "-s", required=True)
    t.add_argument("--target", "-t", required=True)
    t.add_argument("--output", "-o")

    sj = sub.add_parser("semantic-json", help="export SemanticProgram JSON")
    sj.add_argument("input", nargs="?", default="-")
    sj.add_argument("--source", "-s", required=True)
    sj.add_argument("--output", "-o")

    sub.add_parser("languages", help="list supported languages")
    sub.add_parser("licenses", help="list bundled license and notice files")
    sub.add_parser("gui", help="launch Tkinter GUI")
    return p


def main(argv: list[str] | None = None) -> int:
    ns = build_parser().parse_args(argv)
    try:
        if ns.command == "languages":
            for lang in languages():
                print(f"{lang['id']}\t{lang['name']}\t{','.join(lang['extensions'])}")
            return 0
        if ns.command == "licenses":
            root = files("codetranspiler").joinpath("licenses")
            for item in sorted(root.iterdir(), key=lambda x: x.name.lower()):
                print(item.name)
            return 0
        if ns.command == "gui":
            from .gui import main as gui_main
            gui_main()
            return 0
        if ns.command == "transpile":
            _write(ns.output, transpile(ns.source, ns.target, _read(ns.input)))
            return 0
        if ns.command == "semantic-json":
            _write(ns.output, semantic_json_text(ns.source, _read(ns.input)))
            return 0
    except (CodeTranspilerError, OSError, ValueError, json.JSONDecodeError) as exc:
        print(f"code-transpiler: {exc}", file=sys.stderr)
        return 1
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
