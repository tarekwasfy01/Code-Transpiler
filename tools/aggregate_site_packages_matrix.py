import csv,glob,os,collections
root=os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
dirs=sorted(glob.glob(os.path.join(root,'outputs','site-packages-alltoall-shard-*')))
out=os.path.join(root,'outputs','site-packages-alltoall-matrix'); os.makedirs(out,exist_ok=True)
errors=[]; successes=[]
for d in dirs:
    for name,dst in [('transpile_error_matrix.csv',errors),('transpile_success_matrix.csv',successes)]:
        p=os.path.join(d,name)
        if os.path.exists(p): dst.extend(csv.DictReader(open(p,encoding='utf-8',newline='')))
def write(name,fields,rows):
    with open(os.path.join(out,name),'w',newline='',encoding='utf-8') as f:
        w=csv.DictWriter(f,fieldnames=fields); w.writeheader(); w.writerows(rows)
ec=collections.Counter((r['source_language'],r['target'],r['failure_signature']) for r in errors)
rows=[{'source_language':a,'target':b,'failure_signature':c,'occurrences':n} for (a,b,c),n in sorted(ec.items())]
write('failure_matrix.csv',['source_language','target','failure_signature','occurrences'],rows)
sc=collections.Counter((r['source_language'],r['target']) for r in successes)
write('success_matrix.csv',['source_language','target','occurrences'],[{'source_language':a,'target':b,'occurrences':n} for (a,b),n in sorted(sc.items())])
write('exact_failure_row_classes.csv',['row_class','source_language','target','failure_signature','occurrences'],[{'row_class':'ROW_%04d'%i,'source_language':a,'target':b,'failure_signature':c,'occurrences':n} for i,((a,b,c),n) in enumerate(sorted(ec.items()),1)])
cols=collections.Counter(r['failure_signature'] for r in errors)
write('exact_failure_column_classes.csv',['column_class','failure_signature','occurrences'],[{'column_class':'COL_%04d'%i,'failure_signature':s,'occurrences':n} for i,(s,n) in enumerate(cols.most_common(),1)])
primitive_rows=[
    {'primitive':'frontend_facts_to_uast','handler':'BuildCanonicalUniversalASTFromFrontendFacts','status':'IMPLEMENTED'},
    {'primitive':'generic_target_projection','handler':'UniversalTargetProjector','status':'IMPLEMENTED'},
    {'primitive':'runtime_last_resort','handler':'Compatibility/Runtime fallback','status':'IMPLEMENTED'},
]
mapping=os.path.join(root,'outputs','miner-v4.3-all-to-all','primitive_handler_mapping.csv')
if os.path.exists(mapping):
    primitive_rows.extend({'primitive':r['primitive'],'handler':r['handler_file']+'::'+r['handler_symbol'],'status':r['status']} for r in csv.DictReader(open(mapping,encoding='utf-8',newline='')) if not r['primitive'].endswith('_PRIMITIVE'))
write('primitive_coverage.csv',['primitive','handler','status'],primitive_rows)
langs=sorted({r['source_language'] for r in errors+successes}); targets=sorted({r['target'] for r in errors+successes})
print('SHARDS=%d ERROR_ROWS=%d EXACT_ROW_CLASSES=%d FAILURE_SIGNATURES=%d SUCCESS_ROWS=%d LANGUAGES=%d TARGETS=%d' % (len(dirs),sum(int(x['occurrences']) for x in rows),len(rows),len(cols),sum(int(x['occurrences']) for x in successes),len(langs),len(targets)))
