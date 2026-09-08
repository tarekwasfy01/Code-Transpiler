import fs from 'node:fs/promises';
import path from 'node:path';
import {spawn} from 'node:child_process';
import {createHash} from 'node:crypto';
import {sourceIdentity} from '../matrix-audit/source-identity.mjs';

// Validate current source against the already measured V16 target texts without
// building or modifying the release EXE. go run uses only its temporary adapter.
const root=process.cwd();
const measuredPath=path.resolve(process.argv[2]||'outputs/transpiler-audit-v16/measurements.json');
const outputPath=path.resolve(process.argv[3]||'outputs/language-route-matrix-v1/source_equivalence.json');
const sha=bytes=>createHash('sha256').update(bytes).digest('hex');
const measuredBytes=await fs.readFile(measuredPath);
const measured=JSON.parse(measuredBytes);
const records=measured.records.filter(record=>typeof record.source_code==='string'&&record.source_code.length>0);
const requests=records.map(record=>({id:record.id,source:record.source,target:record.target,code:record.source_code}));
const before=await sourceIdentity(root);
const releasePath=path.resolve('dist/CodeTranspiler.exe');
async function releaseIdentity(){try{const bytes=await fs.readFile(releasePath);return {path:releasePath,sha256:sha(bytes),bytes:bytes.length}}catch(error){if(error.code==='ENOENT')return null;throw error}}
const releaseBefore=await releaseIdentity();
const env={...process.env,GOCACHE:path.resolve('.audit-cache/go-build')};
const command=['go','run','./tools/matrix-audit'];
function run(){return new Promise((resolve,reject)=>{
 const child=spawn(command[0],command.slice(1),{cwd:root,env,windowsHide:true});
 let stdout='',stderr='',settled=false;
 const finish=(error,result)=>{if(settled)return;settled=true;clearTimeout(timer);error?reject(error):resolve(result)};
 const timer=setTimeout(()=>{child.kill();finish(Error('current source adapter exceeded 180 seconds'))},180000);
 child.stdout.setEncoding('utf8');child.stderr.setEncoding('utf8');
 child.stdout.on('data',part=>stdout+=part);child.stderr.on('data',part=>stderr+=part);
 child.on('error',error=>finish(error));child.on('close',exit=>finish(null,{exit,stdout,stderr}));
 child.stdin.on('error',()=>{});child.stdin.end(JSON.stringify(requests));
})}
let runResult,results=[],invocationError=null;
try{runResult=await run();if(runResult.exit!==0)throw Error(`adapter exit ${runResult.exit}: ${runResult.stderr}`);results=JSON.parse(runResult.stdout);if(!Array.isArray(results))throw Error('adapter output is not an array')}
catch(error){invocationError=String(error.stack||error)}
const after=await sourceIdentity(root),releaseAfter=await releaseIdentity();
const lookup=new Map(),issues=[];let translationMatches=0,errorMatches=0;
if(new Set(requests.map(request=>request.id)).size!==requests.length)issues.push({kind:'duplicate_measurement_identifiers'});
for(const result of results){if(lookup.has(result.id))issues.push({id:result.id,kind:'duplicate_response'});lookup.set(result.id,result)}
if(results.length!==requests.length)issues.push({kind:'response_count',expected:requests.length,actual:results.length});
for(const record of records){
 const actual=lookup.get(record.id);
 if(!actual){issues.push({id:record.id,kind:'missing_response'});continue}
 if(record.emit==='PASS'){
  if(actual.error||actual.code!==record.target_code){issues.push({id:record.id,kind:'translation_mismatch',error:actual.error||null,expected_sha256:sha(record.target_code||''),actual_sha256:typeof actual.code==='string'?sha(actual.code):null})}
  else translationMatches++;
 }else if(record.emit==='FAIL'){
  if(!actual.error||actual.error!==record.reason)issues.push({id:record.id,kind:'error_mismatch',expected:record.reason,actual:actual.error||null});
  else errorMatches++;
 }else issues.push({id:record.id,kind:'unclassified_emission',emit:record.emit});
}
const expectedIds=new Set(requests.map(request=>request.id));
for(const result of results)if(!expectedIds.has(result.id))issues.push({id:result.id,kind:'unexpected_response'});
const sourceStable=before.source_tree_hash===after.source_tree_hash;
const releaseStable=JSON.stringify(releaseBefore)===JSON.stringify(releaseAfter);
const measurementsStable=sha(await fs.readFile(measuredPath))===sha(measuredBytes);
const report={
 schema:'source-equivalence-v1',measured_at:new Date().toISOString(),
 status:!invocationError&&issues.length===0&&sourceStable&&releaseStable&&measurementsStable?'PASS':'FAIL',
 measured_file:measuredPath,measurement_sha256:sha(measuredBytes),
 measured_source_tree_hash:measured.summary.source_tree_hash,
 current_source_before:before,current_source_after:after,
 source_stable:sourceStable,measurements_stable:measurementsStable,
 release_before:releaseBefore,release_after:releaseAfter,release_unchanged:releaseStable,
 request_count:requests.length,response_count:results.length,
 byte_exact_translation_matches:translationMatches,byte_exact_error_matches:errorMatches,
 command,adapter_exit:runResult?.exit??null,adapter_stderr:runResult?.stderr??null,invocation_error:invocationError,
 reference_native_matrix:measured.summary.overall,
 policy:'Current source adapter invoked once via go run, with all measured source-coded routes. Exact code/error comparison against V16 measurements; no new native target executions and no new release EXE build. Matching output preserves the measured target-text evidence only; it does not certify unmeasured inputs or full language compatibility.',
 issue_count:issues.length,issues
};
await fs.mkdir(path.dirname(outputPath),{recursive:true});await fs.writeFile(outputPath,JSON.stringify(report,null,2));
console.log(JSON.stringify({...report,current_source_before:{source_tree_hash:before.source_tree_hash},current_source_after:{source_tree_hash:after.source_tree_hash}},null,2));
if(report.status!=='PASS')process.exitCode=1;
