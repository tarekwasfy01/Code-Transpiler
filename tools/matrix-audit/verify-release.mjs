import fs from 'node:fs/promises';
import path from 'node:path';
import {spawn} from 'node:child_process';
import {createHash} from 'node:crypto';
import assert from 'node:assert/strict';
import {sourceIdentity} from './source-identity.mjs';

const directory=process.argv[3]||'outputs/transpiler-audit-v12';
const executable=path.resolve(process.argv[2]||'dist/CodeTranspiler.exe');
const measured=JSON.parse(await fs.readFile(path.join(directory,'measurements.json')));
const identity=await sourceIdentity(process.cwd());assert.equal(identity.source_tree_hash,measured.summary.source_tree_hash,'Source changed after native audit');
const bytes=await fs.readFile(executable),sha256=createHash('sha256').update(bytes).digest('hex');
assert.equal(bytes.toString('ascii',0,2),'MZ');const pe=bytes.readUInt32LE(60);assert.equal(bytes.readUInt32LE(pe),0x4550);assert.equal(bytes.readUInt16LE(pe+4),0x8664);assert.equal(bytes.readUInt16LE(pe+24+68),2);
function run(args,input=''){return new Promise((resolve,reject)=>{
 const child=spawn(executable,args,{windowsHide:true});let stdout='',stderr='';
 const timer=setTimeout(()=>{child.kill();reject(Error('EXE timeout'))},120000);
 child.stdout.setEncoding('utf8');child.stderr.setEncoding('utf8');child.stdout.on('data',v=>stdout+=v);child.stderr.on('data',v=>stderr+=v);
 child.on('error',reject);child.on('close',exit=>{clearTimeout(timer);if(exit!==0)reject(Error(`EXE exit ${exit}: ${stderr}`));else resolve({stdout,stderr})});child.stdin.on('error',()=>{});child.stdin.end(input);
})}
const version=await run(['version']);assert(version.stdout.includes(directory.includes('v16')?'v0.9':directory.includes('v15')?'v0.8':directory.includes('v14')?'v0.7':directory.includes('v13')?'v0.6':'v0.5'));
const runtimes=await run(['runtimes']);assert.equal(runtimes.stdout.trim().split(/\r?\n/).length,12);assert(runtimes.stdout.includes('external compiler required'));
const records=measured.records.filter(r=>r.source_code);
const requests=records.map(r=>({id:r.id,source:r.source,target:r.target,code:r.source_code}));
const batch=await run(['transpile-batch'],JSON.stringify(requests));const results=JSON.parse(batch.stdout);assert.equal(results.length,requests.length);
const lookup=new Map(results.map(r=>[r.id,r]));assert.equal(lookup.size,requests.length);
for(const r of records){const actual=lookup.get(r.id);assert(actual,r.id);if(r.emit==='PASS'){assert(!actual.error,`${r.id}: ${actual.error}`);assert.equal(actual.code,r.target_code,r.id)}else{assert(actual.error,r.id);assert.equal(actual.error,r.reason,r.id)}}
const sample=path.resolve('.audit-cache/release-sample.R');await fs.writeFile(sample,'print(2 + 3 * 4)\n');
const embedded=await run(['run',sample]);assert.equal(embedded.stdout.trim(),'14');
const invalid=JSON.parse((await run(['transpile-batch'],JSON.stringify([{id:'invalid',source:'unknown',target:'go',code:'x=1'}]))).stdout);assert(invalid[0].error);
const result={executable,sha256,bytes:bytes.length,format:'PE x64 Windows GUI',source_tree_hash:identity.source_tree_hash,version:version.stdout.trim(),exact_exe_route_matches:records.length,exact_exe_translation_matches:records.filter(r=>r.emit==='PASS').length,exact_exe_error_matches:records.filter(r=>r.emit==='FAIL').length,version_test:'PASS',runtime_listing:'PASS',embedded_execution:'PASS',invalid_input_rejected:'PASS',native_matrix:measured.summary.overall,policy:'Generated output and expected errors from this exact EXE match the audited routes exactly; separate counters distinguish emitted code from rejection. Native execution totals refer to the matrix; missing toolchains remain UNKNOWN. GUI interaction is a separate check.'};
await fs.writeFile(path.join(directory,'release_verification.json'),JSON.stringify(result,null,2));console.log(JSON.stringify(result,null,2));
