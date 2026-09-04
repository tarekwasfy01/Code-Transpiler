import csv,re
from pathlib import Path
from collections import Counter
ROOT=Path(__file__).resolve().parents[1]
OUT=ROOT/'outputs/runtime-smoke-controlled'
def main():
 rows=list(csv.DictReader((OUT/'runtime_failure_contract_matrix.csv').open(encoding='utf-8-sig'))); out=[]
 for r in rows:
  t=(r['case']+' '+r['diagnostic']).lower(); hits=[]
  for p,pat in [('INDEX_SLICE','index|bound|subscript'),('TYPE','type|mismatch|expected'),('BINDING_REFERENCE','name|undefined|not found'),('CALL','call|argument|function'),('CONVERSION','convert|cast')]:
   if re.search(pat,t): hits.append(p)
  out.append({**r,'primitive_candidates':';'.join(hits) if hits else 'UNMAPPED','first_bad_stage':'TARGET_RUNTIME' if 'runtime' in r['failure_class'].lower() else 'TARGET_COMPILE'})
 with (OUT/'runtime_failure_primitive_matrix.csv').open('w',encoding='utf-8',newline='') as f:
  w=csv.DictWriter(f,fieldnames=list(out[0])); w.writeheader(); w.writerows(out)
 c=Counter(r['primitive_candidates'] for r in out)
 with (OUT/'runtime_failure_primitive_summary.csv').open('w',encoding='utf-8',newline='') as f:
  w=csv.DictWriter(f,fieldnames=['primitive_candidates','rows']); w.writeheader(); w.writerows({'primitive_candidates':k,'rows':v} for k,v in sorted(c.items()))
 print(f'RUNTIME_FAILURE_ROWS={len(out)}'); print(f'RUNTIME_PRIMITIVE_CLASSES={len(c)}'); print(f'UNMAPPED_RUNTIME_FAILURES={c.get("UNMAPPED",0)}')
if __name__=='__main__': main()
