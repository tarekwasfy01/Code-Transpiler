import fs from 'node:fs/promises';
import assert from 'node:assert/strict';
import path from 'node:path';

// Intersect actual per-contract route witnesses with an explicit demand vector.
// No scores/priorities; return all equally valid intermediate-language choices.
const [source,target,list,directory='outputs/language-route-matrix-v1',featureDirectory='outputs/language-feature-matrix-v1']=process.argv.slice(2);
if(!source||!target||!list)throw Error('Usage: node tools/language-feature-matrix/find-routes.mjs SOURCE TARGET contract_id[,feature:feature_id] [matrix-directory] [feature-directory]');
const data=JSON.parse(await fs.readFile(directory+'/routes.json','utf8'));
assert.equal(data.verification.status,'PASS');
const s=data.languages.indexOf(source),t=data.languages.indexOf(target);
assert(s>=0&&t>=0&&s!==t,'Specify two different languages in the measured matrix');
const demands=[...new Set(list.split(',').map(value=>value.trim()))];
assert(demands.every(Boolean),'Every requirement must be a nonempty contract ID or feature:feature_id');
const featureDemands=demands.filter(value=>value.startsWith('feature:'));
let registry=null;
if(featureDemands.length){
 registry=JSON.parse(await fs.readFile(path.join(featureDirectory,'language_features.json'),'utf8'));
 assert.equal(registry.verification?.status,'PASS','Feature registry must be independently verified');
 assert.equal(registry.measurement_sha256,data.source_measurement_sha256,'Feature registry and route matrix must use the same measurement');
}
const featureRequirements=featureDemands.map(request=>{
 const id=request.slice('feature:'.length),feature=registry.features.find(value=>value.id===id);
 const contracts=feature?[...new Set(feature.contracts)]:[];
 return {request,id,contracts,status:!feature?'UNKNOWN_FEATURE':contracts.length?'CORPUS_CONTRACTS_ONLY':'NO_CONTRACT_EVIDENCE',full_feature_support:false};
});
const unresolvedFeatures=featureRequirements.filter(feature=>feature.contracts.length===0);
const requested=[...new Set([...demands.filter(value=>!value.startsWith('feature:')),...featureRequirements.flatMap(feature=>feature.contracts)])];
const absent=requested.filter(f=>!data.features.includes(f));
const requirement=data.features.map(f=>Number(requested.includes(f)));
// An unresolved feature contributes a mandatory UNKNOWN atom. Otherwise an
// empty contract expansion would incorrectly satisfy every intermediate.
const requirementCount=requested.length+unresolvedFeatures.length;
assert(requirementCount>0,'At least one nonempty requirement is needed');
const alternatives=data.languages.filter(v=>v!==source&&v!==target).map(via=>{
 const witness=requirement.map((needed,f)=>needed?data.routes[f][s][t]:null);
 const pass=witness.map(w=>Number(Boolean(w?.valid_via.includes(via))));
 const fail=witness.map(w=>Number(Boolean(w?.failed_via.includes(via))));
 const unknown=witness.map(w=>Number(Boolean(w?.unknown_via.includes(via))));
 const dot=v=>v.reduce((sum,x,i)=>sum+x*requirement[i],0);
 const passed=dot(pass),failed=dot(fail),unobserved=dot(unknown)+absent.length+unresolvedFeatures.length;
 assert.equal(passed+failed+unobserved,requirementCount);
 return {via,pass:passed,fail:failed,unknown:unobserved,eligible_for_measured_contracts:passed===requirementCount};
});
const warnings=[];
const provenance={execution_mode:data.execution_mode,source_measurement_sha256:data.source_measurement_sha256,baseline_source_tree_hash:data.baseline_source_tree_hash,relay_source_tree_hash:data.source_tree_hash,new_runtime_execution:false};
if(data.execution_mode?.startsWith('current source')){
 warnings.push('Stored route evidence combines baseline first-leg target texts with second-leg translation from the recorded source version. This query does not rerun either leg or any native program.');
 let equivalence;
 const equivalencePath=path.join(directory,'source_equivalence.json');
 try{equivalence=JSON.parse(await fs.readFile(equivalencePath,'utf8'))}catch(error){if(error.code!=='ENOENT')throw error}
 const matching=Boolean(equivalence&&equivalence.status==='PASS'&&equivalence.source_stable===true&&equivalence.measurements_stable===true&&equivalence.measurement_sha256===data.source_measurement_sha256&&equivalence.current_source_before?.source_tree_hash===data.source_tree_hash);
 provenance.source_equivalence={path:equivalencePath,status:matching?'MATCHING_SAVED_PROOF':equivalence?'SAVED_PROOF_DOES_NOT_MATCH':'NOT_ATTACHED',scope:'Saved byte-equivalence evidence only; not a new compiler or runtime check'};
 if(!matching)warnings.push('No matching source-equivalence report accompanies this route matrix. The baseline and relay source hashes remain separate provenance, not a claim that all current translations were revalidated.');
}
console.log(JSON.stringify({source,target,requests:demands,contracts:requested,feature_requirements:featureRequirements,unresolved_feature_requirements:unresolvedFeatures.map(feature=>feature.request),requirement_count:requirementCount,requirement_vector:requirement,unmeasured_contracts:absent,
 verified_intermediates:alternatives.filter(x=>x.eligible_for_measured_contracts).map(x=>x.via),alternatives,
 provenance,warnings,
 policy:'All alternatives have equal weight. Feature requests expand to their measured corpus contracts and never certify full feature semantics. Unknown features and features without contracts remain unsatisfied UNKNOWN requirements. Eligibility is the intersection of actual witnesses for these corpus contracts, not certification of their arbitrary combination in a new program. New programs must undergo the same actual relay and native-output checks. No translation, build or unverified fallback is executed.'},null,2));
