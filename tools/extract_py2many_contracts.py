"""Extract empirical Python AST/target contracts from a local py2many checkout.

This is an oracle/report generator only: it never imports py2many and never
places its transpiler on the product path.  The output is deliberately keyed
to the existing UAST primitive vocabulary.
"""
from __future__ import annotations
import csv, hashlib, re
import ast
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SRC = ROOT / ".cache" / "py2many-main"
OUT = ROOT / "outputs" / "py2many-contracts"
TARGETS = {"cpp": "cpp", "go": "go", "rs": "rust", "jl": "julia", "kt": "kotlin", "nim": "nim", "zig": "zig", "dart": "dart", "d": "d"}
PRIMITIVES = {
    "List": ("AGGREGATE", "sequence.create"), "Tuple": ("AGGREGATE", "sequence.create"),
    "Set": ("AGGREGATE", "sequence.create"), "Dict": ("AGGREGATE", "map.create"),
    "Subscript": ("INDEX_READ", "index.base"), "Slice": ("INDEX_SLICE", "index.base"),
    "For": ("ITERATION", "iteration.range"), "ListComp": ("ITERATION", "iteration.range"),
    "Lambda": ("FUNCTION_VALUE", "uast.function"), "FunctionDef": ("FUNCTION_DECL", "uast.function"),
    "Call": ("CALL", "uast.call"), "Return": ("RETURN", "uast.return"),
    "If": ("CONDITIONAL", "uast.conditional"), "While": ("LOOP", "uast.loop"),
    "BinOp": ("BINARY_OPERATOR", "uast.binary"), "UnaryOp": ("UNARY_OPERATOR", "uast.unary"),
    "Compare": ("COMPARISON", "uast.binary"), "Assign": ("ASSIGNMENT", "uast.assignment"),
    "AnnAssign": ("ASSIGNMENT", "uast.assignment"), "Constant": ("LITERAL", "literal.number"),
    "Name": ("SYMBOL", "projection.identifier_serialization"), "Attribute": ("SELECTOR", "uast.expression"),
}

def visitor_nodes(path: Path):
    text = path.read_text(encoding="utf-8")
    return set(re.findall(r"def visit_([A-Za-z][A-Za-z0-9_]*)\s*\(", text))

def main():
    if not SRC.exists(): raise SystemExit(f"missing checkout: {SRC}")
    OUT.mkdir(parents=True, exist_ok=True)
    emitters = sorted(p for p in SRC.glob("py*/transpiler.py"))
    rows=[]
    for path in emitters:
        target=path.parent.name[2:]
        if target not in TARGETS: continue
        for node in sorted(visitor_nodes(path)):
            if node in PRIMITIVES:
                prim,uast=PRIMITIVES[node]
                rows.append({"target":TARGETS[target],"emitter":str(path.relative_to(SRC)),"ast_node":node,"primitive":prim,"uast_contract":uast,"source_sha256":hashlib.sha256(path.read_bytes()).hexdigest()})
    expected=[]
    for p in (SRC/"tests"/"expected").glob("*"):
        if p.suffix[1:] in TARGETS:
            expected.append({"file":p.name,"target":TARGETS[p.suffix[1:]],"bytes":p.stat().st_size,"sha256":hashlib.sha256(p.read_bytes()).hexdigest()})
    fixtures=[]
    lowering=[]
    for p in sorted((SRC/"tests"/"cases").glob("*.py")):
        try:
            tree=ast.parse(p.read_text(encoding="utf-8"))
        except (OSError,SyntaxError):
            continue
        nodes=sorted({type(n).__name__ for n in ast.walk(tree) if type(n).__name__ in PRIMITIVES})
        for node in nodes:
            prim,uast=PRIMITIVES[node]
            fixtures.append({"fixture":p.name,"ast_node":node,"primitive":prim,"uast_contract":uast,"source_sha256":hashlib.sha256(p.read_bytes()).hexdigest()})
        for n in ast.walk(tree):
            name=type(n).__name__
            if name not in PRIMITIVES: continue
            prim,uast=PRIMITIVES[name]
            if isinstance(n, ast.Call):
                arity=len(n.args)+len(n.keywords)
            elif isinstance(n, (ast.BinOp, ast.BoolOp, ast.Compare)):
                arity=2
            elif isinstance(n, (ast.FunctionDef, ast.Lambda)):
                arity=len(getattr(n.args, "args", []))
            elif isinstance(n, (ast.For, ast.comprehension)):
                arity=1
            else:
                arity=0
            parents=[type(x).__name__ for x in ast.walk(tree) if n in ast.iter_child_nodes(x)]
            context=";".join(sorted(set(parents)))
            lowering.append({"fixture":p.name,"ast_node":name,"primitive":prim,"uast_contract":uast,"operand_arity":arity,"parent_context":context,"has_type_annotation":bool(getattr(n,"annotation",None) or getattr(n,"returns",None)),"has_control_flow":any(isinstance(x,(ast.If,ast.For,ast.While,ast.Try)) for x in ast.walk(n)),"source_sha256":hashlib.sha256(p.read_bytes()).hexdigest()})
    with (OUT/"py2many_ast_target_contracts.csv").open("w",newline="",encoding="utf-8") as f:
        w=csv.DictWriter(f,fieldnames=["target","emitter","ast_node","primitive","uast_contract","source_sha256"]); w.writeheader(); w.writerows(rows)
    with (OUT/"py2many_expected_corpus.csv").open("w",newline="",encoding="utf-8") as f:
        w=csv.DictWriter(f,fieldnames=["file","target","bytes","sha256"]); w.writeheader(); w.writerows(expected)
    with (OUT/"py2many_fixture_coverage.csv").open("w",newline="",encoding="utf-8") as f:
        w=csv.DictWriter(f,fieldnames=["fixture","ast_node","primitive","uast_contract","source_sha256"]); w.writeheader(); w.writerows(fixtures)
    with (OUT/"py2many_lowering_contracts.csv").open("w",newline="",encoding="utf-8") as f:
        fields=["fixture","ast_node","primitive","uast_contract","operand_arity","parent_context","has_type_annotation","has_control_flow","source_sha256"]
        w=csv.DictWriter(f,fieldnames=fields); w.writeheader(); w.writerows(lowering)
    with (OUT/"py2many_contract_summary.csv").open("w",newline="",encoding="utf-8") as f:
        w=csv.DictWriter(f,fieldnames=["target","ast_nodes","primitive_contracts","expected_files"]); w.writeheader()
        for t in sorted(set(r["target"] for r in rows)|set(r["target"] for r in expected)):
            w.writerow({"target":t,"ast_nodes":len({r["ast_node"] for r in rows if r["target"]==t}),"primitive_contracts":len({r["primitive"] for r in rows if r["target"]==t}),"expected_files":sum(r["target"]==t for r in expected)})
    print(f"AST_CONTRACT_ROWS={len(rows)}")
    print(f"EXPECTED_OUTPUTS={len(expected)}")
    print(f"TARGET_EMITTERS={len(emitters)}")
    print(f"SOURCE_FIXTURE_CONTRACT_ROWS={len(fixtures)}")
    print(f"LOWERING_CONTRACT_ROWS={len(lowering)}")

if __name__ == "__main__": main()
