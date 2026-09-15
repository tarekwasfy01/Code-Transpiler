import csv,json,zipfile,xml.etree.ElementTree as ET
from pathlib import Path
from collections import defaultdict
ROOT=Path(__file__).resolve().parents[1]; OUT=ROOT/'outputs/miner-cross-transpile/external_harvester_crosswalk'; OUT.mkdir(parents=True,exist_ok=True)
NS='{http://schemas.openxmlformats.org/spreadsheetml/2006/main}'
def xrows(path):
 z=zipfile.ZipFile(path); sr=ET.fromstring(z.read('xl/sharedStrings.xml')); ss=[''.join(t.text or '' for t in x.iter(NS+'t')) for x in sr]; wb=ET.fromstring(z.read('xl/workbook.xml')); names=[s.attrib['name'] for s in wb.findall('.//'+NS+'sheet')]; out={}
 for i,n in enumerate(names,1):
  root=ET.fromstring(z.read(f'xl/worksheets/sheet{i}.xml')); rr=[]
  for row in root.findall('.//'+NS+'row'):
   a=[]
   for c in row.findall(NS+'c'):
    v=c.find(NS+'v'); x='' if v is None else (v.text or '')
    if c.attrib.get('t')=='s' and x:x=ss[int(x)]
    a.append(x)
   if a:rr.append(a)
  if rr:
   h=rr[0];out[n]=[dict(zip(h,a+['']*max(0,len(h)-len(a)))) for a in rr[1:]]
 return out
def rcsv(p):
 try:
  with p.open(encoding='utf-8-sig',newline='') as f:return list(csv.DictReader(f))
 except:return []
def wcsv(name,rows,fields):
 with (OUT/name).open('w',encoding='utf-8',newline='') as f:
  w=csv.DictWriter(f,fieldnames=fields,extrasaction='ignore');w.writeheader();w.writerows(rows)
book=xrows(Path(r'F:/download/universal_primitive_evidence_harvester_matrix_v2.xlsx')); ev=book.get('Evidence Rows',[]); rules=book.get('Equivalence Rules',[]); kernels=book.get('Kernel Family Matrix',[]); targets=book.get('Target Basis Evidence',[]); projects=book.get('Source Projects',[])
fails=rcsv(ROOT/'outputs/miner-cross-transpile/failure_tensor.csv'); groups=defaultdict(list)
for r in fails:groups[(r.get('first_failure_stage','UNKNOWN'),r.get('failure_class','UNKNOWN'),r.get('diagnostic','UNKNOWN'))].append(r)
res=[{'residual_id':f'R{i:05d}','failure_stage':k[0],'failure_class':k[1],'diagnostic':k[2],'cases':len(v),'fingerprint':'|'.join(k)} for i,(k,v) in enumerate(groups.items(),1)]
ext=[{'external_id':e.get('evidence_id',''),'normalized_primitive':e.get('normalized_primitive',''),'kernel_class':e.get('kernel_class',''),'candidate_class':e.get('candidate_class',''),'semantic_shape':e.get('source_semantics',''),'arity':e.get('arity',''),'proof_state':e.get('proof_state',''),'evidence_kind':e.get('evidence_kind',''),'source_project':e.get('source_project',''),'target_language':e.get('target_language','')} for e in ev]
wcsv('external_evidence_inventory.csv',ext,list(ext[0]) if ext else ['external_id']); matches=[]
for r in res:
 text=(r['failure_class']+' '+r['diagnostic']).lower()
 for e in ext:
  p=e['normalized_primitive'].lower().replace('_',' '); sh=e['semantic_shape'].lower(); tokens=[x for x in p.split() if len(x)>2]
  if tokens and not any(x in text for x in tokens) and not any(x in text for x in ('compare','literal','symbol','logical') if x in sh):continue
  if not tokens:continue
  strong=e['proof_state'] in ('EXACT_CODE','OPTIMIZER_TESTED','CODE_WITH_TEST_FIXTURES')
  matches.append({'residual_id':r['residual_id'],'external_id':e['external_id'],'semantic_match':'EXACT_SHAPE_MATCH' if strong else 'STRUCTURAL_ONLY_MATCH','proof_state':e['proof_state'],'kernel_class':e['kernel_class'],'target':e['target_language']})
wcsv('unique_semantic_residuals.csv',res,list(res[0]) if res else ['residual_id']); mf=['residual_id','external_id','semantic_match','proof_state','kernel_class','target'];wcsv('residual_external_match_matrix.csv',matches,mf);wcsv('residual_kernel_match_matrix.csv',matches,['residual_id','external_id','kernel_class','semantic_match']);wcsv('external_guard_match_matrix.csv',[],['residual_id','external_id','guard_match']);wcsv('external_target_match_matrix.csv',[],['residual_id','external_id','target_match'])
for n,data in [('parameterized_kernel_candidates.csv',kernels),('derived_recipe_candidates.csv',rules),('equivalence_rule_candidates.csv',rules),('target_terminal_candidates.csv',targets)]:wcsv(n,data,list(data[0]) if data else ['id'])
matched={m['residual_id'] for m in matches};strong=[m for m in matches if m['semantic_match']=='EXACT_SHAPE_MATCH'];wcsv('residuals_with_strong_external_evidence.csv',strong,mf);wcsv('residuals_with_partial_external_evidence.csv',[m for m in matches if m['semantic_match']!='EXACT_SHAPE_MATCH'],mf);wcsv('residuals_without_external_evidence.csv',[r for r in res if r['residual_id'] not in matched],list(res[0]) if res else ['residual_id'])
summary={'harvester_evidence_rows':len(ev),'source_projects':len(projects),'kernel_families':len({e.get('kernel_class','') for e in ev}),'raw_failure_rows':len(fails),'unique_semantic_residuals':len(res),'exact_shape_matches':len(strong),'partial_matches':len(matches)-len(strong),'residuals_without_evidence':len(res)-len(matched),'contract_gaps_checked':len(rcsv(ROOT/'outputs/primitive-contract-closure/remaining_insufficient_evidence.csv'))}
(OUT/'summary.json').write_text(json.dumps(summary,indent=2),encoding='utf-8');print(json.dumps(summary,indent=2))
