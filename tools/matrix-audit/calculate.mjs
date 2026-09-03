import fs from 'node:fs/promises';
import path from 'node:path';
import {createHash} from 'node:crypto';

const dir=path.resolve('outputs/transpiler-audit-2026-08-30');
const raw=await fs.readFile(path.join(dir,'numeric_matrices.json'));
const m=JSON.parse(raw);
const measurements=JSON.parse(await fs.readFile(path.join(dir,'measurements.json'),'utf8'));
const fixtures=JSON.parse(await fs.readFile(path.join(dir,'fixtures.json'),'utf8'));
const {B,K,languages,features,pairs}=m;
const sum=a=>a.reduce((x,y)=>x+y,0);
const tr=a=>a[0].map((_,j)=>a.map(r=>r[j]));
const mm=(a,b)=>a.map(row=>tr(b).map(col=>sum(row.map((v,j)=>v*col[j]))));
const zeros=(n,k)=>Array.from({length:n},()=>Array(k).fill(0));
const P=B.map((r,i)=>r.map((v,j)=>K[i][j]-v));
const assert=(test,message)=>{if(!test)throw new Error(message);};
assert(B.length===156&&K.length===156&&features.length===32,'Wrong dimensions');
for(let i=0;i<156;i++)for(let j=0;j<32;j++)assert([0,1].includes(B[i][j])&&[0,1].includes(K[i][j])&&B[i][j]<=K[i][j],'Invalid masked binary data');
const u=Array.from({length:32},()=>[1]);
const rowB=mm(B,u).flat(),rowK=mm(K,u).flat();
const BB=mm(tr(B),B),KK=mm(tr(K),K),PP=mm(tr(P),P);
const sharedFailures=mm(B,tr(B)),sharedKnown=mm(K,tr(K));
const counts=(b,k,total)=>({pass:k-b,fail:b,unknown:total-k,known:k,total,coverage:k/total,error_rate_known:k?b/k:null,error_lower_bound:b/total,error_upper_bound:(b+total-k)/total});
const byPair=pairs.map((p,i)=>({...p,...counts(rowB[i],rowK[i],32)}));
const byFeature=features.map((feature,j)=>({feature,...counts(BB[j][j],KK[j][j],156)}));
const groupByLanguage=side=>languages.map(language=>{
 const rows=byPair.filter(p=>p[side]===language);return {language,...counts(sum(rows.map(r=>r.fail)),sum(rows.map(r=>r.known)),rows.length*32)};
});
const stages=Object.fromEntries(Object.entries(m.stages).map(([stage,s])=>[stage,counts(sum(s.B.flat()),sum(s.K.flat()),156*32)]));
const recordMap=new Map(measurements.records.map(r=>[r.id,r]));
const fixtureMap=new Map(fixtures.map(r=>[r.source+':'+r.feature,r]));

// Check whether the 156-row emission failures factor exactly into source cases.
const sourceIncidence=pairs.map(p=>languages.map(l=>p.source===l?1:0));
const sourceReject=zeros(13,32),sourceObserved=zeros(13,32),nonfactorable=[];
for(let s=0;s<13;s++)for(let f=0;f<32;f++){
 const rows=pairs.map((p,i)=>p.source===languages[s]?i:-1).filter(i=>i>=0);
 const vals=rows.map(i=>m.stages.emit.B[i][f]+','+m.stages.emit.K[i][f]);
 if(new Set(vals).size!==1)nonfactorable.push({source:languages[s],feature:features[f]});
 sourceReject[s][f]=m.stages.emit.B[rows[0]][f];sourceObserved[s][f]=m.stages.emit.K[rows[0]][f];
}
const product=mm(sourceIncidence,sourceReject);
const residual=sum(product.flatMap((r,i)=>r.map((v,j)=>Math.abs(v-m.stages.emit.B[i][j]))));
const equivalenceGroups=[];
for(let f=0;f<32;f++){
 const signature=JSON.stringify([B.map(r=>r[f]),K.map(r=>r[f])]);
 let g=equivalenceGroups.find(x=>x.signature===signature);if(!g){g={signature,features:[]};equivalenceGroups.push(g);}g.features.push(features[f]);
}
// Boolean relation per feature, constrained to matching scalar contracts.
// Reachability is only a candidate relation: generated code must still be fed through the next translator.
const reachability=[],candidates=[];
for(let f=0;f<32;f++){
 const A=zeros(13,13);
 for(let p=0;p<156;p++)if(P[p][f]){
  const pair=pairs[p],src=fixtureMap.get(pair.source+':'+features[f]),dst=fixtureMap.get(pair.target+':'+features[f]);
  if(src&&dst&&src.kind===dst.kind&&JSON.stringify(src.expected)===JSON.stringify(dst.expected))A[languages.indexOf(pair.source)][languages.indexOf(pair.target)]=1;
 }
 const A2=mm(A,A),closure=A.map(r=>[...r]);
 for(let k=0;k<13;k++)for(let i=0;i<13;i++)for(let j=0;j<13;j++)closure[i][j]=closure[i][j]||(closure[i][k]&&closure[k][j])?1:0;
 let newReachable=0;
 for(let p=0;p<156;p++){
  const {source,target}=pairs[p],i=languages.indexOf(source),j=languages.indexOf(target);
  if(closure[i][j]&&!P[p][f])newReachable++;
  if(A2[i][j]&&!P[p][f])for(let k=0;k<13;k++)if(A[i][k]&&A[k][j]){
   const direct=recordMap.get(`${source}>${target}:${features[f]}`);
   const first=recordMap.get(`${source}>${languages[k]}:${features[f]}`);
   candidates.push({id:`${source}>${languages[k]}>${target}:${features[f]}`,source,intermediate:languages[k],target,feature:features[f],direct_status:direct.overall,native_source_verified:direct.source_validated,
    expected:direct.expected,kind:fixtureMap.get(source+':'+features[f]).kind,intermediate_code:first.target_code,original_source:first.source_code});
  }
 }
 reachability.push({feature:features[f],observed_pass_edges:sum(P.map(r=>r[f])),contract_compatible_edges:sum(A.flat()),two_step_walks:sum(A2.flat()),additional_candidate_pairs:newReachable,A,A_squared:A2,closure});
}
const findings={input_sha256:createHash('sha256').update(raw).digest('hex'),method:'Exact integer matrix products, binary masks, equal weights. No imputation of unknown success and no root-cause inference from co-occurrence.',
 total:counts(sum(rowB),sum(rowK),156*32),by_pair:byPair,by_feature:byFeature,by_source:groupByLanguage('source'),by_target:groupByLanguage('target'),stages,
 category_cofailure:BB,category_coobserved:KK,category_cosuccess:PP,pair_cofailure:sharedFailures,pair_coobserved:sharedKnown,
 identical_masked_feature_patterns:equivalenceGroups.map(({features})=>features),
 emission_factorization:{source_incidence:sourceIncidence,source_rejected:sourceReject,source_observed:sourceObserved,nonfactorable,absolute_residual:residual,rejected_source_cases:sum(sourceReject.flat()),rejected_route_cases:sum(product.flat())},
 reachability,candidate_count:candidates.length,candidate_direct_pairs:new Set(candidates.map(c=>`${c.source}>${c.target}:${c.feature}`)).size,
 native_candidate_count:candidates.filter(c=>c.native_source_verified).length};
assert(sum(byFeature.map(r=>r.fail))===measurements.summary.overall.FAIL&&sum(rowK)===measurements.summary.overall.PASS+measurements.summary.overall.FAIL,'Conservation failure');
assert(BB.every((r,i)=>r[i]===byFeature[i].fail)&&KK.every((r,i)=>r[i]===byFeature[i].known),'Gram diagonal mismatch');
await fs.writeFile(path.join(dir,'matrix_calculations.json'),JSON.stringify(findings,null,2));
await fs.writeFile(path.join(dir,'two_step_candidates.json'),JSON.stringify(candidates,null,2));
console.log(JSON.stringify({total:findings.total,stages,feature:byFeature,factorization:findings.emission_factorization,candidates:{routes:candidates.length,pairs:findings.candidate_direct_pairs,native:findings.native_candidate_count},reachability:reachability.map(({feature,observed_pass_edges,contract_compatible_edges,two_step_walks,additional_candidate_pairs})=>({feature,observed_pass_edges,contract_compatible_edges,two_step_walks,additional_candidate_pairs}))},null,2));
