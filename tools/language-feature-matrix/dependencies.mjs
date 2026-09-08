import fs from 'node:fs/promises';
import path from 'node:path';
import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';

const dir=path.resolve(process.argv[2]||'outputs/semantic-matrix-v17');
const raw=await fs.readFile(path.join(dir,'measurements.json')),data=JSON.parse(raw);
const trees=JSON.parse(await fs.readFile(path.join(dir,'semantic_trees.json')));
const C=['source_decode','runtime_envelope','operation_decode','value_contract','binding_scope','call_evaluation','control_flow','iteration','helper_lowering','sparse_storage','target_emit','native_environment'];
const kinds=[...new Set(trees.flatMap(t=>t.semantic?.evidence.nodes.map(n=>n.kind)||[]))].sort();
const matrix=(r,c)=>Array.from({length:r},()=>Array(c).fill(0));
const mul=(a,b)=>a.map(row=>b[0].map((_,j)=>row.reduce((n,v,k)=>n+v*b[k][j],0)));
const transpose=a=>a[0].map((_,j)=>a.map(r=>r[j]));
const threshold=a=>a.map(r=>r.map(v=>Number(v!==0)));
const W=matrix(kinds.length,C.length);
const links={literal:['value_contract'],identifier:['binding_scope'],assign:['binding_scope','value_contract'],parameter:['binding_scope'],function:['binding_scope','call_evaluation','helper_lowering'],call:['call_evaluation','value_contract'],binary:['value_contract','operation_decode'],unary:['value_contract','operation_decode'],index:['iteration','value_contract'],if:['control_flow'],while:['control_flow'],for:['iteration'],iteration:['iteration'],repeat:['control_flow'],break:['control_flow'],continue:['control_flow'],return:['control_flow']};
for(const [kind,components] of Object.entries(links))if(kinds.includes(kind))for(const c of components)W[kinds.indexOf(kind)][C.indexOf(c)]=1;
const X=trees.map(t=>kinds.map(k=>t.semantic?.evidence.nodes.filter(n=>n.kind===k).length||0));
const B=mul(X,W); // Actual node-kind counts projected to common components.
const A=matrix(C.length,C.length);
const edges={source_decode:['sparse_storage'],operation_decode:['runtime_envelope','value_contract'],call_evaluation:['binding_scope','value_contract'],iteration:['control_flow','binding_scope','value_contract'],helper_lowering:['control_flow','call_evaluation'],target_emit:['value_contract']};
for(const [a,bs] of Object.entries(edges))for(const b of bs)A[C.indexOf(a)][C.indexOf(b)]=1;
const closure=A.map((r,i)=>r.map((v,j)=>Number(v||i===j)));
for(let k=0;k<C.length;k++)for(let i=0;i<C.length;i++)for(let j=0;j<C.length;j++)closure[i][j]=Number(closure[i][j]||(closure[i][k]&&closure[k][j]));
const required=threshold(mul(B,closure));
const treeIndex=new Map(trees.map((t,i)=>[t.id,i]));
const rows=[...data.direct.map(r=>({...r,kind:'direct'})),...data.routes.map(r=>({...r,kind:'route'})),...data.roundtrips.map(r=>({...r,kind:'roundtrip'}))];
const dependencies=[],faults=[],status=[];const stageCounts={};
function diagnosticComponents(r){
 if(r.status==='PASS')return [];
 const text=r.reason||'';
 if(text.startsWith('RESOURCE_LIMIT:'))return ['sparse_storage'];
 if(text.startsWith('DECODE_HELPER:'))return ['helper_lowering'];
 if(text.startsWith('DECODE_OPERATION:'))return ['operation_decode'];
 if(text.startsWith('DECODE_BINDING:'))return ['binding_scope'];
 if(text.startsWith('DECODE_EVALUATION:'))return ['call_evaluation'];
 if(text.startsWith('DECODE_CONTROL:'))return ['control_flow'];
 if(text.startsWith('DECODE_'))return ['operation_decode'];
 if(r.stage==='intermediate_decode')return ['source_decode'];
 if(r.stage==='first_emit'||r.stage==='direct_emit'||r.stage==='second_emit')return ['target_emit'];
 if(r.status==='UNKNOWN')return ['native_environment'];
 // An output/compile mismatch alone does not isolate a unique implementation
 // cause. These are explicitly candidate components, not proven root causes.
 return ['target_emit','value_contract'];
}
const XTState=matrix(C.length,3),FTState=matrix(C.length,3),stateCounts=[0,0,0];
for(let i=0;i<rows.length;i++){
 const r=rows[i],ti=treeIndex.get(r.source+':'+r.feature);assert(ti!==undefined);
 const demand=required[ti].slice();for(const c of ['source_decode','sparse_storage','target_emit','native_environment'])demand[C.indexOf(c)]=1;
 if(r.kind!=='direct')for(const c of ['runtime_envelope','operation_decode'])demand[C.indexOf(c)]=1;
 const state=['PASS','FAIL','UNKNOWN'].indexOf(r.status);assert(state>=0);stateCounts[state]++;status.push(state);stageCounts[r.stage]=(stageCounts[r.stage]||0)+1;
 demand.forEach((v,c)=>{if(v){dependencies.push([i,c,1]);XTState[c][state]++}});
 for(const c of new Set(diagnosticComponents(r))){const j=C.indexOf(c);faults.push([i,j,1]);FTState[j][state]++}
}
// Independent enumeration checks all source projection cells and closure
// reachability, then all component-by-state sums. No priority weights.
let checks=0;
for(let i=0;i<trees.length;i++)for(let c=0;c<C.length;c++){assert.equal(B[i][c],(trees[i].semantic?.evidence.nodes||[]).filter(n=>(links[n.kind]||[]).includes(C[c])).length);checks++}
for(let i=0;i<C.length;i++){const reached=new Set([i]),queue=[i];for(let q=0;q<queue.length;q++)A[queue[q]].forEach((v,j)=>{if(v&&!reached.has(j)){reached.add(j);queue.push(j)}});for(let j=0;j<C.length;j++){assert.equal(closure[i][j],Number(reached.has(j)));checks++}}
for(let c=0;c<C.length;c++)for(let s=0;s<3;s++){assert.equal(XTState[c][s],dependencies.filter(([i,j])=>j===c&&status[i]===s).length);assert.equal(FTState[c][s],faults.filter(([i,j])=>j===c&&status[i]===s).length);checks+=2}
assert.equal(stateCounts.reduce((a,b)=>a+b,0),rows.length);
const report={schema:'shared-component-dependencies-v1',measurement_sha256:createHash('sha256').update(raw).digest('hex'),source_tree_hash:data.summary.source_tree_hash,components:C,node_kinds:kinds,
 source_ids:trees.map(t=>t.id),node_kind_counts:X,node_to_component:W,source_component_counts:B,component_adjacency:A,component_closure:closure,source_dependencies:required,
 row_ids:rows.map(r=>`${r.kind}:${r.source}>${r.via?r.via+'>':''}${r.target}:${r.feature}`),
 requirements:{storage:'coo',rows:rows.length,cols:C.length,entries:dependencies},diagnostic_candidates:{storage:'coo',rows:rows.length,cols:C.length,entries:faults},
 state_axes:['PASS','FAIL','UNKNOWN'],state_vector:status,requirements_transpose_times_state:XTState,diagnostics_transpose_times_state:FTState,component_cooccurrence:mul(transpose(threshold(B)),threshold(B)),
 state_counts:stateCounts,stage_counts:stageCounts,verification:{status:'PASS',checks},
 policy:'B=X*W uses actual AST nodes. D=bool(B*closure(A)). Sparse path requirements and diagnostic candidates are separate. X^T*state and F^T*state are associations; overlapping columns must not be summed as unique failures. Native failure candidates are not proven causes. All weights 1; no priorities. A covered dependency is not a fixed failure; only new native measurements prove improvements.'};
await fs.writeFile(path.join(dir,'dependencies.json'),JSON.stringify(report,null,2));
const s=data.summary;
const md=['# Gemeinsamer Semantik- und Matrixblock V17','',
 'Alle fünf Arbeitsbereiche sind im Quellstand verbunden: ausführbarer gemeinsamer AST, Decoder für erzeugte Operationen, Abhängigkeitsmatrizen, dünn besetzte Graphen und gemeinsame Direkt-/Rück-/Umwegprüfungen. Das ist eine Erweiterung des belegten Teilbereichs, kein vollständiger Universalübersetzer.',
 '', '| Prüfung | PASS | FAIL | UNKNOWN |','|---|---:|---:|---:|',...Object.entries(s.counts).map(([k,v])=>`| ${k} | ${v.PASS} | ${v.FAIL} | ${v.UNKNOWN} |`),
 '', `Untersucht: ${s.dimensions.fixtures} Originalbeispiele, ${s.dimensions.direct} direkte Wege, ${s.dimensions.routes} Wege über eine andere Sprache, ${s.dimensions.roundtrips} Rückwege. ${s.matrix_checks} unabhängige Routen-/Ergebnisprüfungen bestanden.`,
 '', '## Gemeinsame Abhängigkeiten','',`Die Pfadmatrix hat ${rows.length} × ${C.length} Zellen und speichert ${dependencies.length} belegte Einträge. Die Diagnosematrix speichert ${faults.length} Kandidatenzuordnungen. ${checks} unabhängige Projektions-, Erreichbarkeits- und Summenprüfungen bestanden.`,
 '', '| Gemeinsamer Bestandteil | PASS-Bezug | FAIL-Bezug | UNKNOWN-Bezug |','|---|---:|---:|---:|',...C.map((c,i)=>`| ${c} | ${XTState[i].join(' | ')} |`),
 '', 'Mehrfachzuordnungen sind Abhängigkeiten, keine zusätzlichen Fehler. Die Diagnosematrix lokalisiert beobachtete Fehlerstufen; bei Laufzeit-/Ausgabefehlern bleiben mehrere Ursachen möglich. Keine Gewichtung nach Prioritäten.',
 '', '## Grenzen','',
 '- Der ausführbare AST wird direkt weitergegeben; vorhandene konservative Quellparser bleiben als Frontends erhalten. Der Parser ist nicht vollständig ersetzt.',
 '- Der Decoder prüft das genaue Laufzeitpräfix und alle erzeugten Programm-Token. Änderungen am Programm werden gelesen; veränderte oder unbekannte Laufzeithelfer erhalten keine pauschale Freigabe.',
 '- Generierte Funktionshelfer, komplexe Zustandsmaschinen und manche Schleifenformen sind noch nicht vollständig rückübersetzbar. Die Diagnosematrix weist die verbleibenden Fälle aus.',
 '- Wertevertrag: bestehende dynamische Werte und binary64. Vollständige Quelltypen, Integerbreiten, Closures, Ausnahmen, Nebenläufigkeit und Fremdbibliotheken sind nicht bewiesen.',
 '- PASS belegt den ursprünglichen Ausgabevertrag des jeweiligen Beispiels mit nativem Endprogramm. Native Quell- und Zwischenprüfung bleiben getrennte Angaben. Unveränderte Zielbytes dürfen bereits gemessene native Belege wiederverwenden.',
 '- Der frühere Lauf war auf 2.048 Token begrenzt, dieser auf 65.536. UNKNOWN→PASS umfasst daher auch neu messbare Fälle und ist nicht mit FAIL→PASS gleichzusetzen.',
 '', '## Liefergrenze','',`Keine auslieferbare EXE gebaut. Bestehende EXE unverändert: SHA-256 ${s.release_sha256}. Quellstand: ${s.source_tree_hash}.`,
 '', 'Details und Übergänge stehen in summary.json; jeder Einzelweg in measurements.json; echte AST-Projektionen in semantic_trees.json; erzeugte Texte und Compilerbelege in code_evidence.json und native_evidence.json.'];
await fs.writeFile(path.join(dir,'STAND.md'),md.join('\n')+'\n');
console.log(JSON.stringify({status:'PASS',rows:rows.length,components:C.length,nonzero_dependencies:dependencies.length,checks},null,2));
