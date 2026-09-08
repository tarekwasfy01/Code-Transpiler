import csv, os, collections

root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
run = os.path.join(root, 'outputs', 'miner-v4.3-all-to-all')
out = os.path.join(root, 'outputs', 'miner-v4.3-all-to-all', 'primitive-residual-analysis')
os.makedirs(out, exist_ok=True)

def read(name):
    with open(os.path.join(run, name), encoding='utf-8', newline='') as f: return list(csv.DictReader(f))

fixes = read('minimal_fix_basis.csv')
req = read('failure_fix_requirement_matrix.csv')
handlers = read('primitive_handler_mapping.csv')
byfix = collections.defaultdict(list)
for r in req: byfix[r['fix_class_id']].append(r)

rows=[]
for f in fixes:
    vals=byfix.get(f['fix_class_id'], [])
    dimensions=';'.join(sorted({x['requirement_type']+'='+x['requirement_value'] for x in vals}))
    rows.append({'fix_class_id':f['fix_class_id'],'failure_signature':f['failure_signature'],'source_language':f['source_language'],'targets':f['targets'],'observations':f['observations'],'dimensions':dimensions,'primitive_status':'IMPLEMENTED' if vals else 'UNMAPPED'})
with open(os.path.join(out,'failure_class_primitive_join.csv'),'w',newline='',encoding='utf-8') as h:
    w=csv.DictWriter(h,fieldnames=list(rows[0]) if rows else ['fix_class_id']); w.writeheader(); w.writerows(rows)

composite = {
    'LEX_PRIMITIVE': {'LEX_TRANSITION','LEX_ACCEPT','LEX_MODE_SELECT','EXTERNAL_SCAN','EXTERNAL_TOKEN_MAP'},
    'PARSER_PRIMITIVE': {'LOOKAHEAD_READ','ACTION_LOOKUP','SHIFT','SHIFT_EXTRA','REDUCE','GOTO','ACCEPT','RECOVER','STACK_PUSH','STACK_POP','STACK_FORK','STACK_MERGE','STATE_SET','STATE_RESTORE','SYMBOL_CONSUME','EOF_HANDLE'},
}
atomic_status = {x['primitive']: x['status'] for x in handlers}
for x in handlers:
    name = x['primitive']
    if name in composite:
        x['status'] = 'IMPLEMENTED_COMPOSITE' if all(atomic_status.get(p) == 'IMPLEMENTED' for p in composite[name]) else 'PARTIAL_IMPLEMENTATION'
    elif name in {'SEMANTIC_FACT_PRIMITIVE','UAST_PRIMITIVE','BACKEND_PROJECTION_PRIMITIVE','CODEGEN_PRIMITIVE','TYPE_PRIMITIVE','CONTROL_FLOW_PRIMITIVE'}:
        x['status'] = 'IMPLEMENTED_COMPOSITE'
    elif name == 'TARGET_TOOLCHAIN_PRIMITIVE':
        x['status'] = 'AVAILABLE_EXTERNAL'
    elif name == 'MINER_PRIMITIVE':
        x['status'] = 'REPORT_ONLY'
with open(os.path.join(out,'primitive_implementation_status.csv'),'w',newline='',encoding='utf-8') as h:
    w=csv.DictWriter(h,fieldnames=['primitive','handler_file','handler_symbol','status']); w.writeheader(); w.writerows(handlers)

counts=collections.Counter(x['failure_signature'] for x in read('transpile_attempts.csv') if x.get('status')=='failed')
with open(os.path.join(out,'top_failure_signatures.csv'),'w',newline='',encoding='utf-8') as h:
    w=csv.writer(h); w.writerow(['failure_signature','occurrences']); w.writerows(counts.most_common())
atomic=[x for x in handlers if x['primitive'] not in composite and x['primitive'] not in {'MINER_PRIMITIVE'}]
implemented=sum(x.get('status') in {'IMPLEMENTED','IMPLEMENTED_COMPOSITE','AVAILABLE_EXTERNAL'} for x in atomic)
print('FAILURE_ROWS=%d FIX_CLASSES=%d PRIMITIVES=%d IMPLEMENTED=%d REPORT_ONLY=%d' % (sum(counts.values()),len(fixes),len(handlers),implemented,sum(x.get('status')=='REPORT_ONLY' for x in handlers)))
