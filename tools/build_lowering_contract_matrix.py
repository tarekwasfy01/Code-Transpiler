#!/usr/bin/env python3
import csv
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "outputs" / "lowering-contract-matrix"

def read(path):
    with path.open(encoding="utf-8-sig", newline="") as f:
        return list(csv.DictReader(f))

def main():
    OUT.mkdir(parents=True, exist_ok=True)
    py = read(ROOT / "outputs/py2many-contracts/py2many_lowering_contracts.csv")
    comp = read(ROOT / "outputs/compiler-semantic-oracle/compiler_ir_semantic_matrix.csv")
    existing = {r["canonical_primitive"] for r in read(ROOT / "outputs/compiler-semantic-oracle/primitive_existing_implementation_matrix.csv") if r.get("product_handler_present") == "true"}
    aliases = {"SYMBOL":"BINDING_DECLARATION", "COMPARISON":"BINARY_UNARY_OPERATOR", "BINARY_OPERATOR":"BINARY_UNARY_OPERATOR", "UNARY_OPERATOR":"BINARY_UNARY_OPERATOR", "FUNCTION_DECL":"CLOSURE_FUNCTION", "FUNCTION_VALUE":"CLOSURE_FUNCTION", "SELECTOR":"MEMBER_ACCESS", "RETURN":"CONTROL_TRANSFER", "INDEX_READ":"INDEX_SLICE", "LOOP":"ITERATION"}
    rows = []
    seen = set()
    for r in py:
        key = ("py2many", r.get("fixture", ""), r.get("ast_node", ""), r.get("primitive", ""), "target-contract")
        if key in seen: continue
        seen.add(key)
        prim = r.get("primitive","")
        rows.append({"source":"py2many","language":"python","target":"all-py2many-targets","construct":r.get("ast_node",""),"primitive":prim,"uast_contract":r.get("uast_contract",""),"context":r.get("parent_context",""),"operand_arity":r.get("operand_arity",""),"status":"IMPLEMENTED" if aliases.get(prim, prim) in existing else "MISSING_CONTRACT"})
    for r in comp:
        key = ("compiler", r.get("language",""), r.get("compiler_node",""), r.get("canonical_primitive",""), "oracle")
        if key in seen: continue
        seen.add(key)
        prim = r.get("canonical_primitive","")
        if prim == "UNMAPPED_ORACLE_SEMANTICS":
            status = "UNMAPPED"
        elif prim == "COMPILER_IMPLEMENTATION_DETAIL":
            status = "VALIDATION_ONLY"
        else:
            status = "IMPLEMENTED" if prim in existing else "DERIVABLE"
        rows.append({"source":"compiler-oracle","language":r.get("language",""),"target":"all","construct":r.get("compiler_node",""),"primitive":prim,"uast_contract":"","context":r.get("source_kind",""),"operand_arity":"","status":status})
    fields = ["source","language","target","construct","primitive","uast_contract","context","operand_arity","status"]
    with (OUT / "lowering_contract_matrix.csv").open("w", encoding="utf-8", newline="") as f:
        w = csv.DictWriter(f, fieldnames=fields); w.writeheader(); w.writerows(rows)
    prims = sorted({r["primitive"] for r in rows if r["primitive"]})
    with (OUT / "lowering_contract_summary.csv").open("w", encoding="utf-8", newline="") as f:
        w = csv.DictWriter(f, fieldnames=["primitive","rows","implemented","derivable","missing_contract","unmapped"]); w.writeheader()
        for p in prims:
            q = [r for r in rows if r["primitive"] == p]
            w.writerow({"primitive":p,"rows":len(q),"implemented":sum(r["status"]=="IMPLEMENTED" for r in q),"derivable":sum(r["status"]=="DERIVABLE" for r in q),"missing_contract":sum(r["status"]=="MISSING_CONTRACT" for r in q),"unmapped":sum(r["status"]=="UNMAPPED" for r in q)})
    classes = {}
    for r in rows:
        key = (r["primitive"], r["operand_arity"], r["context"], r["status"])
        classes.setdefault(key, 0)
        classes[key] += 1
    with (OUT / "exact_contract_classes.csv").open("w", encoding="utf-8", newline="") as f:
        fields = ["class_id","primitive","operand_arity","context","status","rows"]
        w = csv.DictWriter(f, fieldnames=fields); w.writeheader()
        for i, ((p,a,c,s), n) in enumerate(sorted(classes.items()), 1):
            w.writerow({"class_id":f"C{i:04d}","primitive":p,"operand_arity":a,"context":c,"status":s,"rows":n})
    print(f"CONTRACT_ROWS={len(rows)}")
    print(f"UNIQUE_PRIMITIVES={len(prims)}")
    print(f"IMPLEMENTED_ROWS={sum(r['status']=='IMPLEMENTED' for r in rows)}")
    print(f"DERIVABLE_ROWS={sum(r['status']=='DERIVABLE' for r in rows)}")
    print(f"MISSING_CONTRACT_ROWS={sum(r['status']=='MISSING_CONTRACT' for r in rows)}")
    print(f"UNMAPPED_ROWS={sum(r['status']=='UNMAPPED' for r in rows)}")
    print(f"EXACT_CONTRACT_CLASSES={len(classes)}")

if __name__ == "__main__": main()
