import fs from 'node:fs/promises';
import {spawn} from 'node:child_process';
const python=process.env.AUDIT_PYTHON;if(!python)throw Error('AUDIT_PYTHON required');
const fixtures=JSON.parse(await fs.readFile((process.argv[2]||'outputs/transpiler-audit-v11')+'/fixtures.json'));
function run(command,args,input=''){return new Promise(resolve=>{
 const p=spawn(command,args,{windowsHide:true,env:{...process.env,PYTHONIOENCODING:'utf-8'}});let out='',error='',done=false;
 const finish=(exit)=>{if(done)return;done=true;clearTimeout(timer);resolve({exit,out,error})};
 const timer=setTimeout(()=>{p.kill();error+='timeout';finish(null)},10000);
 p.stdout.setEncoding('utf8');p.stderr.setEncoding('utf8');p.stdout.on('data',s=>out+=s);p.stderr.on('data',s=>error+=s);
 p.on('error',e=>{error+=e.message;finish(null)});p.on('close',finish);p.stdin.on('error',()=>{});p.stdin.end(input);
})}
const requests=fixtures.map(f=>({id:f.source+':'+f.feature,source:f.source,target:'python',code:f.code}));
const bridge=await run('.audit-cache/matrix-audit.exe',[],JSON.stringify(requests));if(bridge.exit!==0)throw Error(bridge.error);
const responses=JSON.parse(bridge.out),results=[];let next=0;
await Promise.all(Array.from({length:4},async()=>{while(next<responses.length){const i=next++,r=responses[i],f=fixtures[i];
 const actual=r.error?{exit:null,out:'',error:r.error}:await run(python,['-c',r.code]);
 let value=actual.out.trim().replace(/^\[1\]\s*/gm,'').replace(/\r/g,'');
 const match=f.kind==='number'?value!==''&&Math.abs(Number(value)-f.expected)<1e-10:f.kind==='boolean'?['True','true','TRUE','1'].includes(value)===f.expected&&/^(True|False|true|false|TRUE|FALSE|0|1)$/.test(value):value===f.expected||value===JSON.stringify(f.expected);
 results.push({id:r.id,pass:actual.exit===0&&match,expected:f.expected,actual:value,error:actual.error});
}}));
const failures=results.filter(r=>!r.pass);
await fs.writeFile('.audit-cache/pipeline-check.json',JSON.stringify({policy:'Python target diagnostic only; not a substitute for the full native matrix',results},null,2));
console.log(JSON.stringify({pass:results.length-failures.length,fail:failures.length,failures},null,2));
if(failures.length)process.exitCode=1;
