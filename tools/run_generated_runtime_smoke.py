import csv,subprocess,os
from pathlib import Path
from multiprocessing import Pool
ROOT=Path(__file__).resolve().parents[1]; OUT=ROOT/'outputs/runtime-smoke'; OUT.mkdir(parents=True,exist_ok=True)
FILES=[p for base in (ROOT/'outputs/site-packages-alltoall-current/generated',ROOT/'outputs/site-packages-alltoall-round2/generated') for p in base.rglob('*') if p.is_file()]
def run(p):
 t=p.suffix.lstrip('.').lower(); cmd=None
 if t in ('py','python'): cmd=['python',str(p)]
 elif t=='go': cmd=['go','run',str(p)]
 elif t=='rs': cmd=['rustc',str(p),'-o',str(p.with_suffix('.exe'))]
 else: return [str(p),t,'NOT_AVAILABLE','','']
 try:
  cp=subprocess.run(cmd,capture_output=True,text=True,timeout=30,cwd=str(p.parent)); st='PASS' if cp.returncode==0 else 'FAIL'; d=(cp.stderr or cp.stdout or '').strip()[:500]
 except subprocess.TimeoutExpired: st='TIMEOUT'; d='timeout'
 return [str(p),t,st,str(cp.returncode) if 'cp' in locals() else '',d]
def main():
 out=OUT/'runtime_results.csv'; first=not out.exists()
 with out.open('a',encoding='utf-8',newline='') as f:
  w=csv.writer(f)
  if first:w.writerow(['file','target','status','exit_code','diagnostic'])
  with Pool(6) as pool:
   for row in pool.imap_unordered(run,FILES,chunksize=1):w.writerow(row);f.flush()
 print(f'FILES={len(FILES)}')
if __name__=='__main__':main()
