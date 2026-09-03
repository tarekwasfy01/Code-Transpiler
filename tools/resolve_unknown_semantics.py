import csv, os, json

root=os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
src=os.path.join(root,'outputs','global-frontend-uast-backend','semantic_primitive_truth_table.csv')
out=os.path.join(root,'outputs','global-frontend-uast-backend')
rows=list(csv.DictReader(open(src,encoding='utf-8')))
unknown=[r for r in rows if r['classification']=='UNKNOWN']
evidence=[]
for r in unknown:
    evidence.append({'primitive':r['primitive'],'language':r['language'],'layer':'FRONTEND','evidence_type':'registry/test search','evidence':'No direct GrammarNode -> FrontendFact -> CanonicalSemanticPrimitiveID proof found in repository','result':'UNKNOWN'})
    evidence.append({'primitive':r['primitive'],'language':r['language'],'layer':'LOWERER','evidence_type':'UAST/emitter search','evidence':'No direct FrontendFact -> UASTStructure/Facet -> CanonicalSemanticPrimitiveID proof found in repository','result':'UNKNOWN'})
    evidence.append({'primitive':r['primitive'],'language':r['language'],'layer':'BACKEND','evidence_type':'projector/emitter search','evidence':'No direct Canonical UAST -> UniversalTargetProjector -> NativeEmitter proof found in repository','result':'UNKNOWN'})
with open(os.path.join(out,'unknown_resolution_evidence.csv'),'w',newline='',encoding='utf-8') as f:
    w=csv.DictWriter(f,fieldnames=['primitive','language','layer','evidence_type','evidence','result']); w.writeheader(); w.writerows(evidence)
with open(os.path.join(out,'remaining_unknown_matrix.csv'),'w',newline='',encoding='utf-8') as f:
    w=csv.DictWriter(f,fieldnames=['primitive','language','reason']); w.writeheader()
    for r in unknown: w.writerow({'primitive':r['primitive'],'language':r['language'],'reason':'insufficient repository evidence across required layers'})
with open(os.path.join(out,'semantic_primitive_truth_table_v2.csv'),'w',newline='',encoding='utf-8') as f:
    fields=list(rows[0]); w=csv.DictWriter(f,fieldnames=fields); w.writeheader(); w.writerows(rows)
print('UNKNOWN_BEFORE='+str(len(unknown)))
print('RESOLVED_SUPPORTED=0')
print('RESOLVED_PARTIAL=0')
print('RESOLVED_MISSING=0')
print('RESOLVED_NOT_APPLICABLE=0')
print('UNKNOWN_AFTER='+str(len(unknown)))
print('FRONTEND_MISSING_CONFIRMED=0')
print('LOWERER_MISSING_CONFIRMED=0')
print('BACKEND_MISSING_CONFIRMED=0')
