import csv, hashlib, os, subprocess, sys
from pathlib import Path
from multiprocessing import Pool
ROOT=Path(__file__).resolve().parents[1]; OUT=Path(os.environ.get('UAST_ALLTOALL_OUT', str(ROOT/'outputs/site-packages-alltoall-current'))); GEN=OUT/'generated'; EXE=ROOT/'dist/CodeTranspiler.exe'
TARGETS=['c','cpp','csharp','go','java','julia','kotlin','nim','python','r','rust','swift','zig']
EXT={'.py':'python','.c':'c','.h':'c','.cpp':'cpp','.cc':'cpp','.rs':'rust','.go':'go','.java':'java','.kt':'kotlin','.kts':'kotlin','.swift':'swift','.nim':'nim','.jl':'julia','.zig':'zig','.cs':'csharp','.r':'r','.R':'r'}
def run(task):
 src,target=task; p=Path(src); lang=EXT.get(p.suffix)
 if not lang:return None
 cid=hashlib.sha1(f'{src}|{target}'.encode()).hexdigest()[:16]; od=GEN/cid; od.mkdir(parents=True,exist_ok=True); out=od/f'{p.stem}.{target}'
 try:
  cp=subprocess.run([str(EXE),'transpile','-source',lang,'-target',target,str(p),'-o',str(out)],capture_output=True,text=True,timeout=30,creationflags=0x08000000)
  status='PASS' if cp.returncode==0 and out.exists() else 'FAIL'
  diag=(cp.stderr or cp.stdout or '').strip().replace('\n',' ')[:1000]
 except subprocess.TimeoutExpired: status='TIMEOUT'; diag='timeout'; out=Path('')
 return {'source_language':lang,'target':target,'source_file':src,'status':status,'diagnostic':diag,'output_file':str(out) if str(out) else ''}
def main():
 manifest=OUT/'generated_manifest.txt' if os.environ.get('UAST_ALLTOALL_RETRANSPILE')=='1' and (OUT/'generated_manifest.txt').exists() else OUT/'source_manifest.txt'
 files=[x.strip() for x in manifest.read_text(encoding='utf-8').splitlines() if x.strip()]
 result=OUT/'all_to_all_results.csv'; first=not result.exists()
 completed=set()
 if not first:
  try:
   with result.open(encoding='utf-8-sig',newline='') as rf:
    completed={(r.get('source_file',''),r.get('target','')) for r in csv.DictReader(rf)}
  except OSError: pass
 tasks=((f,t) for f in files for t in TARGETS if (f,t) not in completed)
 with result.open('a',encoding='utf-8',newline='') as fh:
  w=csv.DictWriter(fh,fieldnames=['source_language','target','source_file','status','diagnostic','output_file'])
  if first:w.writeheader();fh.flush()
  done=0
  with Pool(6) as pool:
   for row in pool.imap_unordered(run,tasks,chunksize=1):
    if row is None:continue
    w.writerow(row);fh.flush();done+=1
    if done%100==0: print(f'PROCESSED={done}',flush=True)
 print(f'COMPLETE={done}')
 # A subsequent invocation can consume the generated tree as a new source
 # corpus. Keep rounds separate so every generated artifact is traceable.
 if os.environ.get('UAST_ALLTOALL_RETRANSPILE') == '1':
  next_manifest=OUT/'generated_manifest.txt'
  outputs=[str(p) for p in GEN.rglob('*') if p.is_file() and p.suffix in EXT]
  next_manifest.write_text('\n'.join(outputs),encoding='utf-8')
  print(f'GENERATED_MANIFEST={len(outputs)}')
if __name__=='__main__':main()
