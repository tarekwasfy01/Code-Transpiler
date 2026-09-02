import fs from 'node:fs/promises';
import path from 'node:path';
import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';
import {sourceIdentity} from '../matrix-audit/source-identity.mjs';

const directory=process.argv[2]||'outputs/language-route-matrix-v1';
const bytes=await fs.readFile(path.join(directory,'routes.json'));
const data=JSON.parse(bytes),L=data.languages;
const proof=JSON.parse(await fs.readFile(path.join(directory,'source_equivalence.json')));
assert.equal(proof.status,'PASS');assert.equal(proof.current_source_before.source_tree_hash,data.source_tree_hash);
assert.equal(proof.measurement_sha256,data.source_measurement_sha256);
assert.equal((await sourceIdentity(process.cwd())).source_tree_hash,data.source_tree_hash);
const stages=[...new Set(data.paths.map(p=>p.stage))].sort();
// X: path x intermediate-language; Y: path x gate/stage. C = X^T Y
// is accumulated sparsely and checked independently by filtered witnesses.
const gateMatrix=L.map(()=>stages.map(()=>0));
for(const p of data.paths)gateMatrix[L.indexOf(p.via)][stages.indexOf(p.stage)]++;
for(let i=0;i<L.length;i++)for(let j=0;j<stages.length;j++)assert.equal(gateMatrix[i][j],data.paths.filter(p=>p.via===L[i]&&p.stage===stages[j]).length);
assert.equal(gateMatrix.flat().reduce((a,b)=>a+b,0),data.paths.length);
const limited=data.paths.filter(p=>p.stage==='resource_limit').length;
const otherUnknown=data.paths.filter(p=>p.status==='UNKNOWN'&&p.stage!=='resource_limit').length;
const passed=data.paths.filter(p=>p.status==='PASS');
const nativeAll=passed.filter(p=>p.first_native&&p.source_native).length;
const uniquePairs=new Set(passed.map(p=>p.source+'>'+p.target));
const changes={source:'internal/matrixir/model.go; internal/matrixir/lexical.go',operation:'batch dense relation matrix allocation',scope:'Lexical graph only; final dense storage remains quadratic; canonical statement graph still uses incremental additions',equivalent_target_texts:proof.byte_exact_translation_matches};
const releaseSha=createHash('sha256').update(await fs.readFile('dist/CodeTranspiler.exe')).digest('hex');
assert.equal(releaseSha,proof.release_after.sha256,'release changed after no-build instruction');
const summary={route_matrix_sha256:createHash('sha256').update(bytes).digest('hex'),source_tree_hash:data.source_tree_hash,
 dimensions:data.dimensions,counts:data.counts,resource_unknown:limited,other_unknown:otherUnknown,
 intermediate_languages_with_verified_paths:L.filter(l=>passed.some(p=>p.via===l)),unique_pairs_with_verified_paths:uniquePairs.size,
 all_three_languages_native: nativeAll,newly_rescued_paths:data.new_verified_routes,
 rejected_despite_independent_native_edges:data.rejected_despite_native_edge_candidates,
 gate_matrix:{formula:'X^T * Y',rows:L,columns:stages,values:gateMatrix,verification:'PASS'},
 changes,release_unchanged:true,release_sha256:releaseSha,
 scope:'PASS refers to native final output against an original fixture contract. Every first-leg text is byte-identical to the measured V16 result, proven independently from current source. Intermediate/source execution flags do not become native proofs through graph reachability. Gains count paths, not unique language pairs.'};
await fs.writeFile(path.join(directory,'summary.json'),JSON.stringify(summary,null,2));
const text=['# Arbeitsstand: Sprachmerkmale und Zwischenrouten','',
 '**Kein weiterer Release-Bau.** Die vorhandene EXE blieb nach der entsprechenden Anweisung unverändert. Die neuere gemeinsame Matrix-Allokation ist ausschließlich im Quellcode enthalten.',
 '', '## Gemeinsame Änderung','',
 'Der lexikalische Graph legt alle Tokenknoten in einem Schritt an und erweitert seine fünf Beziehungsmatrizen einmalig. Bisher erfolgte die vollständige Vergrößerung und Kopie pro Token. Vorhandene Kanten, IDs und unabhängige Wertvektoren bleiben erhalten. Der Kopieraufwand dieses Aufbauschritts sinkt von kubisch auf quadratisch; dichte Matrizen benötigen weiterhin quadratischen Speicher.',
 '', 'Pakettests und Gleichheitsprüfungen bestanden. Ein einzelner 512-Knoten-Benchmark meldete rund 1,80 GB insgesamt allokierte Bytes beim schrittweisen Aufbau gegenüber 10,77 MB beim gemeinsamen Aufbau. Das ist kein allgemeiner Laufzeitfaktor für den Transpiler und kein Maß seines Spitzenarbeitsspeichers. Rohwerte: graph-bulk-benchmark.txt.',
 '', `**${proof.byte_exact_translation_matches.toLocaleString('de-DE')} bisherige Übersetzungen aus dem aktuellen Quellcode sind bytegleich** mit der nativ geprüften V16-Messung. Der Quellhash des neuen Stands und der alte Messhash stehen getrennt in source_equivalence.json.`,
 '', '## Sprachmerkmale','',
 '13 Sprachen × 64 Merkmale, getrennte Quell- und Zielmasken; 75 Fallklassen werden durch eine Inzidenzmatrix auf die Merkmale abgebildet. 53.184 unabhängige Zellprüfungen bestanden. Es gibt keine pauschalen Sprachfähigkeitsbehauptungen: beispielsweise geprüfte lokale Variablen bedeuten keine vollständige Closure-Semantik.',
 '', '## Tatsächliche Zwischenrouten','',
 `${data.features.length} Fallklassen × 156 gerichtete Sprachpaare × 11 Zwischensprachen = **${data.paths.length.toLocaleString('de-DE')} Kombinationszellen**. Ressourcengrenzen beenden eine Zelle vor der vollständigen Übersetzung und bleiben unbekannt.`,
 '', `**${data.counts.PASS.toLocaleString('de-DE')} PASS · ${data.counts.FAIL.toLocaleString('de-DE')} FAIL · ${data.counts.UNKNOWN.toLocaleString('de-DE')} UNKNOWN.**`,
 '', `Unbekannt: ${limited.toLocaleString('de-DE')} wegen der einheitlichen Grenze von ${data.token_limit} Token, ${otherUnknown.toLocaleString('de-DE')} weitere ohne abgeschlossenen nativen Endnachweis.`,
 '', `Belegte Zwischenwege existieren für ${uniquePairs.size} gerichtete Sprachpaare im untersuchten Korpus. Erfolgreiche Zwischensprachen: ${summary.intermediate_languages_with_verified_paths.join(', ')||'keine'}. **${data.new_verified_routes} zuvor direkt nicht bestandene Fälle wurden zusätzlich durch einen Umweg erschlossen.** Ein alternativer Weg zu einem schon funktionierenden Direktfall ist keine neue Sprachunterstützung.`,
 '', `${data.rejected_despite_native_edge_candidates.toLocaleString('de-DE')} Wege scheiterten trotz zweier für sich nativ bestandener Einzelkanten. A² ist deshalb nur die Kandidatenmatrix. Die Freigabemaske basiert auf der tatsächlichen zweiten Übersetzung und deren Endausgabe, immer verglichen mit dem ursprünglichen Quellvertrag.`,
 '', `Von den bestandenen Wegen besitzen ${nativeAll} einen nativen Nachweis für Quelle, Zwischenprogramm und Ziel. Insbesondere ersetzt ein bestandener Endnachweis über R keine fehlende native R-Prüfung. Die drei Nachweise bleiben pro Weg getrennt.`,
 '', '## Gemeinsame offene Ursachen','',
 '- Mitgenerierte Laufzeitbibliotheken werden beim erneuten Lesen noch nicht allgemein in die gemeinsame AST zurückübersetzt. Ein Text, der als Zielprogramm kompiliert, liegt nicht automatisch im unterstützten Quellsubset.',
 '- Ein Umweg von einer eifrig auswertenden Sprache über R kann Argumenteffekte verlieren. Nachgewiesenes Beispiel: statt „99, dann 7“ erscheint nur „7“. Solche Wege werden durch die ursprüngliche Erwartung abgewiesen; eine vollständige Lösung braucht eine gemeinsame Darstellung von Auswertungsreihenfolge und verzögerter Auswertung.',
 '- Fehlende Compiler und Speichergrenzen bleiben UNKNOWN. Parserersatz, allgemeine Closure-Umgebungen, vollständige Typ-/Speichersemantik und beliebige Kombinationen von Merkmalen sind nicht bewiesen.',
 '', '## Nutzung','',
 '`node tools/language-feature-matrix/find-routes.mjs python go arithmetic`',
 '', '`node tools/language-feature-matrix/find-routes.mjs python go feature:loop_break`',
 '', 'Alle Zwischenwege werden gleich gewichtet. Mehrere Anforderungen werden über ihre Masken geschnitten. Merkmale ohne Beispiele bleiben unbekannt. Die Abfrage baut nichts und leitet neue Programme nicht ungeprüft um.',
 '', 'Nachweise: routes.json, ROUTENMATRIX.md, summary.json, source_equivalence.json; Sprachmerkmale: ../language-feature-matrix-v1/SPRACHMERKMALE.md und language_features.json.',
 '', `Aktueller Quellhash: ${data.source_tree_hash}`,`Unveränderte EXE-SHA256: ${releaseSha}`];
await fs.writeFile(path.join(directory,'STAND.md'),text.join('\n')+'\n');
console.log(JSON.stringify({...summary,gate_matrix:undefined},null,2));
