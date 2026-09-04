import csv
from pathlib import Path
from collections import Counter
ROOT=Path(__file__).resolve().parents[1]; SRC=ROOT/'outputs/partial-unified-matrix/partial_unified_matrix.csv'; OUT=ROOT/'outputs/partial-unified-matrix'
def main():
 rows=list(csv.DictReader(SRC.open(encoding='utf-8-sig')))
 # Preserve primitive vectors; blank/unknown primitive is an explicit residual.
 groups=Counter((r.get('primitive','') or 'UNMAPPED',r.get('status',''),r.get('signature','')) for r in rows)
 with (OUT/'partial_primitive_matrix.csv').open('w',encoding='utf-8',newline='') as f:
  w=csv.DictWriter(f,fieldnames=['primitive','status','signature','rows']);w.writeheader()
  for (p,s,g),n in sorted(groups.items()):w.writerow({'primitive':p,'status':s,'signature':g,'rows':n})
 prim=Counter((r.get('primitive','') or 'UNMAPPED') for r in rows)
 with (OUT/'partial_primitive_summary.csv').open('w',encoding='utf-8',newline='') as f:
  w=csv.DictWriter(f,fieldnames=['primitive','rows']);w.writeheader();w.writerows({'primitive':p,'rows':n} for p,n in sorted(prim.items()))
 print(f'PRIMITIVE_ROWS={len(rows)}');print(f'UNIQUE_PRIMITIVES={len(prim)}');print(f'UNMAPPED_ROWS={prim.get("UNMAPPED",0)}')
if __name__=='__main__':main()
