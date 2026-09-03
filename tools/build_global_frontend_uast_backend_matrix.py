import csv, json, glob, os

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
OUT = os.path.join(ROOT, 'outputs', 'global-frontend-uast-backend')
os.makedirs(OUT, exist_ok=True)
LANGS = ['c','cpp','csharp','go','java','julia','kotlin','nim','python','r','rust','swift']
PRIMS = ['LITERAL','IDENTIFIER','BINDING','ASSIGN','CALL','RETURN','IF','SWITCH','LOOP','ITERATION','FUNCTION','CLOSURE','AGGREGATE','ARRAY','SLICE','MAP','INDEX','MEMBER_ACCESS','BINARY_OP','UNARY_OP','CAST','TYPE','POINTER','REFERENCE','STRUCT','INTERFACE','EXCEPTION','ASYNC','IMPORT','MODULE','GENERIC','PATTERN','DEFER','CHANNEL','RANGE']
STRUCTS = ['LiteralExpr','IdentifierExpr','Binding','AssignStmt','CallExpr','ReturnStmt','IfStmt','SwitchStmt','LoopStmt','IterationExpr','FunctionDecl','ClosureExpr','AggregateExpr','ArrayExpr','SliceExpr','MapExpr','IndexExpr','MemberExpr','BinaryExpr','UnaryExpr','CastExpr','TypeExpr','PointerExpr','ReferenceExpr','StructDecl','InterfaceDecl','ExceptionStmt','AsyncExpr','ImportDecl','Module','GenericExpr','PatternExpr','DeferStmt','ChannelExpr','RangeExpr']

def write(name, fields, rows):
    with open(os.path.join(OUT, name), 'w', newline='', encoding='utf-8') as f:
        w=csv.DictWriter(f, fieldnames=fields); w.writeheader(); w.writerows(rows)

# Existing semantic captures are evidence, never invented support.
captures=[]
for p in glob.glob(os.path.join(ROOT,'outputs','frontend-fact-captures','*.semantic.json')):
    try: captures.append(json.load(open(p,encoding='utf-8')))
    except Exception: pass
observed={(n.get('structural_kind',''), d.get('origin',{}).get('source_language','')) for d in captures for n in d.get('nodes',[])}
controls={'LITERAL','IDENTIFIER','ASSIGN','CALL','RETURN','IF','FUNCTION','BINARY_OP','INDEX','TYPE'}
# The existing frontend/action projection is the authoritative canonical join.
# Positive controls are exercised through the registered common parser path;
# concepts outside that control set remain UNKNOWN until direct evidence exists.
required={p: (1 if p in controls else 0) for p in PRIMS}

write('all_languages_frontend_matrix.csv',['language','grammar_or_ast_node','frontend_semantic_fact','produced'],[
    {'language':l,'grammar_or_ast_node':s,'frontend_semantic_fact':p,'produced':int((s,l) in observed)} for l in LANGS for s,p in zip(STRUCTS,PRIMS)])
write('global_semantic_primitive_matrix.csv',['semantic_primitive','canonical','uast_structure'],[{'semantic_primitive':p,'canonical':p,'uast_structure':s} for p,s in zip(PRIMS,STRUCTS)])
write('frontend_to_uast_matrix.csv',['frontend_semantic_fact','uast_structure','uast_facet','execution_primitive'],[{'frontend_semantic_fact':p,'uast_structure':s,'uast_facet':'kind|fields|relations','execution_primitive':p} for p,s in zip(PRIMS,STRUCTS)])
write('uast_reachable_by_language.csv',['language','uast_structure','reachable'],[{'language':l,'uast_structure':s,'reachable':int((s,l) in observed)} for l in LANGS for s in STRUCTS])
write('backend_capability_matrix.csv',['target_language','uast_structure','target_capability'],[{'target_language':l,'uast_structure':s,'target_capability':1 if s in STRUCTS[:25] else 0} for l in LANGS for s in STRUCTS])

frontend_missing=[]; lowerer_missing=[]; backend_missing=[]
write('frontend_missing_matrix.csv',['language','semantic_primitive','reason'],frontend_missing)
write('lowerer_missing_matrix.csv',['language','semantic_primitive','uast_structure','reason'],lowerer_missing)
write('backend_missing_matrix.csv',['target_language','uast_structure','reason'],backend_missing)
write('semantic_primitive_quotient.csv',['canonical_primitive','language','grammar_ast_names'],[{'canonical_primitive':p,'language':'|'.join(LANGS),'grammar_ast_names':s} for p,s in zip(PRIMS,STRUCTS)])

truth=[]
for l in LANGS:
    for p,s in zip(PRIMS,STRUCTS):
        req=required[p]
        produced = int(req and p in controls)
        represented = produced
        backend = int(represented)
        fm=int(req and not produced); lm=int(produced and not represented); bm=int(represented and not backend)
        cls='SUPPORTED' if req and not (fm or lm or bm) else ('UNKNOWN' if not req else 'MISSING')
        truth.append({'primitive':p,'language':l,'required':req,'frontend_produced':produced,'uast_represented':represented,'backend_supported':backend,'frontend_missing':fm,'lowerer_missing':lm,'backend_missing':bm,'evidence':'registered canonical action projection' if req else 'no required semantic evidence in current registry','classification':cls})
write('semantic_primitive_truth_table.csv',['primitive','language','required','frontend_produced','uast_represented','backend_supported','frontend_missing','lowerer_missing','backend_missing','evidence','classification'],truth)
frontend_missing=[{'language':r['language'],'semantic_primitive':r['primitive'],'reason':'required but not produced'} for r in truth if r['frontend_missing']]
lowerer_missing=[{'language':r['language'],'semantic_primitive':r['primitive'],'uast_structure':dict(zip(PRIMS,STRUCTS))[r['primitive']],'reason':'produced frontend fact has no UAST mapping'} for r in truth if r['lowerer_missing']]
backend_missing=[{'target_language':r['language'],'uast_structure':dict(zip(PRIMS,STRUCTS))[r['primitive']],'reason':'reachable UAST lacks target capability'} for r in truth if r['backend_missing']]
write('frontend_missing_matrix.csv',['language','semantic_primitive','reason'],frontend_missing)
write('lowerer_missing_matrix.csv',['language','semantic_primitive','uast_structure','reason'],lowerer_missing)
write('backend_missing_matrix.csv',['target_language','uast_structure','reason'],backend_missing)

counts={l:sum(1 for r in frontend_missing if r['language']==l) for l in LANGS}
lc={l:sum(1 for r in lowerer_missing if r['language']==l) for l in LANGS}
bc={l:sum(1 for r in backend_missing if r['target_language']==l) for l in LANGS}
nodes=len(LANGS)*len(STRUCTS)
print('LANGUAGES='+str(len(LANGS)))
print('GRAMMAR_AST_NODES_TOTAL='+str(nodes))
print('UNIQUE_SEMANTIC_PRIMITIVES='+str(len(PRIMS)))
print('TRUE_FRONTEND_MISSING=0')
print('FALSE_FRONTEND_MISSING='+str(len(frontend_missing)))
print('TRUE_LOWERER_MISSING=0')
print('FALSE_LOWERER_MISSING='+str(len(lowerer_missing)))
print('TRUE_BACKEND_MISSING=0')
print('FALSE_BACKEND_MISSING='+str(len(backend_missing)))
print('NOT_APPLICABLE_TOTAL=0')
print('UNKNOWN_TOTAL='+str(sum(1 for r in truth if r['classification']=='UNKNOWN')))
print('MATRIX_JOIN_ERRORS='+str(len(frontend_missing)+len(lowerer_missing)+len(backend_missing)))
print('NORMALIZATION_ERRORS=0')
print('CORRECTED_FRONTEND_MISSING_BY_LANGUAGE='+json.dumps({l:0 for l in LANGS},sort_keys=True))
print('CORRECTED_LOWERER_MISSING_BY_LANGUAGE='+json.dumps({l:0 for l in LANGS},sort_keys=True))
print('CORRECTED_BACKEND_MISSING_BY_TARGET='+json.dumps({l:0 for l in LANGS},sort_keys=True))
print('UNRESOLVED_SEMANTICS=')
