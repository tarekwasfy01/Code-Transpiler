import csv,re,hashlib
from pathlib import Path
from collections import defaultdict
ROOT=Path(__file__).resolve().parents[1]; OUT=ROOT/'outputs/partial-unified-matrix'
SRC=OUT/'partial_unified_matrix.csv'
def norm(s):
 s=(s or '').lower(); s=re.sub(r'0x[0-9a-f]+|\d+','<n>',s); s=re.sub(r'\s+',' ',s); return s[:500]
def main():
 rows=list(csv.DictReader(SRC.open(encoding='utf-8-sig'))); groups=defaultdict(list)
 for r in rows:
  sig=norm(r.get('signature',''))
  if not sig: continue
  groups[sig].append(r)
 out=[]
 for sig,items in groups.items():
  files={r.get('construct','') for r in items if r.get('construct','')}; langs={r.get('language','') for r in items if r.get('language','')}; targets={r.get('target','') for r in items if r.get('target','')}
  # Repetition across independent source files is the empirical criterion;
  # target coverage is recorded but not required because failure rows often
  # intentionally omit a target after frontend rejection.
  proven=len(files)>=3 and len(langs)>=1
  primitives=sorted({r.get('primitive','') for r in items if r.get('primitive','') and r.get('primitive','')!='UNMAPPED'})
  if not primitives:
   if any(k in sig for k in ('aload','astore','dload','dstore','fload','fstore','iload','istore','lload','lstore')): primitives=['BINDING_REFERENCE']
   elif any(k in sig for k in ('const','lit','none')): primitives=['LITERAL']
  out.append({'contract_id':hashlib.sha1(sig.encode()).hexdigest()[:16],'signature':sig,'observations':len(items),'independent_files':len(files),'languages':len(langs),'targets':len(targets),'primitive_candidates':';'.join(primitives),'status':'EMPIRICALLY_PROVEN' if proven else 'UNPROVEN'})
 with (OUT/'empirical_contract_threshold_matrix.csv').open('w',encoding='utf-8',newline='') as f:
  w=csv.DictWriter(f,fieldnames=list(out[0]) if out else ['contract_id']);w.writeheader();w.writerows(out)
 print(f'NORMALIZED_CONTRACTS={len(out)}'); print(f'EMPIRICALLY_PROVEN_CONTRACTS={sum(r["status"]=="EMPIRICALLY_PROVEN" for r in out)}'); print(f'UNPROVEN_CONTRACTS={sum(r["status"]=="UNPROVEN" for r in out)}')
if __name__=='__main__':main()
