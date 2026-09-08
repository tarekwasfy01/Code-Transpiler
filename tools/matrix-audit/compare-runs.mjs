import fs from 'node:fs/promises';
import path from 'node:path';
const beforeDir=path.resolve('outputs/transpiler-audit-before-cpp-precedence');
const afterDir=path.resolve('outputs/transpiler-audit-2026-08-30');
const before=JSON.parse(await fs.readFile(path.join(beforeDir,'measurements.json'),'utf8'));
const after=JSON.parse(await fs.readFile(path.join(afterDir,'measurements.json'),'utf8'));
const bm=new Map(before.records.map(r=>[r.id,r])),am=new Map(after.records.map(r=>[r.id,r]));
if(bm.size!==am.size)throw new Error('Record count changed');
const stages=['emit','compile','run','output','overall'],transitions={},changed=[];
for(const stage of stages)transitions[stage]={};
for(const [id,b] of bm){const a=am.get(id);if(!a)throw new Error('Missing '+id);let diff=false;
 for(const stage of stages){const key=b[stage]+'>'+a[stage];transitions[stage][key]=(transitions[stage][key]??0)+1;if(b[stage]!==a[stage])diff=true;}
 if(diff||b.target_code!==a.target_code)changed.push({id,source:b.source,target:b.target,feature:b.feature,before:{emit:b.emit,compile:b.compile,run:b.run,output:b.output,overall:b.overall,actual:b.actual,target_code:b.target_code,reason:b.reason},after:{emit:a.emit,compile:a.compile,run:a.run,output:a.output,overall:a.overall,actual:a.actual,target_code:a.target_code,reason:a.reason}});
}
const regressions=changed.filter(r=>r.before.overall==='PASS'&&r.after.overall!=='PASS'||r.before.overall==='UNKNOWN'&&r.after.overall==='FAIL');
const improvements=changed.filter(r=>r.before.overall!=='PASS'&&r.after.overall==='PASS');
const result={before_hash:before.summary.source_tree_hash,after_hash:after.summary.source_tree_hash,record_count:bm.size,changed_records:changed.length,improvements:improvements.length,regressions:regressions.length,transitions,improved_ids:improvements.map(r=>r.id),regression_ids:regressions.map(r=>r.id),changed};
await fs.writeFile(path.join(afterDir,'cpp_precedence_delta.json'),JSON.stringify(result,null,2));
console.log(JSON.stringify({...result,changed:undefined},null,2));
