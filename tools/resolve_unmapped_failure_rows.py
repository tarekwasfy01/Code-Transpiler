import csv,re
from pathlib import Path
from collections import Counter
ROOT=Path(__file__).resolve().parents[1]; SRC=ROOT/'outputs/partial-unified-matrix/partial_unified_matrix.csv'; OUT=ROOT/'outputs/partial-unified-matrix'
RULES=[('LEXER',r'lex|token|character|unicode'),('PARSER',r'parse|syntax|unexpected|expected'),('UAST',r'uast|semantic|structure|relation|field'),('TYPE',r'type|convert|cast'),('CALL',r'call|invoke|argument|function'),('INDEX',r'index|subscript|slice|bound'),('ITERATION',r'loop|range|iterate|for each'),('BINDING',r'bind|scope|undefined|name'),('OPERATOR',r'operator|binary|unary|add|sub|mul|div'),('TARGET',r'codegen|emit|target|compile')]
def main():
 rows=list(csv.DictReader(SRC.open(encoding='utf-8-sig'))); out=[]; c=Counter()
 for r in rows:
  text=(r.get('signature','')+' '+r.get('construct','')).lower(); hits=[p for p,pat in RULES if re.search(pat,text)]
  stage='UNKNOWN' if not hits else hits[0]; primitive=r.get('primitive','') or ('UNMAPPED' if not hits else hits[0])
  status=r.get('status',''); root='FAILURE' if status not in ('PASS','IMPLEMENTED') else 'PASS'
  c[(stage,primitive,root)]+=1; out.append({**r,'failure_stage':stage,'primitive_candidate':primitive,'root_status':root})
 fields=list(out[0]) if out else []
 with (OUT/'resolved_failure_matrix.csv').open('w',encoding='utf-8',newline='') as f:w=csv.DictWriter(f,fieldnames=fields);w.writeheader();w.writerows(out)
 with (OUT/'resolved_failure_classes.csv').open('w',encoding='utf-8',newline='') as f:
  fs=['class_id','failure_stage','primitive_candidate','root_status','rows'];w=csv.DictWriter(f,fieldnames=fs);w.writeheader()
  for i,((s,p,st),n) in enumerate(sorted(c.items()),1):w.writerow({'class_id':f'RF{i:05d}','failure_stage':s,'primitive_candidate':p,'root_status':st,'rows':n})
 print(f'RESOLVED_ROWS={len(out)}');print(f'EXACT_RESOLVED_CLASSES={len(c)}');print(f'UNKNOWN_STAGE_ROWS={sum(n for (s,p,st),n in c.items() if s=="UNKNOWN")}')
if __name__=='__main__':main()
