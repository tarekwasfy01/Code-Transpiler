import csv
from pathlib import Path
from collections import Counter

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "outputs" / "unified-evidence-matrix"

def load(path):
    if not path.exists(): return []
    with path.open(encoding="utf-8-sig", newline="") as f: return list(csv.DictReader(f))

def main():
    OUT.mkdir(parents=True, exist_ok=True); rows=[]
    sources = [
        ("miner", ROOT/"outputs/miner-v4.3-all-to-all/failure_matrix.csv"),
        ("site-packages", ROOT/"outputs/site-packages-alltoall-matrix/failure_matrix.csv"),
        ("compiler-oracle", ROOT/"outputs/compiler-semantic-oracle/compiler_ir_semantic_matrix.csv"),
        ("py2many", ROOT/"outputs/py2many-contracts/py2many_lowering_contracts.csv"),
        ("lowering", ROOT/"outputs/lowering-contract-matrix/lowering_contract_matrix.csv"),
    ]
    for source,path in sources:
        for r in load(path):
            rows.append({"source":source,"language":r.get("language",r.get("source_language","")),"target":r.get("target",r.get("target_language","")),"construct":r.get("construct",r.get("ast_node",r.get("compiler_node",""))),"primitive":r.get("primitive",r.get("canonical_primitive","")),"status":r.get("status",r.get("evidence_status",r.get("failure_kind",""))),"signature":r.get("failure_signature",r.get("uast_contract",r.get("context","")))})
    fields=["source","language","target","construct","primitive","status","signature"]
    with (OUT/"unified_evidence_matrix.csv").open("w",encoding="utf-8",newline="") as f:
        w=csv.DictWriter(f,fieldnames=fields); w.writeheader(); w.writerows(rows)
    c=Counter((r["source"],r["status"]) for r in rows)
    with (OUT/"unified_evidence_summary.csv").open("w",encoding="utf-8",newline="") as f:
        w=csv.DictWriter(f,fieldnames=["source","status","rows"]); w.writeheader()
        for (s,st),n in sorted(c.items()): w.writerow({"source":s,"status":st,"rows":n})
    print(f"UNIFIED_ROWS={len(rows)}")
    print(f"UNIFIED_PRIMITIVES={len({r['primitive'] for r in rows if r['primitive']})}")
    print(f"UNIFIED_SOURCES={len({r['source'] for r in rows})}")

if __name__ == "__main__": main()
