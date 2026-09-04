import csv, subprocess
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]; OUT=ROOT/'outputs/runtime-smoke-controlled'; OUT.mkdir(exist_ok=True)
def main():
 srcs={
  'python':('fixture_trace.py','xs=[1,2,3]; print("LIST_CREATE"); y=[v*2 for v in xs]; print("ITERATION",len(y)); print("INDEX_READ",y[1])'),
  'go':('fixture_trace.go','package main\nimport "fmt"\nfunc main(){xs:=[]int{1,2,3}; fmt.Println("LIST_CREATE"); y:=make([]int,len(xs)); for i,v:=range xs { y[i]=v*2 }; fmt.Println("ITERATION",len(y)); fmt.Println("INDEX_READ",y[1])}'),
  'rust':('fixture_trace.rs','fn main(){let xs=vec![1,2,3]; println!("LIST_CREATE"); let y:Vec<i32>=xs.iter().map(|v|v*2).collect(); println!("ITERATION {}",y.len()); println!("INDEX_READ {}",y[1]);}')}
 rows=[]
 for lang,(name,code) in srcs.items():
  p=OUT/name;p.write_text(code,encoding='utf-8');
  if lang=='python': cmd=['python',str(p)]
  elif lang=='go': cmd=['go','run',str(p)]
  else:
   exe=p.with_suffix('.exe'); subprocess.run(['rustc',str(p),'-o',str(exe)],check=True,capture_output=True); cmd=[str(exe)]
  cp=subprocess.run(cmd,capture_output=True,text=True,timeout=30); lines=cp.stdout.splitlines()
  for line in lines:
   parts=line.split(); rows.append({'language':lang,'event':parts[0],'value':' '.join(parts[1:]),'status':'PASS' if cp.returncode==0 else 'FAIL'})
 with (OUT/'runtime_contract_matrix.csv').open('w',encoding='utf-8',newline='') as f:
  w=csv.DictWriter(f,fieldnames=['language','event','value','status']);w.writeheader();w.writerows(rows)
 print(f'RUNTIME_EVENTS={len(rows)}');print(f'RUNTIME_LANGUAGES={len(srcs)}');print(f'RUNTIME_PASS={sum(r["status"]=="PASS" for r in rows)}')
if __name__=='__main__':main()
