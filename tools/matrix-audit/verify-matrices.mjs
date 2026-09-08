import fs from 'node:fs/promises';
import path from 'node:path';
import assert from 'node:assert/strict';
import {spawn} from 'node:child_process';
import {sourceIdentity} from './source-identity.mjs';

const directory=process.argv[2]||'outputs/transpiler-audit-v13';
const evidence=JSON.parse(await fs.readFile(path.join(directory,'evidence_matrix.json')));
const measured=JSON.parse(await fs.readFile(path.join(directory,'measurements.json')));
const identity=await sourceIdentity(process.cwd());
assert.equal(identity.source_tree_hash,evidence.source_tree_hash);
assert.equal(identity.source_tree_hash,measured.summary.source_tree_hash);
const X=evidence.design,n=X.shape[0],p=X.shape[1],s=evidence.stages.length;
const rows=Array.from({length:n},()=>[]);
for(let i=0;i<X.value.length;i++)rows[X.row[i]].push([X.column[i],X.value[i]]);
const gram=new Float64Array(p*p),xe=Array.from({length:p},()=>Array(s).fill(0)),xk=Array.from({length:p},()=>Array(s).fill(0));
for(let r=0;r<n;r++) {
 for(let j=0;j<s;j++){assert.equal(evidence.observed[r][j]+evidence.unknown[r][j],1);assert(evidence.failure[r][j]<=evidence.observed[r][j])}
 for(const [a,av] of rows[r]) {
  for(const [b,bv] of rows[r])gram[a*p+b]+=av*bv;
  for(let j=0;j<s;j++){xe[a][j]+=av*evidence.failure[r][j];xk[a][j]+=av*evidence.observed[r][j]}
 }
}
const stored=new Float64Array(p*p),G=evidence.gram;
for(let i=0;i<G.value.length;i++)stored[G.row[i]*p+G.column[i]]=G.value[i];
assert.deepEqual(gram,stored);assert.deepEqual(xe,evidence.projected_failure);assert.deepEqual(xk,evidence.projected_known);

const fixtures=JSON.parse(await fs.readFile(path.join(directory,'fixtures.json')));
const requests=fixtures.map(f=>({id:f.source+':'+f.feature,source:f.source,code:f.code,mode:'function-flow'}));
const flows=await new Promise((resolve,reject)=>{
 const child=spawn(path.resolve('.audit-cache/matrix-audit.exe'),[],{windowsHide:true});
 let out='',err='';const timer=setTimeout(()=>{child.kill();reject(Error('flow audit timeout'))},60000);
 child.stdout.setEncoding('utf8');child.stderr.setEncoding('utf8');child.stdout.on('data',s=>out+=s);child.stderr.on('data',s=>err+=s);
 child.on('error',e=>{clearTimeout(timer);reject(e)});child.on('close',code=>{clearTimeout(timer);if(code!==0)reject(Error(err));else {try{resolve(JSON.parse(out))}catch(e){reject(e)}}});
 child.stdin.on('error',()=>{});child.stdin.end(JSON.stringify(requests));
});
let functions=0,branchNodes=0,loopNodes=0,stateMachines=0,stateSlots=0,iterationVectors=0,iterationProjectionChecks=0;
for(const response of flows) {
 assert(!response.error,`${response.id}: ${response.error}`);
 for(const flow of response.flows||[]) {
  assert(!flow.error,`${response.id}/${flow.name}: ${flow.error}`);
  functions++;
  if(flow.state_machine)stateMachines++;
  stateSlots+=flow.slots?.length||0;
  for(const iteration of flow.iterations||[]) {
   iterationVectors++;
   const m=iteration.advance;
   assert.equal(m.Rows,3);assert.equal(m.Cols,3);assert.equal(m.Data.length,9);
   assert.equal(iteration.initial_position,1);
   const privateSlots=[iteration.sequence_slot,iteration.position_slot,iteration.length_slot];
   assert.equal(new Set(privateSlots).size,3);
   for(const name of [...privateSlots,iteration.element_slot])assert(flow.slots.includes(name),name);
   // Check actual exported affine transition on independent integer states,
   // not by asking the generator to compute its own expected answers.
   for(const length of [0,1,2,7,1000])for(const position of [1,2,7,1001]) {
    const vector=[position,length,1];
    const projected=Array.from({length:3},(_,i)=>vector.reduce((sum,v,j)=>sum+m.Data[i*3+j]*v,0));
    assert.deepEqual(projected,[position+1,length,1]);iterationProjectionChecks++;
   }
  }
  const n=flow.nodes.length,adj=Array.from({length:n},()=>new Set());
  const matrices=[flow.always,flow.when_true,flow.when_false];
  for(const m of matrices){assert.equal(m.Rows,n);assert.equal(m.Cols,n);assert.equal(m.Data.length,n*n)}
  for(let i=0;i<n;i++) {
   const counts=matrices.map(m=>m.Data.slice(i*n,(i+1)*n).reduce((sum,v,j)=>{assert(v===0||v===1);if(v)adj[i].add(j);return sum+v},0));
   const kind=flow.nodes[i];
   if(kind==='*backend.IfStmt'||kind==='*backend.WhileStmt'){if(kind==='*backend.IfStmt')branchNodes++;else loopNodes++;assert.deepEqual(counts,[0,1,1])}
   else if(kind==='*backend.ReturnStmt'||i===0)assert.deepEqual(counts,[0,0,0]);
   else assert.deepEqual(counts,[1,0,0]);
  }
  // Independent graph traversal checks the generator's matrix closure vector.
  function reachable(start){const seen=new Set(),queue=[start];while(queue.length){const i=queue.pop();if(seen.has(i))continue;seen.add(i);queue.push(...adj[i])}return seen}
  const seen=reachable(flow.entry);assert(!seen.has(0),'reachable fallthrough');
  assert.deepEqual(flow.reachable,Array.from({length:n},(_,i)=>Number(seen.has(i))));
  if(flow.cycles)assert.deepEqual(flow.cycles,adj.map((next,i)=>Number([...next].some(j=>reachable(j).has(i)))));
  if(flow.initial){
   const k=flow.slots.length;assert.equal(flow.initial.length,k);
   for(const m of [flow.reads,flow.writes,flow.defined]){assert.equal(m.Rows,n);assert.equal(m.Cols,k);assert.equal(m.Data.length,n*k);assert(m.Data.every(v=>v===0||v===1))}
   // Propagate concrete sets from the entry; joins intersect observations.
   // This does not use the generator's matrix fixed-point implementation.
   const known=new Map(),queue=[[flow.entry,new Set(flow.initial.flatMap((v,i)=>v?[i]:[]))]];
   while(queue.length){
    const [i,incoming]=queue.shift(),prior=known.get(i);
    const merged=prior?new Set([...prior].filter(j=>incoming.has(j))):incoming;
    if(prior&&merged.size===prior.size)continue;known.set(i,merged);
    const out=new Set(merged);for(let j=0;j<k;j++)if(flow.writes.Data[i*k+j])out.add(j);
    for(const target of adj[i])queue.push([target,new Set(out)]);
   }
   for(const i of seen){assert.deepEqual(flow.defined.Data.slice(i*k,(i+1)*k),Array.from({length:k},(_,j)=>Number(known.get(i).has(j))));for(let j=0;j<k;j++)assert(flow.reads.Data[i*k+j]<=flow.defined.Data[i*k+j])}
  }
 }
}
await fs.writeFile(path.join(directory,'function_flow_matrices.json'),JSON.stringify({source_tree_hash:identity.source_tree_hash,functions,branch_nodes:branchNodes,loop_nodes:loopNodes,state_machines:stateMachines,state_slots:stateSlots,iteration_vectors:iterationVectors,iteration_projection_checks:iterationProjectionChecks,fixtures:flows},null,2));
const result={matrix_products:'PASS',mask_invariants:'PASS',source_identity:'PASS',source_tree_hash:identity.source_tree_hash,design:X.shape,control_flow_matrices:'PASS',definite_assignment:'PASS',iteration_transitions:'PASS',functions,branch_nodes:branchNodes,loop_nodes:loopNodes,state_machines:stateMachines,state_slots:stateSlots,iteration_vectors:iterationVectors,iteration_projection_checks:iterationProjectionChecks,native_source_and_target_pass:measured.records.filter(r=>r.overall==='PASS'&&r.source_validated).length};
await fs.writeFile(path.join(directory,'verification.json'),JSON.stringify(result,null,2));console.log(JSON.stringify(result,null,2));
