import csv
from collections import Counter, defaultdict
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SRC = ROOT / 'outputs/unified-route-evidence/large_route_evidence_matrix.csv'
OUT = ROOT / 'outputs/unified-route-evidence'

# Existing productive contracts. This is an evidence-to-handler mapping, not a
# second IR or registry; parser machine atoms are intentionally kept separate.
SEMANTIC = {
    'AGGREGATE':'data', 'INDEX_READ':'data', 'INDEX_SLICE':'data', 'ITERATION':'control',
    'LOOP':'control', 'CONDITIONAL':'control', 'FUNCTION_VALUE':'capture',
    'FUNCTION_DECL':'declaration', 'CALL':'call', 'RETURN':'control',
    'BINARY_OPERATOR':'expression', 'COMPARISON':'expression', 'UNARY_OPERATOR':'expression',
    'ASSIGNMENT':'binding', 'LITERAL':'expression', 'SYMBOL':'binding', 'SELECTOR':'data',
    'BINARY_UNARY_OPERATOR':'expression', 'CONTROL_TRANSFER':'control', 'TYPE':'types',
    'CONCURRENCY':'concurrency', 'CLOSURE_FUNCTION':'capture', 'CONVERSION':'conversion',
    'SWITCH_MATCH':'control', 'EXCEPTION':'exception', 'DEALLOCATION':'lifetime',
    'MEMBER_ACCESS':'data', 'ALLOCATION':'memory', 'BINDING_DECLARATION':'declaration',
    'BINDING_REFERENCE':'binding', 'LIFETIME':'lifetime', 'COMPILETIME':'compiletime',
    'MODULE':'module', 'MEMORY':'memory', 'ASSERTION':'control', 'IR_BINARY_OPERATION':'expression',
    'IR_CONVERSION':'conversion', 'IR_CONTROL_TRANSFER':'control', 'ANNOTATION':'annotation',
    'FFI_ABI':'abi', 'ASSERTION':'control',
}
HANDLER_ALIASES = {
    'execABI':'abi','execAnnotation':'annotation','execBinding':'binding','execCall':'call',
    'execCapture':'capture','execCompileTime':'compiletime','execConcurrency':'concurrency',
    'execControl':'control','execConversion':'conversion','execData':'data',
    'execDeclaration':'declaration','execException':'exception','execExpression':'expression',
    'execLifetime':'lifetime','execMemory':'memory','execModule':'module','execTypes':'types',
    'SEMANTIC_FACT_PRIMITIVE':'validation','UAST_PRIMITIVE':'syntax',
    'TYPE_PRIMITIVE':'types','CONTROL_FLOW_PRIMITIVE':'control',
    'BACKEND_PROJECTION_PRIMITIVE':'lowering','CODEGEN_PRIMITIVE':'lowering',
    'TARGET_TOOLCHAIN_PRIMITIVE':'validation','COMPILER_IMPLEMENTATION_DETAIL':'validation',
}
PARSER_ATOMS = {
    'LEX_TRANSITION','LEX_ACCEPT','LEX_MODE_SELECT','EXTERNAL_SCAN','EXTERNAL_TOKEN_MAP',
    'LOOKAHEAD_READ','ACTION_LOOKUP','SHIFT','SHIFT_EXTRA','REDUCE','GOTO','ACCEPT','RECOVER',
    'STACK_PUSH','STACK_POP','STACK_FORK','STACK_MERGE','STATE_SET','STATE_RESTORE',
    'SYMBOL_CONSUME','EOF_HANDLE',
}

def split(v):
    for x in (v or '').replace('|',';').replace(',',';').split(';'):
        x=x.strip()
        if x: yield x

def main():
    rows=[]; seen=Counter(); sources=defaultdict(set)
    with SRC.open(encoding='utf-8-sig', newline='') as f:
        for r in csv.DictReader(f):
            for name in split(r.get('primitive','')):
                seen[name]+=1; sources[name].add(r.get('source',''))
    for name,n in sorted(seen.items()):
        if name in SEMANTIC:
            handler=SEMANTIC[name]; kind='SEMANTIC'; status='LINKED_EXISTING_HANDLER'
        elif name in HANDLER_ALIASES:
            handler=HANDLER_ALIASES[name]; kind='SEMANTIC_ALIAS'; status='LINKED_EXISTING_HANDLER'
        elif name in PARSER_ATOMS:
            handler=name.lower(); kind='PARSER_ATOMIC'; status='EXISTING_PRODUCTIVE_OPERATION'
        else:
            handler=''; kind='EVIDENCE_ONLY'; status='UNRESOLVED'
        rows.append({'primitive':name,'kind':kind,'handler':handler,'status':status,'rows':n,'sources':len(sources[name])})
    with (OUT/'large_atomic_primitive_matrix.csv').open('w',encoding='utf-8',newline='') as f:
        w=csv.DictWriter(f,fieldnames=['primitive','kind','handler','status','rows','sources']); w.writeheader(); w.writerows(rows)
    linked=sum(r['status']=='LINKED_EXISTING_HANDLER' for r in rows)
    parser=sum(r['kind']=='PARSER_ATOMIC' for r in rows)
    unresolved=sum(r['status']=='UNRESOLVED' for r in rows)
    with (OUT/'large_atomic_primitive_summary.csv').open('w',encoding='utf-8',newline='') as f:
        w=csv.DictWriter(f,fieldnames=['metric','value']); w.writeheader()
        for k,v in [('UNIQUE_ATOMIC_OR_OBSERVED_PRIMITIVES',len(rows)),('LINKED_EXISTING_HANDLERS',linked),('PARSER_ATOMIC_OPERATIONS',parser),('UNRESOLVED_EVIDENCE_ONLY',unresolved)]: w.writerow({'metric':k,'value':v})
    print(f'UNIQUE_PRIMITIVES={len(rows)} LINKED_EXISTING_HANDLERS={linked} PARSER_ATOMIC_OPERATIONS={parser} UNRESOLVED_EVIDENCE_ONLY={unresolved}')

if __name__=='__main__': main()
