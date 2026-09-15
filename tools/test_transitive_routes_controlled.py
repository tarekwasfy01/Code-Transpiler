import csv,subprocess
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
OUT=ROOT/'outputs/runtime-smoke-controlled'; EXE=ROOT/'dist/CodeTranspiler.exe'
SOURCES={'python':OUT/'fixture_trace.py','go':OUT/'fixture_trace.go','rust':OUT/'fixture_trace.rs'}
TARGETS=['r','go','rust','cpp','c','python','zig','julia','nim','csharp','java','kotlin','swift']
def trans(src,sl,t,tag):
 out=OUT/f'route_{tag}_{t}'
 try:
  cp=subprocess.run([str(EXE),'transpile','-source',sl,'-target',t,str(src),'-o',str(out)],capture_output=True,text=True,timeout=30)
  return cp.returncode==0 and out.exists(),out,(cp.stderr or cp.stdout or '').strip()[:300]
 except subprocess.TimeoutExpired:return False,out,'timeout'
def main():
 rows=[]
 for sl,src in SOURCES.items():
  if not src.exists():
   rows.append({'source_language':sl,'route':f'{sl}->*','status':'NOT_AVAILABLE','diagnostic':f'missing fixture: {src.name}'})
   continue
  for t in TARGETS:
   ok,out,d=trans(src,sl,t,f'{sl}_direct'); rows.append({'source_language':sl,'route':f'{sl}->{t}','status':'PASS' if ok else 'FAIL','diagnostic':d})
   if t==sl: continue
   ok1,mid,d1=trans(src,sl,t,f'{sl}_via'); ok2=False; d2=''
   if ok1: ok2,_,d2=trans(mid,t,t,'identity')
   rows.append({'source_language':sl,'route':f'{sl}->{t}->{t}','status':'PASS' if ok1 and ok2 else 'FAIL','diagnostic':d1 or d2})
 with (OUT/'transitive_route_matrix.csv').open('w',encoding='utf-8',newline='') as f:
  w=csv.DictWriter(f,fieldnames=['source_language','route','status','diagnostic']); w.writeheader(); w.writerows(rows)
 print(f'ROUTES={len(rows)}'); print(f'PASS={sum(r["status"]=="PASS" for r in rows)}'); print(f'FAIL={sum(r["status"]=="FAIL" for r in rows)}')
if __name__=='__main__': main()
