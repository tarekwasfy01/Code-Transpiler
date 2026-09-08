import fs from 'node:fs/promises';
import path from 'node:path';
import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';

const directory=process.argv[2]||'outputs/transpiler-audit-v11';
const previous=process.argv[3];
const bytes=await fs.readFile(path.join(directory,'measurements.json'));
const {summary,records}=JSON.parse(bytes);
const version=summary.features.length>69?'V16':summary.features.length>65?'V15':summary.features.length>57?'V14':summary.features.length>49?'V13':summary.features.length>41?'V12':'V11';
const runtimePackagePresent=summary.source_manifest?.some(f=>f.path==='internal/runtimeassets/runtimeassets.go');
const stages=['emit','compile','run','output'];
const languages=summary.languages,features=summary.features.map(f=>f.id);
const pairs=languages.flatMap(s=>languages.filter(t=>t!==s).map(t=>s+'>'+t));
assert.equal(records.length,pairs.length*features.length);
assert.equal(new Set(records.map(r=>r.id)).size,records.length);
const ordered=pairs.flatMap(p=>features.map(f=>p+':'+f));
const lookup=new Map(records.map(r=>[r.id,r]));
const rows=ordered.map(id=>{assert(lookup.has(id),id);return lookup.get(id)});
const columns=[];
for(const l of languages)columns.push('source:'+l);
for(const l of languages)columns.push('target:'+l);
for(const f of features)columns.push('feature:'+f);
for(const l of languages)for(const f of features)columns.push('source_feature:'+l+':'+f);
for(const l of languages)for(const f of features)columns.push('target_feature:'+l+':'+f);
const columnIndex=new Map(columns.map((x,i)=>[x,i]));
const X={format:'COO',shape:[rows.length,columns.length],row:[],column:[],value:[]};
const K=[],E=[],U=[],N=[];
const gram=new Map();
const projectedFailure=columns.map(()=>stages.map(()=>0));
const projectedKnown=columns.map(()=>stages.map(()=>0));
for(const [i,r] of rows.entries()) {
 const active=[`source:${r.source}`,`target:${r.target}`,`feature:${r.feature}`,`source_feature:${r.source}:${r.feature}`,`target_feature:${r.target}:${r.feature}`].map(x=>columnIndex.get(x));
 assert(active.every(Number.isInteger));
 for(const c of active){X.row.push(i);X.column.push(c);X.value.push(1)}
 const k=stages.map(s=>Number(r[s]!=='UNKNOWN')),e=stages.map(s=>Number(r[s]==='FAIL')),u=k.map(v=>1-v);
 K.push(k);E.push(e);U.push(u);N.push(Number(r.source_validated));
 for(let j=0;j<stages.length;j++){assert.equal(k[j]+u[j],1);assert(e[j]<=k[j])}
 for(const a of active){
  for(let j=0;j<stages.length;j++){projectedFailure[a][j]+=e[j];projectedKnown[a][j]+=k[j]}
  for(const b of active){const key=a*columns.length+b;gram.set(key,(gram.get(key)||0)+1)}
 }
}
const G={format:'COO',shape:[columns.length,columns.length],row:[],column:[],value:[]};
for(const [key,value] of [...gram].sort((a,b)=>a[0]-b[0])){G.row.push(Math.floor(key/columns.length));G.column.push(key%columns.length);G.value.push(value)}
// X contains only independent identifiers. Outcomes are projected AFTER X is
// constructed; neither success nor failure labels are input features.
const byFeature=features.map(feature=>{
 const selected=rows.filter(r=>r.feature===feature);
 return {feature,...Object.fromEntries(['PASS','FAIL','UNKNOWN'].map(s=>[s,selected.filter(r=>r.overall===s).length])),native_source:selected.filter(r=>r.source_validated).length};
});
let comparison=null;
if(previous){
 const before=JSON.parse(await fs.readFile(path.join(previous,'measurements.json')));
 const old=new Map(before.records.map(r=>[r.id,r]));
 const transitions=Object.fromEntries(['PASS','FAIL','UNKNOWN'].flatMap(a=>['PASS','FAIL','UNKNOWN'].map(b=>[a+'>'+b,0])));
 for(const r of rows){if(old.has(r.id))transitions[old.get(r.id).overall+'>'+r.overall]++}
 comparison={previous,previous_source_hash:before.summary.source_tree_hash,transitions,
  repaired_failures:transitions['FAIL>PASS'],regressions:transitions['PASS>FAIL'],lost_evidence:transitions['PASS>UNKNOWN'],
  note:'FAIL to UNKNOWN is not a repair. UNKNOWN to FAIL means newly observed failure. Comparisons refer to the same case IDs.'};
}
const result={schema:'evidence-matrix-'+version.toLowerCase(),measurement_sha256:createHash('sha256').update(bytes).digest('hex'),source_tree_hash:summary.source_tree_hash,
 rows:ordered,columns,stages,design:X,observed:K,failure:E,unknown:U,native_source:N,gram:G,
 projected_failure:projectedFailure,projected_known:projectedKnown,by_feature:byFeature,comparison,
 calculations:['K + U = 1','E <= K','G = transpose(X) * X','projected_failure = transpose(X) * E','projected_known = transpose(X) * K'],
 policy:'All weights are 1. Counts are descriptive evidence, not priorities or learned translation rules. Source-unvalidated contracts remain explicitly marked. No full-language coverage claim.',
 semantic_completion:false,build_policy:'Experimental EXE requested explicitly; build and exact-file verification are separate from compatibility coverage.',open_limits:['Unmeasured semantics and unavailable native toolchains','Restricted source header recognizers and retained R expression parser','Incomplete target runtimes',...(!runtimePackagePresent?['Missing runtimeassets build package']:[])]};
await fs.writeFile(path.join(directory,'evidence_matrix.json'),JSON.stringify(result));
const md=['# Gleichgewichtete Fehlermatrix '+version,'',`Quelle: ${summary.source_tree_hash}`,``,`${pairs.length} gerichtete Sprachpaare × ${features.length} Fallklassen × ${stages.length} Stufen = ${rows.length*stages.length} beobachtbare Stufenzellen.`,
 `Die tatsächlich gespeicherte Designmatrix X hat ${X.shape.join(' × ')} Elemente (${X.value.length} Nichtnullwerte). Ihre Spalten enthalten nur Quelle, Ziel, Fallklasse und deren Wechselwirkungen.`,
 '', 'K kennzeichnet Beobachtungen; E enthält nachgewiesene Fehler; U = 1 − K bleibt unbekannt. G = XᵀX und XᵀE sind gespeichert. Alle Gewichte sind 1. Große Dimensionen sind kein Beweis für Übersetzungsfähigkeit.',
 '', '| Fallklasse | PASS | FAIL | UNKNOWN | Quelle nativ geprüft (Routen) |','|---|---:|---:|---:|---:|',...byFeature.map(r=>`| ${r.feature} | ${r.PASS} | ${r.FAIL} | ${r.UNKNOWN} | ${r.native_source} |`),
 '',...(comparison?[`Gleiche Fälle gegenüber vorher: ${comparison.repaired_failures} FAIL → PASS; ${comparison.regressions} PASS → FAIL; ${comparison.lost_evidence} PASS → UNKNOWN.`, 'FAIL → UNKNOWN zählt nicht als Reparatur.','']:[]),
 'Keine universelle Kompatibilitätsfreigabe: unbekannte Sprachmerkmale, der R-Ausdrucksparser und unvollständige Zielruntimes bleiben offen. Ein ausdrücklich gewünschter experimenteller EXE-Bau wird separat geprüft.',runtimePackagePresent?'Das bisher fehlende Runtimepaket ist im gemessenen Quellenstand vorhanden.':'Das Runtimepaket fehlt in diesem gemessenen Quellenstand.'];
await fs.writeFile(path.join(directory,'FEHLERMATRIX.md'),md.join('\n')+'\n');
console.log(JSON.stringify({design:X.shape,nonzero:X.value.length,stage_cells:rows.length*4,overall:summary.overall,by_feature:byFeature.filter(r=>r.FAIL),comparison},null,2));
