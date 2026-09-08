import csv
from pathlib import Path
from collections import Counter
ROOT=Path(__file__).resolve().parents[1]; OUT=ROOT/'outputs/partial-unified-matrix'; OUT.mkdir(parents=True,exist_ok=True)
RUN=Path(r'C:\Users\tarek\Desktop\uast-ecosystem-miner-v4.3-direct-first-fixpoint\run_20260903_143421')
def read(p):
 try:
  with p.open(encoding='utf-8-sig',newline='') as f:return list(csv.DictReader(f))
 except (OSError,FileNotFoundError): return []
def main():
 rows=[]
 for p in RUN.glob('*/results/*.csv'):
  for r in read(p): rows.append({'source':'miner-partial','language':r.get('source_language',r.get('language','')),'target':r.get('target',''),'construct':r.get('source_file',r.get('artifact','')),'primitive':'','status':r.get('status',r.get('result','')),'signature':r.get('failure_signature',r.get('diagnostic',''))})
 files=[('miner',ROOT/'outputs/miner-v4.3-all-to-all/failure_matrix.csv'),('site',ROOT/'outputs/site-packages-alltoall-matrix/failure_matrix.csv'),('compiler',ROOT/'outputs/compiler-semantic-oracle/compiler_ir_semantic_matrix.csv'),('primitive',ROOT/'outputs/unmapped-oracle-evidence/empirical_primitive_matrix.csv')]
 files += [('site-round1',ROOT/'outputs/site-packages-alltoall-current/all_to_all_results.csv'),('site-round2',ROOT/'outputs/site-packages-alltoall-round2/all_to_all_results.csv')]
 files += [('runtime-contracts',ROOT/'outputs/runtime-smoke-controlled/runtime_contract_matrix.csv'),('runtime-failures',ROOT/'outputs/runtime-smoke-controlled/runtime_failure_primitive_matrix.csv')]
 for source,p in files:
  for r in read(p): rows.append({'source':source,'language':r.get('source_language',r.get('language','')),'target':r.get('target',r.get('target_language','')),'construct':r.get('compiler_node',r.get('case_id',r.get('construct',''))),'primitive':r.get('primitive_candidate',r.get('canonical_primitive',r.get('primitive',''))),'status':r.get('status',r.get('failure_kind',r.get('evidence_status',''))),'signature':r.get('failure_signature',r.get('evidence',r.get('confidence','')))})
 fields=['source','language','target','construct','primitive','status','signature']
 with (OUT/'partial_unified_matrix.csv').open('w',encoding='utf-8',newline='') as f:w=csv.DictWriter(f,fieldnames=fields);w.writeheader();w.writerows(rows)
 sig=Counter((r['source'],r['language'],r['target'],r['primitive'],r['status'],r['signature']) for r in rows)
 with (OUT/'partial_exact_classes.csv').open('w',encoding='utf-8',newline='') as f:
  fs=['class_id','source','language','target','primitive','status','signature','rows'];w=csv.DictWriter(f,fieldnames=fs);w.writeheader()
  for i,(k,n) in enumerate(sorted(sig.items()),1):w.writerow({'class_id':f'P{i:06d}','source':k[0],'language':k[1],'target':k[2],'primitive':k[3],'status':k[4],'signature':k[5],'rows':n})
 print(f'PARTIAL_UNIFIED_ROWS={len(rows)}');print(f'PARTIAL_EXACT_CLASSES={len(sig)}');print(f'PARTIAL_SOURCES={len({r["source"] for r in rows})}')
if __name__=='__main__':main()
