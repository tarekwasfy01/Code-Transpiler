"""Recompute supplied handoff products and collect fresh CPython AST evidence.

Uses the local Python parser, never executes scanned source, and never edits
coverage cells. Imported support estimates retain their baseline provenance.
"""
import argparse
import ast
import csv
import hashlib
import io
import json
import math
from pathlib import Path
import re
import sys
import time
import tokenize
import zipfile


def rows(archive, name):
    return list(csv.DictReader(io.StringIO(archive.read(name).decode("utf-8-sig"))))


def matvec(matrix, vector):
    if any(len(row) != len(vector) for row in matrix):
        raise ValueError("matrix/vector dimension mismatch")
    return [math.fsum(a * b for a, b in zip(row, vector)) for row in matrix]


def incidence(records, columns):
    ids = {name: i for i, name in enumerate(columns)}
    entries = []
    for row, values in enumerate(records):
        for value in sorted(set(values)):
            if value in ids:
                entries.append([row, ids[value], 1])
    return {"rows": len(records), "cols": len(columns), "storage": "coo", "entries": entries}


def transpose_product_vector(matrix, vector):
    if matrix["rows"] != len(vector):
        raise ValueError("incidence/vector dimension mismatch")
    out = [0.0] * matrix["cols"]
    for row, col, value in matrix["entries"]:
        out[col] += value * vector[row]
    return out


def save(path, obj):
    path.write_text(json.dumps(obj, indent=2, sort_keys=True, ensure_ascii=True, allow_nan=False) + "\n", encoding="utf-8")


def table(path, header, data):
    with path.open("w", newline="", encoding="utf-8") as stream:
        writer = csv.writer(stream)
        writer.writerow(header)
        writer.writerows(data)


def recompute(archive, out):
    gap_rows = rows(archive, "06_feature_gap_vector.csv")
    by_feature = {r["feature"]: r for r in gap_rows}
    operator = rows(archive, "09_gap_propagation_operator.csv")
    features = list(operator[0])[1:]
    if len(features) != len(set(features)) or set(features) != set(by_feature):
        raise ValueError("feature basis mismatch")
    op_by_feature = {r["feature"]: r for r in operator}
    if set(op_by_feature) != set(features):
        raise ValueError("operator row basis mismatch")
    op = [[float(op_by_feature[f][g]) for g in features] for f in features]
    base = [float(by_feature[f]["importance"]) * (1 - float(by_feature[f]["current_sp_support"])) for f in features]
    if any(not math.isfinite(x) or x < 0 for row in op for x in row) or any(not math.isfinite(x) or x < 0 for x in base):
        raise ValueError("invalid gap coefficients")
    propagated = matvec(op, base)
    # Exported coefficients are rounded; do not demand bit-identical products.
    delta = max(abs(propagated[i] - float(by_feature[f]["propagated_gap"])) for i, f in enumerate(features))
    if delta > 0.001:
        raise ValueError(f"supplied gap products cannot be reproduced: max error {delta}")
    cluster_rows = rows(archive, "13_cluster_feature_matrix.csv")
    cluster_matrix = [[float(row[f]) for f in features] for row in cluster_rows]
    cluster_scores = matvec(cluster_matrix, propagated)
    ordered = sorted(zip(cluster_rows, cluster_scores), key=lambda pair: -pair[1])
    table(out / "clusters.csv", ["cluster", "recomputed_gap"], [(r["cluster"], score) for r, score in ordered])
    table(out / "feature-gaps.csv", ["feature", "base_gap", "propagated_gap"], zip(features, base, propagated))
    with zipfile.ZipFile(io.BytesIO(archive.read("90_original_language_matrix_python.zip"))) as original:
        q = rows(original, "05_feature_sp_projection_matrix.csv")
        q_by = {r["feature"]: r for r in q}
        axes = list(q[0])[1:]
        axis_matrix = [[float(q_by[f][axis]) for f in features] for axis in axes]
        axis_gaps = matvec(axis_matrix, propagated)
        table(out / "axis-gaps.csv", ["axis", "gap"], sorted(zip(axes, axis_gaps), key=lambda x: -x[1]))
    canonical = {}
    for filename, name_field, label in [
        ("11_canonical_asdl_ast_gap_matrix.csv", "ast_node", "asdl"),
        ("12_canonical_python_grammar_gap_matrix.csv", "grammar_rule", "grammar"),
    ]:
        data = rows(archive, filename)
        # Some exports name grammar rules simply 'rule'. Keep the input schema explicit.
        if name_field not in data[0]:
            if label == "grammar" and "rule" in data[0]:
                name_field = "rule"
            else:
                raise ValueError(f"unexpected {label} columns: {list(data[0])}")
        mapped = [r["mapped_features"].split(";") for r in data]
        a = [[float(f in names) for f in features] for names in mapped]
        values = matvec(a, propagated)
        table(out / f"{label}-gaps.csv", [name_field, "gap"], zip([r[name_field] for r in data], values))
        canonical[label] = {r[name_field]: names for r, names in zip(data, mapped)}
    return {"features": features, "base_gap": base, "propagated_gap": propagated,
            "operator": op, "cluster_matrix": cluster_matrix,
            "cluster_names": [r["cluster"] for r in cluster_rows],
            "highest_cluster": ordered[0][0]["cluster"], "rounding_max_error": delta,
            "support_source": "unchanged handoff estimates; not current verified completion"}, canonical


# Rules generate every presence/count cell; none are manually assigned per file.
NODE_FEATURES = {
    "Module": ("module",), "ClassDef": ("class",),
    "FunctionDef": ("function",), "AsyncFunctionDef": ("function", "task_async"),
    "Import": ("import",), "ImportFrom": ("import",),
    "AnnAssign": ("attribute_annotation", "assignment"), "Assign": ("assignment",),
    "arg": ("parameter",), "TypeVar": ("generic",), "TypeVarTuple": ("generic",),
    "ParamSpec": ("generic",), "TypeAlias": ("generic",),
    "Tuple": ("tuple",), "List": ("array_list",), "Dict": ("map_dict",), "Set": ("set",),
    "Name": ("identifier",), "Call": ("call",), "Attribute": ("member_access",),
    "Subscript": ("index",), "Slice": ("slice",), "BinOp": ("binary_op",), "UnaryOp": ("unary_op",),
    "ListComp": ("comprehension",), "SetComp": ("comprehension",), "DictComp": ("comprehension",),
    "GeneratorExp": ("comprehension", "yield"), "Yield": ("yield",), "YieldFrom": ("yield",),
    "Await": ("await",), "Lambda": ("closure_lambda",), "Raise": ("throw_raise",),
    "Try": ("exception_try",), "TryStar": ("exception_try",), "Match": ("switch_match",),
    "For": ("for", "foreach_iteration"), "While": ("while",), "If": ("if",),
    "Return": ("return",), "Break": ("break",), "Continue": ("continue",),
}


def syntax(node):
    """Lossless Python syntax evidence; explicitly not a resolved universal type."""
    if isinstance(node, ast.AST):
        return {"kind": type(node).__name__, "fields": {name: syntax(value) for name, value in ast.iter_fields(node)}}
    if isinstance(node, list):
        return [syntax(x) for x in node]
    if isinstance(node, (bytes, complex)) or node is Ellipsis or isinstance(node, float) and not math.isfinite(node):
        return {"literal_kind": type(node).__name__, "value": repr(node)}
    return node


def declarations(tree):
    """CL01 syntax capture, separate from binding resolution and executable HIR."""
    result = []
    def visit(node, owner):
        kind = type(node).__name__
        if isinstance(node, (ast.Module, ast.ClassDef, ast.FunctionDef, ast.AsyncFunctionDef, ast.Import, ast.ImportFrom, ast.AnnAssign, ast.TypeAlias)):
            entry = {"id": len(result), "owner": owner, "kind": kind,
                     "line": getattr(node, "lineno", 0), "column": getattr(node, "col_offset", 0),
                     "end_line": getattr(node, "end_lineno", 0), "end_column": getattr(node, "end_col_offset", 0)}
            if hasattr(node, "name"):
                entry["name"] = syntax(node.name)
            for field in ("bases", "keywords", "decorator_list", "returns", "type_params", "annotation", "target", "module", "level", "names"):
                if hasattr(node, field):
                    entry[field] = syntax(getattr(node, field))
            if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
                args = node.args
                positional = args.posonlyargs + args.args
                defaults = [None] * (len(positional) - len(args.defaults)) + args.defaults
                params = []
                for i, (arg, default) in enumerate(zip(positional, defaults)):
                    params.append({"name": arg.arg, "passing": "positional_only" if i < len(args.posonlyargs) else "positional_or_keyword", "annotation": syntax(arg.annotation), "default": syntax(default), "has_default": default is not None})
                for arg, mode in ((args.vararg, "variadic_positional"),):
                    if arg:
                        params.append({"name": arg.arg, "passing": mode, "annotation": syntax(arg.annotation), "has_default": False})
                for arg, default in zip(args.kwonlyargs, args.kw_defaults):
                    params.append({"name": arg.arg, "passing": "keyword_only", "annotation": syntax(arg.annotation), "default": syntax(default), "has_default": default is not None})
                if args.kwarg:
                    params.append({"name": args.kwarg.arg, "passing": "variadic_keyword", "annotation": syntax(args.kwarg.annotation), "has_default": False})
                entry["parameters"] = params
            result.append(entry)
            owner = entry["id"]
        for child in ast.iter_child_nodes(node):
            visit(child, owner)
    visit(tree, -1)
    kinds = sorted({r["kind"] for r in result})
    kinds_matrix = incidence([[r["kind"]] for r in result], kinds)
    ownership = {"rows": len(result), "cols": len(result), "storage": "coo",
                 "entries": [[r["id"], r["owner"], 1] for r in result if r["owner"] >= 0]}
    return {"schema": "code-transpiler.python-declaration-evidence.v1", "executable": False,
            "declarations": result, "kinds": kinds, "kind_matrix": kinds_matrix,
            "kind_counts": transpose_product_vector(kinds_matrix, [1] * len(result)),
            "lexical_ownership": ownership,
            "limits": ["Syntax annotations are not resolved types.", "Lexical ownership is not Python execution scope or name resolution.", "Not a SemanticDocument and not accepted as executable HIR."]}


def scan(root, out, features, canonical):
    inventory, failures, counts = [], [], []
    declaration_dir = out / "declarations"
    declaration_dir.mkdir()
    total_nodes = total_declarations = 0
    for path in sorted(root.rglob("*")):
        if not path.is_file() or ".git" in path.parts or "__pycache__" in path.parts:
            continue
        relative = path.relative_to(root).as_posix()
        raw = path.read_bytes()
        inventory.append([relative, len(raw), hashlib.sha256(raw).hexdigest()])
        if path.suffix != ".py":
            continue
        try:
            encoding, _ = tokenize.detect_encoding(io.BytesIO(raw).readline)
            tree = ast.parse(raw.decode(encoding), filename=relative, type_comments=True)
        except (SyntaxError, UnicodeError, LookupError, ValueError, RecursionError) as error:
            failures.append({"file": relative, "error": str(error)})
            continue
        found = {}
        for node in ast.walk(tree):
            total_nodes += 1
            for feature in NODE_FEATURES.get(type(node).__name__, ()):
                found[feature] = found.get(feature, 0) + 1
        counts.append([relative] + [found.get(f, 0) for f in features])
        decl = declarations(tree)
        total_declarations += len(decl["declarations"])
        decl["file"] = relative
        decl["sha256"] = inventory[-1][2]
        # Path hash prevents basename collisions without relying on platform paths.
        save(declaration_dir / (hashlib.sha256(relative.encode()).hexdigest() + ".json"), decl)
    table(out / "source-inventory.csv", ["file", "bytes", "sha256"], inventory)
    table(out / "python-file-feature-counts.csv", ["file"] + features, counts)
    presence = [[int(x > 0) for x in row[1:]] for row in counts]
    n = len(features)
    # P^T P: sparse row outer products, evaluated locally over the complete scan.
    coupling = [[0] * n for _ in range(n)]
    for row in presence:
        active = [i for i, value in enumerate(row) if value]
        for i in active:
            for j in active:
                coupling[i][j] += 1
    table(out / "python-cooccurrence.csv", ["feature"] + features, [[f] + coupling[i] for i, f in enumerate(features)])
    save(out / "parse-failures.json", failures)
    asdl = (root / "Parser/Python.asdl").read_text(encoding="utf-8")
    asdl = re.sub(r"--[^\n]*", "", asdl)
    constructors = sorted(set(re.findall(r"(?:=|\|)\s*([A-Z]\w*)", asdl)))
    grammar = (root / "Grammar/python.gram").read_text(encoding="utf-8")
    grammar = re.sub(r"'''[\s\S]*?'''", "", grammar)
    grammar_rules = sorted(set(re.findall(r"^([a-z_]\w*)(?:\[[^\n]*?\])?(?:\([^\n]*?\))?\s*:", grammar, re.M)))
    current = {"asdl_constructors": constructors, "peg_rule_headers": grammar_rules,
               "asdl_unmapped": sorted(set(constructors) - set(canonical["asdl"])),
               "grammar_unmapped": sorted(set(grammar_rules) - set(canonical["grammar"]))}
    save(out / "canonical-current.json", current)
    return {"files_hashed": len(inventory), "python_parsed": len(counts), "python_parse_failures": len(failures),
            "ast_nodes": total_nodes, "declarations": total_declarations,
            "asdl_constructors": len(constructors), "peg_rule_headers": len(grammar_rules),
            "collector": "fresh AST-based Python-source evidence; not the unavailable original mixed-source collector"}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--handoff", type=Path, required=True)
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--out", type=Path, default=Path("outputs/python-handoff") / time.strftime("%Y%m%d-%H%M%S"))
    args = parser.parse_args()
    if not (args.source / "Parser/Python.asdl").is_file() or not (args.source / "Grammar/python.gram").is_file():
        parser.error("source must contain CPython canonical ASDL and PEG grammar")
    args.out.mkdir(parents=True, exist_ok=False)
    with zipfile.ZipFile(args.handoff) as archive:
        calculation, canonical = recompute(archive, args.out)
    print("Matrix-selected cluster:", calculation["highest_cluster"], flush=True)
    print("Scanning CPython source locally (source is parsed, never executed)...", flush=True)
    collected = scan(args.source, args.out, calculation["features"], canonical)
    save(args.out / "report.json", {"calculation": calculation, "scan": collected,
         "handoff_sha256": hashlib.sha256(args.handoff.read_bytes()).hexdigest(),
         "collector_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
         "python": sys.version, "source": str(args.source.resolve()),
         "completion_change": "not claimed; syntax capture does not establish interpreter/backend parity"})
    (args.out / "SUMMARY.md").write_text(
        "# Python handoff calculation\n\n"
        f"Highest recomputed baseline cluster: **{calculation['highest_cluster']}**. "
        f"Maximum exported-rounding discrepancy: {calculation['rounding_max_error']:.8f}.\n\n"
        f"Fresh local scan: {collected['files_hashed']} files hashed; "
        f"{collected['python_parsed']} Python files parsed; {collected['python_parse_failures']} parse failures. "
        f"{collected['ast_nodes']} AST nodes and {collected['declarations']} declaration records.\n\n"
        f"Canonical-source extraction: {collected['asdl_constructors']} ASDL constructors, "
        f"{collected['peg_rule_headers']} PEG rule headers. These counts are not implementation coverage.\n\n"
        "See clusters.csv, axis-gaps.csv, feature-gaps.csv and report.json for recomputed products. "
        "Fresh file-feature counts and cooccurrence are in python-file-feature-counts.csv and python-cooccurrence.csv. "
        "Failures are retained in parse-failures.json. Declaration syntax and matrices are in declarations/.\n\n"
        "Imported support estimates were not upgraded. The original mixed-source collector was not supplied; "
        "this AST collector is separate evidence, not an identical rerun of that collector. "
        "Executable SemanticProgram lowering and target parity for CL01 remain open.\n", encoding="utf-8")
    print(json.dumps(collected, indent=2))
    print("Report:", args.out.resolve())


if __name__ == "__main__":
    main()
