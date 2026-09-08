import fs from 'node:fs/promises';
import path from 'node:path';
import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';

// Evidence projection only. No compiler or executable is built by this tool.
const input=path.resolve(process.argv[2]||'outputs/transpiler-audit-v16');
const output=path.resolve(process.argv[3]||'outputs/language-feature-matrix-v1');
const bytes=await fs.readFile(path.join(input,'measurements.json'));
const measured=JSON.parse(bytes), {summary,records}=measured;
const baselines=JSON.parse(await fs.readFile(path.join(input,'source_baselines.json')));
const fixtures=JSON.parse(await fs.readFile(path.join(input,'fixtures.json')));
const model=await fs.readFile('internal/matrixir/model.go','utf8');
const core=[...model.match(/var Features = \[\.\.\.\]string\{([\s\S]*?)\n\}/)[1].matchAll(/"([^"]+)"/g)].map(m=>m[1]);
assert.equal(core.length,32);
const languages=summary.languages, cases=summary.features;
const groups={
 arithmetic:['arithmetic','grouping','unary_negative'], binding:['binding','function_local'],
 reassignment:['reassignment','statement_sequence','function_branch_join'],division:['division'],
 boolean:['boolean','logical_not','logical_combination','nested_logic'],string_comment:['string_comment'],
 string_keywords:['string_keywords','text_tokens','text_delimiters'],
 comparison:['comparison','compare_equal','compare_not_equal','compare_less_equal','compare_greater_equal'],
 multiline:['multiline'],grouping:['grouping','unary_negative'],
 if_else:['if_else',...cases.filter(f=>f.id.startsWith('function_branch')||f.id.startsWith('function_early')||f.id==='function_nested_return').map(f=>f.id)],
 while:['while',...cases.filter(f=>f.id.startsWith('function_while')).map(f=>f.id)],
 for:['for','symbolic_range',...cases.filter(f=>f.id.startsWith('function_for')).map(f=>f.id)],
 function:cases.filter(f=>f.id.startsWith('function')||f.id.includes('argument')||f.id==='call_composition').map(f=>f.id),
 index:['index','two_indexes','expression_index'],integer_division:['integer_division'],
 scope:['function_local','function_branch_local','function_branch_join'],
 lazy_eval:['discarded_argument','used_effect_argument','repeated_effect_argument'],
 short_circuit:['short_circuit_effect','nested_logic'],
};
const extra=[
 ['scalar_negation','Skalare Negation',['unary_negative']],
 ['logical_negation','Logische Negation',['logical_not']],
 ['logical_composition','Logische Verknüpfung',['logical_combination','nested_logic']],
 ['equality','Gleichheit',['compare_equal']],
 ['inequality','Ungleichheit',['compare_not_equal']],
 ['ordered_comparison','Geordnete Vergleiche',['comparison','compare_less_equal','compare_greater_equal']],
 ['string_token_preservation','Textinhalt erhalten',['text_tokens','text_delimiters','string_keywords','string_comment']],
 ['computed_index','Berechnete Indizes',['expression_index','two_indexes']],
 ['range_bounds','Bereichsgrenzen',['for','symbolic_range']],
 ['call_composition','Aufrufkomposition',['call_composition']],
 ['argument_effect_once','Argumenteffekt einmal',['used_effect_argument','repeated_effect_argument']],
 ['unused_argument_effect','Unbenutzte Argumente',['discarded_argument']],
 ['local_binding','Lokale Bindungen',['function_local','function_branch_local']],
 ['branch_merge','Zusammengeführte Zweige',['function_branch_join']],
 ['early_return','Frühe Rückgabe',['function_early_true','function_early_false','function_while_return','function_for_return']],
 ['nested_branch','Verschachtelte Zweige',['function_nested_return']],
 ['loop_break','Schleifenabbruch',['function_while_break','function_for_break']],
 ['loop_continue','Iteration überspringen',['function_while_continue','function_for_continue']],
 ['loop_nesting','Verschachtelte Schleifen',['function_while_nested','function_for_nested']],
 ['loop_effect','Schleifeneffekte',['function_while_effect','function_for_effect']],
 ['zero_iteration','Leere Schleife',['function_while_zero']],
 ['iterable_function','Iterable in Funktionen',cases.filter(f=>f.id.startsWith('function_for')).map(f=>f.id)],
 ['recursive_calls','Rekursive Aufrufe',[]],
 ['closure_environment','Closure-Umgebungen',[]],
 ['numeric_coercion','Vollständige numerische Koerzierung',[]],
 ['vector_recycling','Vektorrecycling',[]],
 ['missing_value_logic','NA-/NaN-Logik',[]],
 ['integer_width','Zahlenbreite und Überlauf',[]],
 ['exception_cleanup','Ausnahme und Aufräumen',[]],
 ['async_await','Async und Await',[]],
 ['pattern_matching','Pattern Matching',[]],
 ['memory_lifetime','Speicherlebensdauer',[]],
];
// Model references identify relevant implementation, not complete semantics.
const refs={
 arithmetic:['internal/backend/expression_matrix.go'],boolean:['internal/backend/expression_matrix.go'],
 if_else:['internal/backend/flow_matrix.go'],while:['internal/backend/state_matrix.go'],for:['internal/backend/iteration_matrix.go'],
 function:['internal/backend/binding_matrix.go'],scope:['internal/backend/state_matrix.go'],lazy_eval:['internal/backend/call_matrix.go'],
 short_circuit:['internal/backend/expression_matrix.go'],range_bounds:['internal/matrixir/range_matrix.go'],
 iterable_function:['internal/backend/iteration_matrix.go'],argument_effect_once:['internal/backend/binding_matrix.go'],
 unused_argument_effect:['internal/backend/call_matrix.go'],branch_merge:['internal/backend/flow_matrix.go'],
 early_return:['internal/backend/flow_matrix.go'],loop_break:['internal/backend/flow_matrix.go'],loop_continue:['internal/backend/flow_matrix.go'],
 loop_nesting:['internal/backend/state_matrix.go'],loop_effect:['internal/backend/state_matrix.go'],local_binding:['internal/backend/state_matrix.go'],
};
const features=[...core.map(id=>({id,label:cases.find(f=>f.id===id)?.label||id,contracts:groups[id]||[],axis:'core'})),
 ...extra.map(([id,label,contracts])=>({id,label,contracts,axis:'detail'}))];
assert.equal(features.length,64);assert.equal(new Set(features.map(f=>f.id)).size,64);
for(const f of features){f.model_references=refs[f.id]||[];f.full_support_claim=false;for(const c of f.contracts)assert(cases.some(k=>k.id===c),c)}
// B: fixture-class x language-feature incidence. Reserved general categories
// are never executable contracts, even when a narrower subset was measured.
const B=cases.map(c=>features.map(f=>Number(f.contracts.includes(c.id))));
const matrix=()=>languages.map(()=>features.map(()=>0));
const source={pass:matrix(),fail:matrix(),unknown:matrix(),declared_contracts:matrix()};
const target={emit:matrix(),pass:matrix(),fail:matrix(),unknown:matrix(),declared_contracts:matrix()};
const sourceCounts=languages.map(()=>cases.map(()=>({PASS:0,FAIL:0,UNKNOWN:0})));
for(const f of fixtures){const l=languages.indexOf(f.source),c=cases.findIndex(k=>k.id===f.feature),b=baselines[f.source+':'+f.feature];assert(b);
 const status=b.expected_match===true?'PASS':b.expected_match===false||b.compile==='FAIL'||b.run==='FAIL'?'FAIL':'UNKNOWN';sourceCounts[l][c][status]++}
const targetCounts=languages.map(()=>cases.map(()=>({PASS:0,FAIL:0,UNKNOWN:0,EMIT:0})));
const pairCells=languages.map(()=>languages.map(()=>features.map(()=>({pass:0,fail:0,unknown:0,both_native_pass:0}))));
for(const r of records){if(!r.source_code)continue;const s=languages.indexOf(r.source),t=languages.indexOf(r.target),c=cases.findIndex(k=>k.id===r.feature);
 targetCounts[t][c][r.overall]++;if(r.emit==='PASS')targetCounts[t][c].EMIT++;
 for(let j=0;j<features.length;j++)if(B[c][j]){const v=pairCells[s][t][j];v[r.overall.toLowerCase()]++;if(r.overall==='PASS'&&r.source_validated)v.both_native_pass++}}
// Count matrix multiplication C * B: no weights or language priorities.
for(let l=0;l<languages.length;l++)for(let c=0;c<cases.length;c++)for(let j=0;j<features.length;j++)if(B[c][j]){
 for(const status of ['pass','fail','unknown']){source[status][l][j]+=sourceCounts[l][c][status.toUpperCase()];target[status][l][j]+=targetCounts[l][c][status.toUpperCase()]}
 target.emit[l][j]+=targetCounts[l][c].EMIT;
}
for(let l=0;l<languages.length;l++)for(let j=0;j<features.length;j++){
 source.declared_contracts[l][j]=source.pass[l][j]+source.fail[l][j]+source.unknown[l][j];
 target.declared_contracts[l][j]=target.pass[l][j]+target.fail[l][j]+target.unknown[l][j];
}
const masks=section=>({
 observed:section.pass.map((r,l)=>r.map((v,j)=>Number(v+section.fail[l][j]>0))),
 sample_pass:section.pass.map((r,l)=>r.map((v,j)=>Number(v>0&&section.fail[l][j]===0&&section.unknown[l][j]===0))),
 sample_fail:section.fail.map(r=>r.map(v=>Number(v>0))),
 incomplete:section.unknown.map((r,l)=>r.map((v,j)=>Number(v>0||section.declared_contracts[l][j]===0))),
 full_language_support:matrix(),
});
source.masks=masks(source);target.masks=masks(target);
// Independent audit: recompute each cell by directly selecting record IDs,
// instead of reusing the class-count multiplication.
let checks=0;
for(let l=0;l<languages.length;l++)for(let j=0;j<features.length;j++){
 const ids=new Set(features[j].contracts);
 const selected=records.filter(r=>r.source_code&&r.target===languages[l]&&ids.has(r.feature));
 for(const status of ['pass','fail','unknown']){assert.equal(target[status][l][j],selected.filter(r=>r.overall===status.toUpperCase()).length);checks++}
 assert.equal(target.emit[l][j],selected.filter(r=>r.emit==='PASS').length);checks++;
 const selectedSource=fixtures.filter(f=>f.source===languages[l]&&ids.has(f.feature));
 const direct=selectedSource.map(f=>baselines[f.source+':'+f.feature]);
 assert.equal(source.pass[l][j],direct.filter(b=>b.expected_match===true).length);checks++;
 assert.equal(source.fail[l][j],direct.filter(b=>b.expected_match!==true&&(b.expected_match===false||b.compile==='FAIL'||b.run==='FAIL')).length);checks++;
 assert.equal(source.unknown[l][j],direct.length-source.pass[l][j]-source.fail[l][j]);checks++;
 for(let s=0;s<languages.length;s++){const cell=pairCells[s][l][j],rs=selected.filter(r=>r.source===languages[s]);
  for(const status of ['pass','fail','unknown']){assert.equal(cell[status],rs.filter(r=>r.overall===status.toUpperCase()).length);checks++}
  assert.equal(cell.both_native_pass,rs.filter(r=>r.overall==='PASS'&&r.source_validated).length);checks++;
 }
}
const G=features.map(()=>features.map(()=>0));
for(const row of B)for(let i=0;i<features.length;i++)if(row[i])for(let j=0;j<features.length;j++)G[i][j]+=row[j];
for(let i=0;i<features.length;i++)for(let j=0;j<features.length;j++){assert.equal(G[i][j],cases.filter(c=>features[i].contracts.includes(c.id)&&features[j].contracts.includes(c.id)).length);checks++}
const weights=features.map(()=>1);
const deficits={sample_failure:target.masks.sample_fail.map(r=>r.reduce((a,b)=>a+b,0)),missing_sample_evidence:target.masks.incomplete.map(r=>r.reduce((a,b)=>a+b,0)),full_semantics_unproven:languages.map(()=>features.length)};
const artifact={schema:'language-feature-evidence-v1',measurement_path:path.relative(process.cwd(),input),measurement_sha256:createHash('sha256').update(bytes).digest('hex'),measured_source_tree_hash:summary.source_tree_hash,
 generator_sha256:createHash('sha256').update(await fs.readFile(new URL(import.meta.url))).digest('hex'),languages,features,weights,
 shapes:{language_feature:[13,64],case_feature:[cases.length,64],pair_feature:[13,13,64],directed_pair_feature_cells:13*12*64},
 case_ids:cases.map(c=>c.id),case_feature_incidence:B,feature_cooccurrence:G,source,target,pair_feature:pairCells,deficits,
 calculations:['source_counts = native_source_case_counts * B','target_counts = target_case_counts * B','feature_cooccurrence = transpose(B) * B','deficits = failure_or_unknown_mask * ones'],
 verification:{status:'PASS',independent_cell_checks:checks},
 policy:'Corpus evidence only, not intrinsic language capability or complete feature support. No evidence means UNKNOWN, never unsupported. Source and target evidence are separate; joint routes are directly observed, never inferred by multiplying marginal successes. Features overlap: projected counts cannot be added as unique route counts. All full_language_support masks are zero/unproven. Supplemental EXE probes are not imported. No EXE build or runtime changes.'};
await fs.mkdir(output,{recursive:true});await fs.writeFile(path.join(output,'language_features.json'),JSON.stringify(artifact,null,2));
const status=(section,l,j)=>section.fail[l][j]>0?'FEHLER':section.declared_contracts[l][j]===0?'OFFEN':section.unknown[l][j]>0?'UNGEPRÜFT':'TEILBELEGT';
const md=['# Sprachmerkmalsmatrix','',`**13 Sprachen × 64 Merkmale = 832 Zellen je Quell-/Zielmatrix.** Dazu 156 gerichtete Sprachpaare × 64 Merkmale = 9.984 Paarzellen.`,
 '',`Die ${cases.length} bisherigen Fallklassen werden durch B (${cases.length} × 64) auf 32 gemeinsame und 32 feinere Merkmale projiziert. Alle Gewichte sind 1.`,
 '', 'TEILBELEGT = zugeordnete Beispiele nativ bestanden, keine vollständige Merkmalsunterstützung. UNGEPRÜFT = Beispiele vorhanden, aber native Beobachtungen fehlen. OFFEN = keine zugeordneten ausführbaren Beispiele. FEHLER = mindestens ein gemessener Fehler. Fehlende Beobachtung bedeutet nicht, dass eine Sprache das Merkmal nicht besitzt.',
 '', '## Zielmatrix','',`| Merkmal | ${languages.join(' | ')} |`,`|---|${languages.map(()=>'---').join('|')}|`,
 ...features.map((f,j)=>`| ${f.label} | ${languages.map((_,l)=>status(target,l,j)).join(' | ')} |`),
 '', '## Quellmatrix','',`| Merkmal | ${languages.join(' | ')} |`,`|---|${languages.map(()=>'---').join('|')}|`,
 ...features.map((f,j)=>`| ${f.label} | ${languages.map((_,l)=>status(source,l,j)).join(' | ')} |`),
 '', '## Defizitvektoren','', '| Sprache | Merkmale mit Fehlerbeispiel | Ohne vollständige Beispielbeobachtung | Vollständige Semantik unbewiesen |','|---|---:|---:|---:|',
 ...languages.map((l,i)=>`| ${l} | ${deficits.sample_failure[i]} | ${deficits.missing_sample_evidence[i]} | 64 |`),
 '',`**${checks.toLocaleString('de-DE')} unabhängige Zellprüfungen: PASS.** Die Gegenprüfung selektiert ursprüngliche Fall-IDs direkt, statt dieselben Summenprodukte wiederzuverwenden.`,
 '', 'Die JSON-Datei enthält B, BᵀB, Quell-/Zielzählmatrizen, Beobachtungsmasken, direkt beobachtete Paarzellen, Defizitvektoren und die genaue Fallzuordnung. Quell- und Zielerfolge werden nicht zu unbeobachteten Paarerfolgen kombiniert. Überlappende Merkmale dürfen nicht als zusätzliche Übersetzungen aufsummiert werden.',
 '', 'Modellverweise zeigen vorhandenen Code, keine vollständige Implementierung. Beispiel: lokale Bindungen sind geprüft; allgemeine Closure-Umgebungen bleiben offen. Parameter-Auswertungsbeispiele beweisen keine vollständigen R-Promises. Ganzzahldivision beweist keine vollständige Überlauf- oder Zahlenbreitensemantik.',
 '', 'Diese Ergänzung verändert weder Parser noch Runtime und baut keine EXE. Die Messbasis bleibt V16; zusätzliche Dateiproben gehen nicht in diese Merkmalszählungen ein.',
 '', `Messbasis-Quellhash: ${summary.source_tree_hash}`,`Messdatei-SHA256: ${artifact.measurement_sha256}`];
await fs.writeFile(path.join(output,'SPRACHMERKMALE.md'),md.join('\n')+'\n');
console.log(JSON.stringify({shapes:artifact.shapes,verification:artifact.verification,deficits},null,2));
