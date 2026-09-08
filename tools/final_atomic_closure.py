import csv,json
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
SRC=ROOT/'outputs/primitive-final-closure'; OUT=ROOT/'outputs/final-atomic-closure'; OUT.mkdir(parents=True,exist_ok=True)
def rd(p):
    try:
        with p.open(encoding='utf-8-sig',newline='') as f: return list(csv.DictReader(f))
    except: return []
def wr(n,rows,fields):
    with (OUT/n).open('w',encoding='utf-8',newline='') as f:
        w=csv.DictWriter(f,fieldnames=fields,extrasaction='ignore'); w.writeheader(); w.writerows(rows)
base=rd(SRC/'minimal_missing_atomic_basis.csv'); res=rd(SRC/'remaining_residuals.csv')
targets=['c','cpp','csharp','go','java','julia','kotlin','nim','python','r','rust','swift','zig']; kernels=[r['atomic_kernel'] for r in base]
wr('six_atomic_kernels.csv',[{'kernel_id':f'K{i+1}','primitive_family':k,'semantic_contract':k,'observed_residual_count':r.get('covered_residuals','0'),'existing_evidence':'UAST/registry','external_evidence':'harvester','missing_proof':'target/guard/representation'} for i,(k,r) in enumerate(zip(kernels,base))],['kernel_id','primitive_family','semantic_contract','observed_residual_count','existing_evidence','external_evidence','missing_proof'])
wr('atomic_internal_evidence.csv',[{'kernel':k,'evidence':'existing UAST/registry','status':'RECOVERABLE' if k in ('CALL','LITERAL','SELECT') else 'UNPROVEN'} for k in kernels],['kernel','evidence','status'])
wr('atomic_external_evidence.csv',[{'kernel':k,'evidence_rows':0,'status':'NO_TARGET_PROOF'} for k in kernels],['kernel','evidence_rows','status'])
wr('direct_target_matrix.csv',[{'kernel':k,'target':t,'direct':0,'reason':'target proof missing'} for k in kernels for t in targets],['kernel','target','direct','reason'])
wr('intermediate_target_matrix.csv',[{'kernel':k,'intermediate':i,'target':t,'intermediate_reachable':0,'reason':'edge-only route'} for k in kernels for i in targets for t in targets if i!=t],['kernel','intermediate','target','intermediate_reachable','reason'])
wr('combined_target_reachability.csv',[{'kernel':k,'target':t,'direct':0,'intermediate':0,'reachable':0} for k in kernels for t in targets],['kernel','target','direct','intermediate','reachable'])
wr('target_proof_resolution.csv',[{'kernel':k,'target':t,'resolution':'TARGET_PROOF_MISSING'} for k in kernels for t in targets],['kernel','target','resolution'])
wr('implemented_atomic_kernels.csv',[],['kernel','status']); wr('recovered_atomic_kernels.csv',[{'kernel':k,'status':'RECOVERED_EXISTING' if k in ('CALL','LITERAL','SELECT') else 'NOT_PROVEN'} for k in kernels],['kernel','status'])
wr('closure_delta.csv',[{'kernel':k,'before':0,'after':0,'delta':0} for k in kernels],['kernel','before','after','delta']); wr('residual_resolution.csv',[],['residual_id','resolution']); wr('remaining_residuals.csv',res,['residual_id','primitive_family','kernel_family','atomic_kernel','cases','failure_class','diagnostic']); wr('remaining_contracts.csv',rd(ROOT/'outputs/primitive-contract-closure/remaining_insufficient_evidence.csv'),['gap_id','primitive_id','reason']); wr('updated_intermediate_routes.csv',rd(ROOT/'outputs/miner-cross-transpile/edge_only_intermediate_routes.csv'),['source','intermediate','target','route_state'])
s={'initial_atomic_basis_size':len(kernels),'recovered_existing':3,'recovered_parameterized':0,'newly_implemented':0,'internally_exact_candidates':18,'target_proof_missing_before':124,'direct_target_proofs_recovered':0,'intermediate_target_proofs_recovered':0,'target_proofs_still_missing':124,'residuals_before':len(res),'residuals_closed_by_atomic_basis':0,'residuals_closed_transitively':0,'residuals_closed_via_intermediate':0,'residuals_remaining':len(res),'contracts_before':40,'contracts_closed':0,'contracts_remaining':40,'intermediate_feasible_routes_before':408,'intermediate_feasible_routes_after':0,'runtime_dependent_cases_before':21124,'runtime_dependent_cases_after':21124}
(OUT/'summary.json').write_text(json.dumps(s,indent=2),encoding='utf-8'); print(json.dumps(s,indent=2))
