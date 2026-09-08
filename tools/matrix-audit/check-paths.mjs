import fs from 'node:fs/promises';
import path from 'node:path';
import {spawn} from 'node:child_process';
import {createHash} from 'node:crypto';
const root=process.cwd(),dir=path.resolve('outputs/transpiler-audit-2026-08-30');
const candidates=JSON.parse(await fs.readFile(path.join(dir,'two_step_candidates.json'),'utf8'));
const measurements=JSON.parse(await fs.readFile(path.join(dir,'measurements.json'),'utf8'));
const sourceFiles=['internal/manytomany/manytomany.go','internal/backend/backend.go','internal/backend/parser.go','internal/backend/codegen_generic.go','internal/backend/targets.go','internal/backend/matrix_generated.go'];
const sourceHash=createHash('sha256').update((await Promise.all(sourceFiles.map(p=>fs.readFile(p)))).map(x=>createHash('sha256').update(x).digest('hex')).join('\n')).digest('hex');
if(sourceHash!==measurements.summary.source_tree_hash)throw new Error('Source changed: repeat baseline audit first.');
const env={...process.env,GOCACHE:path.resolve('.audit-cache/go-build'),PYTHONDONTWRITEBYTECODE:'1'};
async function command(cmd,args,options={}){return new Promise(resolve=>{
 let stdout='',stderr='',settled=false;
 const child=spawn(cmd,args,{cwd:root,env,windowsHide:true,stdio:['pipe','pipe','pipe']});
 const done=r=>{if(settled)return;settled=true;clearTimeout(timer);resolve({...r,stdout,stderr,command:[cmd,...args]});};
 const timer=setTimeout(()=>{child.kill();done({exit:null,timeout:true});},options.timeout??45000);
 child.stdout.on('data',b=>stdout+=b.toString());child.stderr.on('data',b=>stderr+=b.toString());child.on('error',e=>{stderr+=e.message;done({exit:null,unavailable:true});});child.on('close',exit=>done({exit}));child.stdin.on('error',()=>{});child.stdin.end(options.input??'');
});}
const adapter=path.resolve('.audit-cache/matrix-audit.exe');
const build=await command('go',['build','-o',adapter,'./tools/matrix-audit']);if(build.exit!==0)throw new Error(build.stderr);
const first=await command(adapter,[],{input:JSON.stringify(candidates.map(c=>({id:c.id,source:c.source,target:c.intermediate,code:c.original_source})))});
if(first.exit!==0)throw new Error(first.stderr);
const firstMap=new Map(JSON.parse(first.stdout).map(r=>[r.id,r]));
for(const c of candidates){const r=firstMap.get(c.id);if(r.error||r.code!==c.intermediate_code)throw new Error('First leg drift: '+c.id);}
const batch=await command(adapter,[],{input:JSON.stringify(candidates.map(c=>({id:c.id,source:c.intermediate,target:c.target,code:firstMap.get(c.id).code})))});
if(batch.exit!==0)throw new Error(batch.stderr);
const second=new Map(JSON.parse(batch.stdout).map(r=>[r.id,r]));
const cache=new Map(),extensions={python:'.py',go:'.go',rust:'.rs',cpp:'.cpp',c:'.c',java:'.java'};
const python=process.env.AUDIT_PYTHON,jdk=process.env.AUDIT_JAVA_HOME;
if(!python||!jdk)throw new Error('Set AUDIT_PYTHON and AUDIT_JAVA_HOME.');
function execute(target,code){const key=target+'-'+createHash('sha256').update(code).digest('hex').slice(0,20);if(!cache.has(key))cache.set(key,run(target,code,key));return cache.get(key);}
async function run(t,code,key){
 const folder=path.resolve('.audit-cache/path-checks',key);await fs.mkdir(folder,{recursive:true});
 const file=path.join(folder,t==='java'?'Main.java':'program'+extensions[t]),exe=path.join(folder,'program.exe');await fs.writeFile(file,code);
 let c;
 if(t==='python')c=await command(python,['-c','import ast,sys;ast.parse(open(sys.argv[1],encoding="utf-8").read())',file]);
 else if(t==='go')c=await command('go',['build','-o',exe,file]);
 else if(t==='rust')c=await command('rustc',['-A','warnings',file,'-o',exe]);
 else if(t==='cpp')c=await command('g++',['-std=c++17',file,'-o',exe]);
 else if(t==='c')c=await command('gcc',['-std=c11',file,'-o',exe]);
 else if(t==='java')c=await command(path.join(jdk,'bin','javac.exe'),[file]);
 else return {compile:'UNKNOWN',run:'UNKNOWN',reason:'toolchain unavailable'};
 if(c.exit!==0)return {compile:c.timeout||c.unavailable?'UNKNOWN':'FAIL',run:'UNKNOWN',compile_detail:c};
 const r=t==='python'?await command(python,[file],{timeout:6000}):t==='java'?await command(path.join(jdk,'bin','java.exe'),['-cp',folder,'Main'],{timeout:6000}):await command(exe,[],{timeout:6000});
 return {compile:'PASS',run:r.timeout||r.unavailable?'UNKNOWN':r.exit===0?'PASS':'FAIL',compile_detail:c,run_detail:r,stdout:r.stdout};
}
function matches(output,c){const s=output.trim().replace(/^\[1\]\s*/,'');if(c.kind==='string')return s===c.expected||s===JSON.stringify(c.expected);if(c.kind==='boolean')return /^(true|false|TRUE|FALSE|True|False|0|1)$/.test(s)&&(['true','TRUE','True','1'].includes(s)===c.expected);return s!==''&&Number.isFinite(Number(s))&&Math.abs(Number(s)-c.expected)<1e-10;}
const records=[];let i=0;
await Promise.all(Array.from({length:4},async()=>{while(i<candidates.length){const c=candidates[i++],r=second.get(c.id);let stages;
 if(r.error)stages={emit:'FAIL',compile:'UNKNOWN',run:'UNKNOWN',output:'UNKNOWN'};
 else{const e=await execute(c.target,r.code);stages={emit:'PASS',...e,output:e.run==='PASS'?(matches(e.stdout,c)?'PASS':'FAIL'):'UNKNOWN'};}
 const statuses=['emit','compile','run','output'].map(k=>stages[k]);
 records.push({...c,final_code:r.code??null,reason:r.error??'',...stages,overall:statuses.includes('FAIL')?'FAIL':statuses.every(s=>s==='PASS')?'PASS':'UNKNOWN'});
}}));
records.sort((a,b)=>a.id.localeCompare(b.id));
const successful=records.filter(r=>r.overall==='PASS');
const distinct=new Set(successful.map(r=>`${r.source}>${r.target}:${r.feature}`));
const summary={source_hash:sourceHash,candidates:records.length,pass:successful.length,fail:records.filter(r=>r.overall==='FAIL').length,unknown:records.filter(r=>r.overall==='UNKNOWN').length,recovered_distinct_cases:distinct.size,
 native_source_candidates:records.filter(r=>r.native_source_verified).length,native_source_pass:successful.filter(r=>r.native_source_verified).length,
 native_source_recovered_cases:new Set(successful.filter(r=>r.native_source_verified).map(r=>`${r.source}>${r.target}:${r.feature}`)).size,
 stages:Object.fromEntries(['emit','compile','run','output'].map(stage=>[stage,Object.fromEntries(['PASS','FAIL','UNKNOWN'].map(s=>[s,records.filter(r=>r[stage]===s).length]))])),
 note:'Validated only these regenerated two-step fixture paths. No production routing change and no general language-composition guarantee.'};
await fs.writeFile(path.join(dir,'two_step_validation.json'),JSON.stringify({summary,records},null,2));
console.log(JSON.stringify(summary,null,2));
console.log('SUCCESSFUL CASES',JSON.stringify(successful.map(({id,expected,stdout,native_source_verified})=>({id,expected,stdout,native_source_verified})),null,2));
