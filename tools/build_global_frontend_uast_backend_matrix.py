#!/usr/bin/env python3
"""Compatibility entrypoint for the evidence-backed frontend/UAST/backend join.

The previous script declared arbitrary structures supported for every target.
That made its zero-gap result a label, not a measurement. Keep the entrypoint
for automation, but delegate to the compiler-oracle builder which records a
positive mapping only with an oracle definition, a canonical UPI contract, and
a concrete execution-handler witness.
"""
from __future__ import annotations

import runpy
from pathlib import Path


if __name__ == "__main__":
    runpy.run_path(str(Path(__file__).with_name("build_compiler_semantic_oracle_matrix.py")), run_name="__main__")
