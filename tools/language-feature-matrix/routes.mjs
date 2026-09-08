import fs from 'node:fs/promises';
import path from 'node:path';
import assert from 'node:assert/strict';
import {spawn} from 'node:child_process';
import {createHash} from 'node:crypto';
import {createNativeExecutor,matches} from './native-executor.mjs';
import {sourceIdentity} from '../matrix-audit/source-identity.mjs';

// Actual two-hop evidence: matrix products enumerate candidates, never certify
// semantic composition. No transpiler executable is built or changed here.
const input=path.resolve(process.argv[2]||'outputs/transpiler-audit-v16');
const output=path.resolve(process.argv[3]||'outputs/language-route-matrix-v1');
const hash=v=>createHash('sha256').update(v).digest('hex');
const measuredBytes=await fs.readFile(path.join(input,'measurements.json'));
const measured=JSON.parse(measuredBytes),L=measured.summary.languages;
const allFixtures=JSON.parse(await fs.readFile(path.join(input,'fixtures.json')));
const selected=process.env.ROUTE_FEATURES?.split(',');
const features=measured.summary.features.filter(f=>f.fixture_available&&(!selected||selected.includes(f.id))).map(f=>f.id);
const executable=path.resolve('.audit-cache/matrix-audit.exe');
const executableHash=hash(await fs.readFile(executable));
const liveSource=process.env.ROUTE_LIVE_SOURCE==='1';
const identity=await sourceIdentity(process.cwd());
if(!liveSource)assert.equal(executableHash,measured.summary.adapter_sha256,'Use exactly the measured adapter; this tool does not rebuild it');
const tokenLimit=Number(process.env.ROUTE_MAX_TOKENS||2048);
await fs.mkdir(output,{recursive:true});
const actual=measured.records.filter(r=>features.includes(r.feature)&&r.source_code);
const lookup=new Map(actual.map(r=>[r.id,r]));
const fixtureLookup=new Map(allFixtures.map(f=>[f.source+':'+f.feature,f]));
const execute=createNativeExecutor(process.cwd());
const adjacency=features.map(feature=>L.map(s=>L.map(t=>Number(lookup.get(`${s}>${t}:${feature}`)?.emit==='PASS'))));
const nativeAdjacency=features.map(feature=>L.map(s=>L.map(t=>Number(lookup.get(`${s}>${t}:${feature}`)?.overall==='PASS'))));
const product=A=>A.map(row=>A.map((_,j)=>row.reduce((sum,a,k)=>sum+a*A[k][j],0)));
const candidates=adjacency.map(product),nativeCandidates=nativeAdjacency.map(product);
// Parse each distinct, actual first-leg text once. A parser rejection prevents
// every target for that exact intermediate input, independently of its fixture.
const firstLegs=new Map();
for(const r of actual)if(r.emit==='PASS'){
 const key=r.target+':'+hash(r.target_code);if(!firstLegs.has(key))firstLegs.set(key,{key,source:r.target,code:r.target_code});
}
function adapter(requests){return new Promise((resolve,reject)=>{
 const child=spawn(liveSource?'go':executable,liveSource?['run','./tools/matrix-audit']:[],{windowsHide:true,stdio:['pipe','pipe','pipe'],env:{...process.env,GOCACHE:path.resolve('.audit-cache/go-build'),GOMEMLIMIT:'384MiB',GOGC:'30'}});let out='',err='',settled=false;
 const timer=setTimeout(()=>{child.kill();finish(Error('relay adapter timeout'))},90000);
 function finish(error,value){if(settled)return;settled=true;clearTimeout(timer);error?reject(error):resolve(value)}
 child.stdout.setEncoding('utf8');child.stderr.setEncoding('utf8');
 child.stdout.on('data',v=>{out+=v;if(out.length>128*1024*1024){child.kill();finish(Error('relay response output limit'))}});
 child.stderr.on('data',v=>err+=v);child.on('error',e=>finish(e));
 child.on('close',exit=>{if(exit!==0)return finish(Error(`adapter exit ${exit}: ${err}`));try{const values=JSON.parse(out);assert.equal(values.length,requests.length);finish(null,values)}catch(e){finish(e)}});
 child.stdin.on('error',()=>{});child.stdin.end(JSON.stringify(requests));
})}
async function batches(items,fn,label){const result=new Map();for(let offset=0;offset<items.length;offset+=24){
 const chunk=items.slice(offset,offset+24),values=await adapter(chunk.map(x=>({...fn(x),max_tokens:tokenLimit})));
 for(const v of values){assert(!result.has(v.id));result.set(v.id,v)}
 if(offset%240===0)console.log(`${label}: ${Math.min(offset+24,items.length)}/${items.length}`);
}return result}
const parsed=await batches([...firstLegs.values()],r=>({id:r.key,source:r.source,code:r.code,mode:'canonical'}),'Parse intermediate text');
const secondInputs=new Map(),paths=[];
for(const feature of features)for(const source of L)for(const target of L)if(target!==source)for(const via of L)if(via!==source&&via!==target){
 const first=lookup.get(`${source}>${via}:${feature}`),direct=lookup.get(`${source}>${target}:${feature}`);
 assert(first&&direct);
 const key=via+':'+hash(first.target_code||''),parse=parsed.get(key);
 const row={source,target,via,feature,direct:direct.overall,source_native:first.source_validated,first_emit:first.emit,
 first_native:first.overall==='PASS',intermediate_hash:first.target_code?hash(first.target_code):null};
 if(first.emit!=='PASS'){row.stage='first_emit';row.status='FAIL';row.reason=first.reason}
 else if(parse.error){row.stage=parse.error.startsWith('RESOURCE_LIMIT:')?'resource_limit':'intermediate_parse';row.status=row.stage==='resource_limit'?'UNKNOWN':'FAIL';row.reason=parse.error}
 else{
  const secondKey=key+'>'+target;row.second_key=secondKey;
  if(!secondInputs.has(secondKey))secondInputs.set(secondKey,{key:secondKey,source:via,target,code:first.target_code});
 }
 paths.push(row);
}
assert.equal(paths.length,features.length*13*12*11);
const emitted=await batches([...secondInputs.values()],r=>({id:r.key,source:r.source,target:r.target,code:r.code}),'Translate second leg');
// Compact compiler evidence is keyed by final text, with exact-output reuse of
// existing native execution only when bytes and target language both match.
const nativeByCode=new Map();
for(const r of measured.records)if(r.target_code&&r.execution?.run==='PASS')nativeByCode.set(r.target+':'+hash(r.target_code),{compile:r.compile,run:r.run,stdout:r.actual,provenance:'exact code hash in V16 native measurement',measurement_id:r.id});
const executionCache=new Map();
let next=0,finished=0;
await Promise.all(Array.from({length:3},async()=>{while(next<paths.length){const row=paths[next++];if(row.status)continue;
 const result=emitted.get(row.second_key);assert(result);
 if(result.error){row.stage=result.error.startsWith('RESOURCE_LIMIT:')?'resource_limit':'second_emit';row.status=row.stage==='resource_limit'?'UNKNOWN':'FAIL';row.reason=result.error;continue}
 row.final_hash=hash(result.code);const cacheKey=row.target+':'+row.final_hash;
 if(!executionCache.has(cacheKey))executionCache.set(cacheKey,Promise.resolve(nativeByCode.get(cacheKey)||execute(row.target,result.code)));
 const evidence=await executionCache.get(cacheKey),fixture=fixtureLookup.get(row.source+':'+row.feature);
 row.final_compile=evidence.compile;row.final_run=evidence.run;row.final_evidence=evidence.provenance||'new native target execution';
 row.final_matches=evidence.run==='PASS'?matches(evidence.stdout,fixture):null;
 row.status=evidence.compile==='FAIL'||evidence.run==='FAIL'||row.final_matches===false?'FAIL':row.final_matches===true?'PASS':'UNKNOWN';
 row.stage=row.status==='PASS'?'verified_output':evidence.compile!=='PASS'?'final_compile':evidence.run!=='PASS'?'final_run':'final_output';
 if(row.status==='FAIL')row.reason=evidence.reason||evidence.stderr||`expected ${JSON.stringify(fixture.expected)}, got ${JSON.stringify(evidence.stdout)}`;
 row.actual=evidence.stdout??null;
 if(++finished%240===0)console.log(`Final native checks: ${finished}`);
}}));
assert.equal(hash(await fs.readFile(executable)),executableHash,'adapter changed during routing measurement');
assert.equal((await sourceIdentity(process.cwd())).source_tree_hash,identity.source_tree_hash,'source changed during routing measurement');
const routes=features.map(feature=>L.map(source=>L.map(target=>{
 const rows=paths.filter(r=>r.feature===feature&&r.source===source&&r.target===target);
 return {valid_via:rows.filter(r=>r.status==='PASS').map(r=>r.via),failed_via:rows.filter(r=>r.status==='FAIL').map(r=>r.via),unknown_via:rows.filter(r=>r.status==='UNKNOWN').map(r=>r.via)};
})));
// Validate Boolean matrix candidate counts by independent enumeration, and
// validate each accepted routing bit against actual second-leg output evidence.
let checks=0;
for(let f=0;f<features.length;f++)for(let s=0;s<L.length;s++)for(let t=0;t<L.length;t++){
 assert.equal(candidates[f][s][t],L.filter((_,m)=>adjacency[f][s][m]&&adjacency[f][m][t]).length);checks++;
 for(const via of routes[f][s][t].valid_via){const witness=paths.find(r=>r.feature===features[f]&&r.source===L[s]&&r.target===L[t]&&r.via===via);assert(witness.final_matches===true&&witness.final_compile==='PASS'&&witness.final_run==='PASS');checks++}
}
const counts=Object.fromEntries(['PASS','FAIL','UNKNOWN'].map(s=>[s,paths.filter(r=>r.status===s).length]));
const stages=Object.fromEntries([...new Set(paths.map(r=>r.stage))].map(stage=>[stage,paths.filter(r=>r.stage===stage).length]));
const gains=paths.filter(r=>r.status==='PASS'&&r.direct!=='PASS');
const rejectedDespiteNativeEdges=paths.filter(r=>r.status==='FAIL'&&r.first_native&&lookup.get(`${r.via}>${r.target}:${r.feature}`)?.overall==='PASS').length;
const report={schema:'verified-two-hop-matrix-v1',languages:L,features,source_measurement_sha256:hash(measuredBytes),baseline_source_tree_hash:measured.summary.source_tree_hash,source_tree_hash:identity.source_tree_hash,adapter_sha256:liveSource?null:executableHash,execution_mode:liveSource?'current source via Go runner; no release EXE':'existing measured adapter',token_limit:tokenLimit,
 dimensions:{languages:13,directed_pairs:156,intermediates_per_pair:11,feature_count:features.length,actual_paths:paths.length},counts,stages,
 distinct_intermediate_texts:firstLegs.size,distinct_second_leg_translations:secondInputs.size,distinct_final_native_texts:executionCache.size,
 new_verified_routes:gains.length,rejected_despite_native_edge_candidates:rejectedDespiteNativeEdges,
 adjacency,adjacency_squared:candidates,native_adjacency:nativeAdjacency,native_adjacency_squared:nativeCandidates,routes,paths,
 verification:{status:'PASS',checks},
 policy:'A squared enumerates candidates only. Every accepted via has actual intermediate text parsed and translated and final output natively verified against the ORIGINAL source contract. Existing native results are reused only for byte-identical final code with same target language. Intermediate execution and native source execution are separate flags. No original-source bypass, no embedded provenance recovery, no hidden fallback. Unknown is not supported. No route preference/weight; all verified intermediates are returned. No transpiler EXE built.'};
await fs.writeFile(path.join(output,'routes.json'),JSON.stringify(report,null,2));
const md=['# Geprüfte Zwischenrouten','',`**${features.length} Fallklassen × 156 Sprachpaare × 11 Zwischensprachen = ${paths.length.toLocaleString('de-DE')} tatsächlich untersuchte Wege.**`,
 '',`PASS: ${counts.PASS}; FAIL: ${counts.FAIL}; UNKNOWN: ${counts.UNKNOWN}. Neue geprüfte Wege gegenüber direkt nicht bestandenen Fällen: ${gains.length}.`,
 '', 'A² zählt mögliche Wege. Ein Weg wird erst freigegeben, wenn der erzeugte Zwischentext tatsächlich erneut gelesen und übersetzt wurde und die native Endausgabe zum ursprünglichen Quellvertrag passt. Eine erfolgreiche Kante beweist keine erfolgreiche Zusammensetzung.',
 '',`**${rejectedDespiteNativeEdges} Wege trotz zweier nativ bestandener Einzelkanten abgewiesen.** Die Matrix verhindert damit eine unbegründete Umwegfreigabe.`,
 '', '| Stufe | Wege |','|---|---:|',...Object.entries(stages).map(([k,v])=>`| ${k} | ${v} |`),
 '', '| Zwischensprache | PASS | FAIL | UNKNOWN |','|---|---:|---:|---:|',...L.map(l=>`| ${l} | ${['PASS','FAIL','UNKNOWN'].map(s=>paths.filter(r=>r.via===l&&r.status===s).length).join(' | ')} |`),
 '', `Unabhängige Routen-/Produktprüfung: ${checks} Prüfungen bestanden.`,
 '', 'Die JSON-Datei enthält für jede Fallklasse A, A², native Kandidatenmasken und alle freigegebenen, abgewiesenen und unbekannten Zwischensprachen. Die Endausführung darf aus V16 übernommen werden, wenn Zieltext und Zielsprache exakt übereinstimmen; das wird pro Weg vermerkt. Fehlende Compiler bleiben UNKNOWN.',
 '', `Einheitliche Ressourcengrenze: ${tokenLimit} Token pro Zwischenprogramm. Größere Programme bleiben RESOURCE_UNKNOWN.`,
 '', 'Dieser Nachweis gilt für die gemessenen Beispiele, nicht für beliebigen Quellcode. Keine auslieferbare EXE gebaut; aktuelle Quellprüfung über den Go-Runner verwendet temporäre Werkzeuge.'];
await fs.writeFile(path.join(output,'ROUTENMATRIX.md'),md.join('\n')+'\n');
console.log(JSON.stringify({...report,adjacency:undefined,adjacency_squared:undefined,native_adjacency:undefined,native_adjacency_squared:undefined,routes:undefined,paths:undefined},null,2));
