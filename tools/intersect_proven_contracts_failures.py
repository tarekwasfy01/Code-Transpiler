import csv
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
OUT=ROOT/'outputs/partial-unified-matrix'
def read(p):
 with p.open(encoding='utf-8-sig',newline='') as f:return list(csv.DictReader(f))
def main():
 proven=[r for r in read(OUT/'empirical_contract_threshold_matrix.csv') if r['status']=='EMPIRICALLY_PROVEN']
 failures=read(OUT/'partial_primitive_matrix.csv'); out=[]
 for p in proven:
  prims=[x for x in p.get('primitive_candidates','').split(';') if x]
  hits=[r for r in failures if r.get('primitive','') in prims and r.get('status','') not in ('PASS','IMPLEMENTED')]
  out.append({'contract_id':p['contract_id'],'signature':p['signature'],'primitive_candidates':';'.join(prims),'failure_rows':len(hits),'failure_languages':len({r.get('language','') for r in hits if r.get('language','')}),'failure_targets':len({r.get('target','') for r in hits if r.get('target','')})})
 with (OUT/'proven_contract_failure_intersection.csv').open('w',encoding='utf-8',newline='') as f:
  w=csv.DictWriter(f,fieldnames=list(out[0]) if out else ['contract_id']);w.writeheader();w.writerows(out)
 print(f'PROVEN_CONTRACTS={len(proven)}');print(f'INTERSECTION_FAILURE_ROWS={sum(int(r["failure_rows"]) for r in out)}');print(f'FIXTURE_CANDIDATES={sum(int(r["failure_rows"])>0 for r in out)}')
if __name__=='__main__':main()
