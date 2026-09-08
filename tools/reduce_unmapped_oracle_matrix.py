import csv, hashlib
from pathlib import Path
from collections import Counter

ROOT=Path(__file__).resolve().parents[1]
SRC=ROOT/"outputs/compiler-semantic-oracle/compiler_ir_semantic_matrix.csv"
OUT=ROOT/"outputs/unmapped-oracle-matrix"; OUT.mkdir(parents=True,exist_ok=True)

def main():
    rows=list(csv.DictReader(SRC.open(encoding='utf-8-sig')))
    rows=[r for r in rows if r.get('canonical_primitive')=='UNMAPPED_ORACLE_SEMANTICS']
    vec=[]; seen=set()
    for r in rows:
        sig=(r.get('language',''),r.get('source_kind',''),r.get('compiler_node',''),r.get('evidence',''))
        if sig in seen: continue
        seen.add(sig)
        vec.append({'case_id':hashlib.sha1('|'.join(sig).encode()).hexdigest()[:16],'language':sig[0],'source_kind':sig[1],'compiler_node':sig[2],'evidence':sig[3],'node_family':r.get('source_kind',''),'status':'UNMAPPED'})
    fields=['case_id','language','source_kind','compiler_node','evidence','node_family','status']
    with (OUT/'unmapped_oracle_matrix.csv').open('w',encoding='utf-8',newline='') as f:
        w=csv.DictWriter(f,fieldnames=fields); w.writeheader(); w.writerows(vec)
    groups=Counter((r['source_kind'],r['node_family']) for r in vec)
    with (OUT/'exact_unmapped_row_classes.csv').open('w',encoding='utf-8',newline='') as f:
        w=csv.DictWriter(f,fieldnames=['class_id','source_kind','node_family','rows']); w.writeheader()
        for i,((s,n),count) in enumerate(sorted(groups.items()),1): w.writerow({'class_id':f'U{i:04d}','source_kind':s,'node_family':n,'rows':count})
    cols=Counter(r['source_kind'] for r in vec)
    with (OUT/'unmapped_column_classes.csv').open('w',encoding='utf-8',newline='') as f:
        w=csv.DictWriter(f,fieldnames=['column_class','rows']); w.writeheader(); w.writerows({'column_class':k,'rows':v} for k,v in sorted(cols.items()))
    print(f'UNMAPPED_ROWS={len(vec)}')
    print(f'EXACT_UNMAPPED_ROW_CLASSES={len(groups)}')
    print(f'EXACT_UNMAPPED_COLUMN_CLASSES={len(cols)}')

if __name__=='__main__': main()
