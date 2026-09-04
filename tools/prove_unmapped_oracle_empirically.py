import csv
from pathlib import Path
from collections import defaultdict
ROOT=Path(__file__).resolve().parents[1]; OUT=ROOT/'outputs/unmapped-oracle-evidence'; OUT.mkdir(parents=True,exist_ok=True)
def read(p):
    if not p.exists(): return []
    with p.open(encoding='utf-8-sig',newline='') as f:return list(csv.DictReader(f))
def main():
    oracle=read(ROOT/'outputs/compiler-semantic-oracle/compiler_ir_semantic_matrix.csv')
    py=read(ROOT/'outputs/py2many-contracts/py2many_ast_target_contracts.csv')
    exp=read(ROOT/'outputs/py2many-contracts/py2many_expected_corpus.csv')
    miner=read(ROOT/'outputs/combined-miner-sitepackages-matrix/failure_matrix.csv')
    py_nodes=defaultdict(set)
    for r in py: py_nodes[r.get('ast_node','')].add(r.get('target',''))
    expected_targets={r.get('target','') for r in exp}
    miner_langs={r.get('source_language','') for r in miner}
    rows=[]
    for r in oracle:
        if r.get('canonical_primitive')!='UNMAPPED_ORACLE_SEMANTICS': continue
        node=r.get('compiler_node',''); evidence=r.get('evidence',''); kind=r.get('source_kind','')
        targets=len(py_nodes.get(node,set()));
        if targets>=2: status='EMPIRICALLY_PROVEN'
        elif kind in ('jvm_opcode','python_opcode','nim_opcode','r_opcode','rust_mir_variant'): status='VALIDATION_ONLY'
        elif any(k.lower() in (node+' '+evidence).lower() for k in ('add','sub','mul','div','call','return','load','store','branch','cast','array','tuple','slice','index')): status='STRUCTURALLY_DERIVABLE'
        else: status='UNMAPPED'
        rows.append({'language':r.get('language',''),'source_kind':kind,'compiler_node':node,'evidence':evidence,'py2many_target_count':targets,'py2many_expected_target_count':len(expected_targets),'miner_language_seen':r.get('language','') in miner_langs,'status':status})
    with (OUT/'unmapped_empirical_evidence.csv').open('w',encoding='utf-8',newline='') as f:
        fields=list(rows[0]) if rows else ['language']; w=csv.DictWriter(f,fieldnames=fields); w.writeheader(); w.writerows(rows)
    counts=defaultdict(int)
    for r in rows: counts[r['status']]+=1
    with (OUT/'unmapped_empirical_summary.csv').open('w',encoding='utf-8',newline='') as f:
        w=csv.DictWriter(f,fieldnames=['status','rows']);w.writeheader();w.writerows({'status':k,'rows':v} for k,v in sorted(counts.items()))
    print(f'UNMAPPED_ORACLE_ROWS={len(rows)}')
    for k in ('EMPIRICALLY_PROVEN','STRUCTURALLY_DERIVABLE','VALIDATION_ONLY','UNMAPPED'): print(f'{k}={counts[k]}')
if __name__=='__main__':main()
