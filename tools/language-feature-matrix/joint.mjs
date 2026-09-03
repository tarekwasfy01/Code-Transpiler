import fs from 'node:fs/promises';
import path from 'node:path';
import assert from 'node:assert/strict';
import {spawn} from 'node:child_process';
import {createHash} from 'node:crypto';
import {sourceIdentity} from '../matrix-audit/source-identity.mjs';
import {createNativeExecutor,matches} from './native-executor.mjs';

// Joint measurement always starts with the original fixtures, translates the
// ACTUAL intermediate text, and checks the ORIGINAL observable contract.
// Fanout shares one decoded AST, never a cached original-source AST.
const root=process.cwd(),output=path.resolve(process.argv[2]||'outputs/semantic-matrix-v17');
await fs.mkdir(output,{recursive:true});
const hash=b=>createHash('sha256').update(b).digest('hex');
const oldBytes=await fs.readFile('outputs/transpiler-audit-v16/measurements.json');
const old=JSON.parse(oldBytes),L=old.summary.languages;
const allFixtures=JSON.parse(await fs.readFile('outputs/transpiler-audit-v16/fixtures.json'));
const selected=process.env.JOINT_FEATURES?.split(',');
const fixtures=allFixtures.filter(f=>!selected||selected.includes(f.feature));
const features=[...new Set(fixtures.map(f=>f.feature))];
const identity=await sourceIdentity(root),release=hash(await fs.readFile('dist/CodeTranspiler.exe'));
const oldById=new Map(old.records.map(r=>[r.id,r]));
const execute=createNativeExecutor(root),nativeCache=new Map(),texts=new Map();
let reusedNativeProof=null;
for(const r of old.records)if(r.target_code&&r.execution?.run==='PASS')nativeCache.set(r.target+':'+hash(r.target_code),Promise.resolve({compile:r.compile,run:r.run,stdout:r.actual,provenance:'identical language and code bytes in V16 native evidence',measurement_id:r.id}));
if(process.env.JOINT_REUSE_DIR){
 const dir=path.resolve(process.env.JOINT_REUSE_DIR),nativeBytes=await fs.readFile(path.join(dir,'native_evidence.json')),codeBytes=await fs.readFile(path.join(dir,'code_evidence.json'));
 const codeByKey=new Map(JSON.parse(codeBytes).map(t=>{assert.equal(t.key,t.language+':'+hash(t.code));return [t.key,t]}));
 reusedNativeProof={directory:dir,native_sha256:hash(nativeBytes),code_sha256:hash(codeBytes)};
 for(const n of JSON.parse(nativeBytes))if(n.compile==='PASS'&&n.run==='PASS'){assert(codeByKey.has(n.key));nativeCache.set(n.key,Promise.resolve({...n,provenance:'identical language and code bytes in saved joint native evidence'}))}
}
function adapter(requests){return new Promise((resolve,reject)=>{
 const child=spawn('go',['run','./tools/matrix-audit'],{windowsHide:true,stdio:['pipe','pipe','pipe'],env:{...process.env,GOCACHE:path.resolve('.audit-cache/go-build'),GOMEMLIMIT:'512MiB',GOGC:'50'}});let stdout='',stderr='',settled=false;
 const stop=()=>{if(process.platform==='win32'&&child.pid){const killer=spawn('taskkill.exe',['/PID',String(child.pid),'/T','/F'],{windowsHide:true,stdio:'ignore'});killer.on('error',()=>child.kill())}else child.kill()};
 const timer=setTimeout(()=>{stop();finish(Error('joint source adapter timeout'))},180000);
 function finish(error,value){if(settled)return;settled=true;clearTimeout(timer);error?reject(error):resolve(value)}
 child.on('error',e=>finish(e));child.stdout.setEncoding('utf8');child.stderr.setEncoding('utf8');
 child.stdout.on('data',v=>{stdout+=v;if(stdout.length>128*1024*1024){stop();finish(Error('joint output limit'))}});child.stderr.on('data',v=>stderr+=v);
 child.on('close',code=>{if(code!==0)return finish(Error(`adapter exit ${code}: ${stderr}`));try{const values=JSON.parse(stdout);assert.equal(values.length,requests.length);finish(null,values)}catch(e){finish(e)}});
 child.stdin.on('error',()=>{});child.stdin.end(JSON.stringify(requests));
})}
async function batch(items,make,label){const out=new Map();for(let i=0;i<items.length;i+=24){const values=await adapter(items.slice(i,i+24).map(make));for(const v of values){assert(!out.has(v.id));out.set(v.id,v)};if(i%120===0)console.log(`${label} ${Math.min(i+24,items.length)}/${items.length}`)}return out}
const originals=await batch(fixtures,f=>({id:f.source+':'+f.feature,source:f.source,code:f.code,mode:'fanout',targets:L,include_semantic:true,max_tokens:65536}),'Source AST and direct fanout');
const direct=[],lookup=new Map(),intermediates=new Map();
for(const f of fixtures){const result=originals.get(f.source+':'+f.feature);for(const target of L)if(target!==f.source){
 const emitted=result.error?result:result.results.find(r=>r.id===target);assert(emitted);
 const row={id:`${f.source}>${target}:${f.feature}`,source:f.source,target,feature:f.feature,emit:emitted.error?'FAIL':'PASS',reason:emitted.error||null};
 if(emitted.code){const key=target+':'+hash(emitted.code);row.code_key=key;texts.set(key,{language:target,code:emitted.code});intermediates.set(key,{key,source:target,code:emitted.code})}
 const baseline=oldById.get(row.id);assert.equal(baseline.source_code,f.code);row.source_native=baseline.source_validated;direct.push(row);lookup.set(row.id,row);
}}
const decoded=await batch([...intermediates.values()],r=>({id:r.key,source:r.source,code:r.code,mode:'fanout',targets:L,max_tokens:65536}),'Actual intermediate decode and fanout');
for(const r of decoded.values())if(!r.error)for(const target of r.results)if(target.code){const key=target.id+':'+hash(target.code);texts.set(key,{language:target.id,code:target.code});target.code_key=key;delete target.code}
const fixtureMap=new Map(fixtures.map(f=>[f.source+':'+f.feature,f]));
const checkedNative=new Set();
async function validate(row,fixture){
 if(row.status)return;
 if(row.emit==='FAIL'){row.status=row.reason?.startsWith('RESOURCE_LIMIT:')?'UNKNOWN':'FAIL';row.stage='direct_emit';return}
 const key=row.code_key;assert(texts.has(key));checkedNative.add(key);
 if(!nativeCache.has(key)){const text=texts.get(key);nativeCache.set(key,execute(text.language,text.code))}
 const n=await nativeCache.get(key);row.compile=n.compile;row.run=n.run;row.actual=n.stdout??null;row.evidence=n.provenance||'new native execution';
 row.output_match=n.run==='PASS'?matches(n.stdout,fixture):null;
 row.status=n.compile==='FAIL'||n.run==='FAIL'||row.output_match===false?'FAIL':row.output_match===true?'PASS':'UNKNOWN';
 row.stage=row.status==='PASS'?'verified_output':n.compile!=='PASS'?'final_compile':n.run!=='PASS'?'final_run':'final_output';
 if(row.status!=='PASS')row.reason=n.reason||n.stderr||`expected ${JSON.stringify(fixture.expected)}; got ${JSON.stringify(n.stdout)}`;
}
async function pool(items,fn,label){let at=0,finished=0;await Promise.all(Array.from({length:3},async()=>{while(at<items.length){const row=items[at++];await fn(row);finished++;if(finished%1500===0)console.log(`${label} ${finished}/${items.length}`)}}))}
await pool(direct,r=>validate(r,fixtureMap.get(r.source+':'+r.feature)),'Direct evidence');
const routes=[],roundtrips=[];
for(const f of fixtures)for(const via of L)if(via!==f.source){const first=lookup.get(`${f.source}>${via}:${f.feature}`);for(const target of L)if(target!==via){
 const row={source:f.source,target,via,feature:f.feature,first_native:first.status==='PASS',source_native:first.source_native,intermediate_key:first.code_key||null};
 if(first.emit!=='PASS'){row.status=first.reason?.startsWith('RESOURCE_LIMIT:')?'UNKNOWN':'FAIL';row.stage='first_emit';row.reason=first.reason}
 else{const dec=decoded.get(first.code_key);assert(dec);if(dec.error){row.status=dec.error.startsWith('RESOURCE_LIMIT:')?'UNKNOWN':'FAIL';row.stage='intermediate_decode';row.reason=dec.error}else{const end=dec.results.find(r=>r.id===target);assert(end);if(end.error){row.status=end.error.startsWith('RESOURCE_LIMIT:')?'UNKNOWN':'FAIL';row.stage='second_emit';row.reason=end.error}else row.code_key=end.code_key}}
 if(target===f.source)roundtrips.push(row);else{row.direct=lookup.get(`${f.source}>${target}:${f.feature}`).status;routes.push(row)}
}}
await pool([...routes,...roundtrips],r=>validate(r,fixtureMap.get(r.source+':'+r.feature)),'Route and roundtrip evidence');
const count=rows=>Object.fromEntries(['PASS','FAIL','UNKNOWN'].map(s=>[s,rows.filter(r=>r.status===s).length]));
const adjacency=features.map(f=>L.map(s=>L.map(t=>Number(lookup.get(`${s}>${t}:${f}`)?.status==='PASS'))));
const square=A=>A.map(row=>A.map((_,t)=>row.reduce((n,a,m)=>n+a*A[m][t],0)));
const squared=adjacency.map(square);let matrixChecks=0;
for(let f=0;f<features.length;f++)for(let s=0;s<L.length;s++)for(let t=0;t<L.length;t++){assert.equal(squared[f][s][t],L.filter((_,m)=>adjacency[f][s][m]&&adjacency[f][m][t]).length);matrixChecks++}
for(const row of [...direct,...routes,...roundtrips])if(row.status==='PASS'){assert.equal(row.compile,'PASS');assert.equal(row.run,'PASS');assert.equal(row.output_match,true);matrixChecks++}
assert.equal(routes.length,features.length*13*12*11);assert.equal(direct.length,features.length*13*12);assert.equal(roundtrips.length,direct.length);
assert.equal((await sourceIdentity(root)).source_tree_hash,identity.source_tree_hash,'source changed during joint measurement');assert.equal(hash(await fs.readFile('dist/CodeTranspiler.exe')),release,'release EXE changed');
const oldRoutes=JSON.parse(await fs.readFile('outputs/language-route-matrix-v1/routes.json'));
const rowKey=r=>`${r.source}>${r.target}:${r.via}:${r.feature}`;const oldRouteMap=new Map(oldRoutes.paths.map(r=>[rowKey(r),r]));
const transitions=(rows,before,key)=>{const m={};for(const r of rows){const previous=before.get(key(r));if(!previous)continue;const k=(previous.status||previous.overall)+'>'+r.status;m[k]=(m[k]||0)+1}return m};
const summary={schema:'joint-semantic-matrix-v17',features,languages:L,source_tree_hash:identity.source_tree_hash,release_sha256:release,release_unchanged:true,baseline_measurement_sha256:hash(oldBytes),baseline_routes_sha256:hash(await fs.readFile('outputs/language-route-matrix-v1/routes.json')),reused_native_proof:reusedNativeProof,token_limit:65536,
 language_pair_counts:L.map(source=>L.map(target=>count(direct.filter(r=>r.source===source&&r.target===target)))),
 dimensions:{fixtures:fixtures.length,direct:direct.length,routes:routes.length,roundtrips:roundtrips.length,distinct_intermediate_texts:intermediates.size,distinct_native_texts_checked:checkedNative.size},counts:{direct:count(direct),routes:count(routes),roundtrips:count(roundtrips)},
 transitions:{direct:transitions(direct,oldById,r=>r.id),routes:transitions(routes,oldRouteMap,rowKey)},new_routes_against_current_direct:routes.filter(r=>r.status==='PASS'&&r.direct!=='PASS').length,
 matrix_checks:matrixChecks,policy:'Actual intermediate text decoded. No original-source recovery. PASS is original fixture output with native final compiler and runtime; first/source native flags separate. UNKNOWN never support. Changed token budget and source version reported. No release EXE build.'};
await fs.writeFile(path.join(output,'summary.json'),JSON.stringify(summary,null,2));
await fs.writeFile(path.join(output,'measurements.json'),JSON.stringify({summary,adjacency,adjacency_squared:squared,direct,routes,roundtrips},null,2));
await fs.writeFile(path.join(output,'semantic_trees.json'),JSON.stringify([...originals].map(([id,r])=>({id,error:r.error||null,semantic:r.semantic||null})),null,2));
await fs.writeFile(path.join(output,'source_identity.json'),JSON.stringify(identity,null,2));
await fs.writeFile(path.join(output,'code_evidence.json'),JSON.stringify([...texts].map(([key,t])=>({key,...t})),null,2));
await fs.writeFile(path.join(output,'native_evidence.json'),JSON.stringify(await Promise.all([...checkedNative].map(async key=>({key,...await nativeCache.get(key)}))),null,2));
console.log(JSON.stringify(summary,null,2));
