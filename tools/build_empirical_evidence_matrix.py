import csv
from pathlib import Path
from collections import Counter
ROOT=Path(__file__).resolve().parents[1]; SRC=ROOT/'outputs/unmapped-oracle-evidence/unmapped_empirical_evidence.csv'; OUT=ROOT/'outputs/unmapped-oracle-evidence'
def main():
 rows=list(csv.DictReader(SRC.open(encoding='utf-8-sig'))); classes=Counter()
 for r in rows:
  key=(r['source_kind'],r['status'],r['py2many_target_count'],r['miner_language_seen']); classes[key]+=1
 with (OUT/'empirical_evidence_matrix.csv').open('w',encoding='utf-8',newline='') as f:
  fields=['case_id','language','source_kind','compiler_node','py2many_target_count','miner_language_seen','status']; w=csv.DictWriter(f,fieldnames=fields);w.writeheader()
  for i,r in enumerate(rows,1): w.writerow({'case_id':f'E{i:04d}',**{k:r.get(k,'') for k in fields[1:]}})
 with (OUT/'empirical_evidence_exact_classes.csv').open('w',encoding='utf-8',newline='') as f:
  fields=['class_id','source_kind','status','py2many_target_count','miner_language_seen','rows'];w=csv.DictWriter(f,fieldnames=fields);w.writeheader()
  for i,((s,t,p,m),n) in enumerate(sorted(classes.items()),1):w.writerow({'class_id':f'EC{i:04d}','source_kind':s,'status':t,'py2many_target_count':p,'miner_language_seen':m,'rows':n})
 print(f'EVIDENCE_ROWS={len(rows)}');print(f'EXACT_EVIDENCE_CLASSES={len(classes)}')
if __name__=='__main__':main()
