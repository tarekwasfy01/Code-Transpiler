import csv
from pathlib import Path
from collections import Counter
ROOT=Path(__file__).resolve().parents[1]; SRC=ROOT/'outputs/partial-unified-matrix/partial_exact_classes.csv'; OUT=ROOT/'outputs/partial-unified-matrix'
def main():
 rows=list(csv.DictReader(SRC.open(encoding='utf-8-sig'))); groups=Counter((r['source'],r['language'],r['target'],r['primitive'],r['status']) for r in rows)
 with (OUT/'partial_exact_class_matrix.csv').open('w',encoding='utf-8',newline='') as f:
  fs=['matrix_class','source','language','target','primitive','status','rows']; w=csv.DictWriter(f,fieldnames=fs);w.writeheader()
  for i,(k,n) in enumerate(sorted(groups.items()),1):w.writerow({'matrix_class':f'M{i:06d}','source':k[0],'language':k[1],'target':k[2],'primitive':k[3],'status':k[4],'rows':n})
 with (OUT/'partial_primitive_status_matrix.csv').open('w',encoding='utf-8',newline='') as f:
  prim=sorted({k[3] for k in groups}); statuses=sorted({k[4] for k in groups}); fs=['primitive']+statuses;w=csv.DictWriter(f,fieldnames=fs);w.writeheader()
  for p in prim:
   row={'primitive':p};
   for s in statuses:row[s]=sum(n for (src,lang,tgt,pr,st),n in groups.items() if pr==p and st==s)
   w.writerow(row)
 print(f'INPUT_CLASSES={len(rows)}');print(f'EXACT_MATRIX_CLASSES={len(groups)}');print(f'PRIMITIVE_COLUMNS={len(prim)}')
if __name__=='__main__':main()
