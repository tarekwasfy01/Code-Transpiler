import csv,json,os
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]; OUT=ROOT/'outputs/miner-cross-transpile'; OUT.mkdir(parents=True,exist_ok=True)
files=[ROOT/'outputs/site-packages-alltoall-current/all_to_all_results.csv',ROOT/'outputs/miner-v4.3-all-to-all/all_to_all_results.csv',ROOT/'outputs/site-packages-alltoall-round3/all_to_all_results.csv',ROOT/'outputs/site-packages-alltoall-round3/roundtrip/all_to_all_results.csv']
rows=[]
for p in files:
 if not p.exists(): continue
 with p.open(encoding='utf-8-sig',newline='') as f:
  for r in csv.DictReader(f):
   src=r.get('source_language',r.get('source','')); tgt=r.get('target_language',r.get('target','')); status=r.get('status','')
   if src and tgt: rows.append({'src':src,'tgt':tgt,'status':status,'file':r.get('source_file',''),'out':r.get('output_file',''),'diag':r.get('diagnostic','')})
edges={}
for r in rows:
 k=(r['src'],r['tgt']); x=edges.setdefault(k,{'pass':0,'fail':0,'w':[]})
 if r['status'].upper()=='PASS': x['pass']+=1
 else: x['fail']+=1
 if r['status'].upper()=='PASS' and r['out']: x['w'].append(r['out'])
links=[]; ec=[]
for (s,t),a in edges.items():
 for (s2,t2),b in edges.items():
  if t!=s2 or s==t2 or not a['pass'] or not b['pass']: continue
  # Existing artifacts have no root/UAST hashes; independent legs are evidence only.
  z={'source':s,'intermediate':t,'target':t2,'leg1_passes':a['pass'],'leg2_passes':b['pass'],'case_linked_witnesses':0,'semantic_verified_witnesses':0,'failures':a['fail']+b['fail'],'primitive_demand_covered':'UNKNOWN','semantic_shapes_covered':'KNOWN_14','route_state':'EDGE_ONLY'}
  links.append(z); ec.append(z)
def write(name, rows, head):
 with (OUT/name).open('w',encoding='utf-8',newline='') as f:
  w=csv.DictWriter(f,fieldnames=head);w.writeheader();w.writerows(rows)
head=['source','intermediate','target','leg1_passes','leg2_passes','case_linked_witnesses','semantic_verified_witnesses','failures','primitive_demand_covered','semantic_shapes_covered','route_state']
write('case_linked_intermediate_routes.csv',[],head);write('semantic_verified_intermediate_routes.csv',[],head);write('edge_only_intermediate_routes.csv',ec,head)
write('primitive_route_matrix.csv',[{'source':x['source'],'intermediate':x['intermediate'],'target':x['target'],'primitive':'UNCLASSIFIED','reachable':'UNKNOWN'} for x in links],['source','intermediate','target','primitive','reachable']);write('demand_route_matrix.csv',[{'source':x['source'],'intermediate':x['intermediate'],'target':x['target'],'demand':'UNKNOWN','feasible':'UNKNOWN'} for x in links],['source','intermediate','target','demand','feasible'])
write('intermediate_route_witnesses.csv',links,head)
write('intermediate_route_matrix.csv',links,head)
fail=[r for r in rows if r['status'].upper()!='PASS']
write('primitive_residual_matrix.csv',[{'source':r['src'],'target':r['tgt'],'primitive':'UNCLASSIFIED','failure':r['diag']} for r in fail],['source','target','primitive','failure'])
write('atomic_residual_matrix.csv',[{'source':r['src'],'target':r['tgt'],'atomic':'UNCLASSIFIED','failure':r['diag']} for r in fail],['source','target','atomic','failure'])
write('external_evidence_residual_crosswalk.csv',[],['observed_gap','external_evidence','status'])
summary={'total_cells':len(rows),'pass':sum(r['status'].upper()=='PASS' for r in rows),'fail':len(fail),'original_routes':328,'case_linked_routes':0,'semantic_verified_routes':0,'edge_only_routes':len(links),'unobserved_closure_feasible_routes':0,'infeasible_routes':0,'primitive_residuals':len(fail),'kernel_residuals':len(fail),'atomic_residuals':len(fail),'verified_intermediate_rescue_cases':0,'runtime_required_cases':len(fail),'no_known_route_cases':len(fail),'external_evidence_matches':0,'lineage_note':'Legacy result CSVs contain no root_case_id/UAST hashes; routes remain EDGE_ONLY.'}
(OUT/'summary.json').write_text(json.dumps(summary,indent=2),encoding='utf-8');print(json.dumps(summary,indent=2))
