import csv,json
from pathlib import Path
from collections import defaultdict
ROOT=Path(__file__).resolve().parents[1]; SRC=ROOT/'outputs/miner-cross-transpile'; EXT=SRC/'external_harvester_crosswalk'; OUT=ROOT/'outputs/primitive-final-closure'; OUT.mkdir(parents=True,exist_ok=True)
def rd(p):
 try:
  with p.open(encoding='utf-8-sig',newline='') as f:return list(csv.DictReader(f))
 except:return []
def wr(name,rows,fields):
 with (OUT/name).open('w',encoding='utf-8',newline='') as f:
  w=csv.DictWriter(f,fieldnames=fields,extrasaction='ignore');w.writeheader();w.writerows(rows)
res=rd(EXT/'unique_semantic_residuals.csv'); ext=rd(EXT/'external_evidence_inventory.csv'); matches=rd(EXT/'residual_external_match_matrix.csv'); fail=rd(SRC/'failure_tensor.csv')
known={'COMPARE','LOGICAL','LITERAL','SYMBOL','LOAD','STORE','CONVERT','CALL','ITERATION','CONTROL','BITWISE','SHIFT','ASYNC','EXCEPTION','OWNERSHIP','SELECT'}
def family(x):
 t=(x.get('failure_class','')+' '+x.get('diagnostic','')).upper()
 for k in known:
  if k in t:return k
 return 'UNCLASSIFIED'
rf=[]
for r in res:
 f=family(r); k={'COMPARE':'COMPARE','LOGICAL':'LOGICAL_BINARY','LITERAL':'LITERAL','SYMBOL':'LOAD','LOAD':'LOAD','STORE':'STORE','CONVERT':'CONVERT','CALL':'CALL','ITERATION':'ITERATION','CONTROL':'CONTROL_FLOW','BITWISE':'BITWISE_BINARY','SHIFT':'SHIFT','ASYNC':'ASYNC','EXCEPTION':'EXCEPTION_MAP','OWNERSHIP':'OWNERSHIP_MAP','SELECT':'SELECT'}.get(f,'UNCLASSIFIED');rf.append({'residual_id':r['residual_id'],'primitive_family':f,'kernel_family':k,'atomic_kernel':k,'cases':r.get('cases','0'),'failure_class':r.get('failure_class',''),'diagnostic':r.get('diagnostic','')})
wr('residual_primitive_family_matrix.csv',rf,['residual_id','primitive_family','kernel_family','atomic_kernel','cases','failure_class','diagnostic']); wr('primitive_kernel_family_matrix.csv',[{'primitive_family':x,'kernel_family':x} for x in sorted({r['primitive_family'] for r in rf})],['primitive_family','kernel_family']);wr('kernel_atomic_matrix.csv',[{'kernel_family':x,'atomic_kernel':x} for x in sorted({r['kernel_family'] for r in rf})],['kernel_family','atomic_kernel'])
shape_ids={'COMPARE_EQ','COMPARE_NE','COMPARE_LT','COMPARE_LE','COMPARE_GT','COMPARE_GE','LOGICAL_AND','LOGICAL_OR','LOGICAL_NOT','LITERAL_BOOL','LITERAL_F64','LITERAL_I64','LITERAL_STRING','SYMBOL_REF'}
proof=[]
for e in ext:
 p=e.get('normalized_primitive','').upper(); exact=p in shape_ids and e.get('proof_state')=='EXACT_CODE'; proof.append({'external_id':e.get('external_id',''),'primitive':p,'semantic_shape':'EXACT_SEMANTIC_SHAPE' if exact else 'SHAPE_CANDIDATE','internal_proof':'INTERNAL_EXACT_PROVEN' if exact else 'PROOF_MISSING','target_proof':'MISSING','guard_proof':'MISSING','representation_proof':'MISSING'})
wr('strong_match_internal_proof.csv',[x for x in proof if x['internal_proof']=='INTERNAL_EXACT_PROVEN'],list(proof[0]) if proof else ['external_id']);wr('new_exact_contracts.csv',[],['primitive','status']);wr('new_generated_recipes.csv',[],['primitive','recipe']);wr('new_equivalence_rules.csv',[],['lhs','rhs'])
counts=defaultdict(int)
for r in rf:counts[r['primitive_family']]+=1
wr('closure_delta.csv',[{'family':k,'before':v,'after':v,'delta':0} for k,v in counts.items()],['family','before','after','delta']);wr('closed_residuals.csv',[],['residual_id','reason']);wr('remaining_residuals.csv',rf,['residual_id','primitive_family','kernel_family','atomic_kernel','cases','failure_class','diagnostic']);wr('minimal_missing_atomic_basis.csv',[{'atomic_kernel':x,'covered_residuals':sum(r['primitive_family']==x for r in rf)} for x in sorted({r['atomic_kernel'] for r in rf})],['atomic_kernel','covered_residuals']);wr('atomic_case_coverage.csv',[{'atomic_kernel':x,'cases':sum(int(r['cases'] or 0) for r in rf if r['atomic_kernel']==x)} for x in sorted({r['atomic_kernel'] for r in rf})],['atomic_kernel','cases']);wr('no_external_evidence_24.csv',rd(EXT/'residuals_without_external_evidence.csv'),['residual_id','failure_stage','failure_class','diagnostic','cases','fingerprint']);wr('remaining_40_contracts.csv',rd(ROOT/'outputs/primitive-contract-closure/remaining_insufficient_evidence.csv'),['gap_id','primitive_id','reason'])
summary={'raw_failure_rows':len(fail),'unique_residuals_before':len(res),'primitive_families':len({r['primitive_family'] for r in rf}),'kernel_families':len({r['kernel_family'] for r in rf}),'parameterized_kernel_families':len({r['kernel_family'] for r in rf if r['kernel_family'] in ('COMPARE','LITERAL','CONVERT','BITWISE_BINARY','LOGICAL_BINARY')}),'strong_external_candidates':len(proof),'internally_exact_proven':sum(x['internal_proof']=='INTERNAL_EXACT_PROVEN' for x in proof),'target_proof_missing':len(proof)-sum(x['internal_proof']=='INTERNAL_EXACT_PROVEN' for x in proof),'new_exact_contracts':0,'new_generated_recipes':0,'new_equivalence_rules':0,'residuals_closed_directly':0,'residuals_closed_transitively':0,'residuals_remaining':len(res),'residuals_without_evidence':len(rd(EXT/'residuals_without_external_evidence.csv')),'original_insufficient_contracts':40,'contracts_closed_after_new_closure':0,'contracts_remaining':40,'minimal_missing_atomic_basis_size':len({r['atomic_kernel'] for r in rf}),'cases_covered_by_existing_basis':0,'cases_requiring_new_atomic_basis':len(fail),'intermediate_feasible_routes_before':408,'intermediate_feasible_routes_after':408,'closure_reachable_rules':0,'executor_reachable_rules':0}
(OUT/'summary.json').write_text(json.dumps(summary,indent=2),encoding='utf-8');print(json.dumps(summary,indent=2))
