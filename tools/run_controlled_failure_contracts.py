import csv,subprocess
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
OUT=ROOT/'outputs/runtime-smoke-controlled'; OUT.mkdir(exist_ok=True)
def main():
 cases=[('python','bad_index.py','xs=[1]\nprint(xs[3])',['python']),('python','bad_syntax.py','def f(:\n  pass',['python']),('go','bad_type.go','package main\nfunc main(){var x int = "x"}',['go','run']),('rust','bad_type.rs','fn main(){let x:i32="x"; println!("{}",x);}',['rustc'])]
 rows=[]
 for lang,name,code,tool in cases:
  p=OUT/name; p.write_text(code,encoding='utf-8')
  cmd=tool+[str(p)] if lang!='rust' else ['rustc',str(p),'-o',str(p.with_suffix('.exe'))]
  try: cp=subprocess.run(cmd,capture_output=True,text=True,timeout=30)
  except subprocess.TimeoutExpired: rows.append({'language':lang,'case':name,'failure_class':'TIMEOUT','diagnostic':'timeout'}); continue
  diag=(cp.stderr or cp.stdout or '').strip().replace('\n',' ')[:1000]
  rows.append({'language':lang,'case':name,'failure_class':'COMPILE_OR_RUNTIME_ERROR' if cp.returncode else 'PASS','diagnostic':diag})
 with (OUT/'runtime_failure_contract_matrix.csv').open('w',encoding='utf-8',newline='') as f:
  w=csv.DictWriter(f,fieldnames=['language','case','failure_class','diagnostic']); w.writeheader(); w.writerows(rows)
 print(f'FAILURE_FIXTURES={len(rows)}'); print(f'EXPECTED_FAILURES={sum(r["failure_class"]=="COMPILE_OR_RUNTIME_ERROR" for r in rows)}')
if __name__=='__main__': main()
