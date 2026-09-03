import fs from 'node:fs/promises';
import path from 'node:path';
const dir=path.resolve('outputs/transpiler-audit-2026-08-30');
const c=JSON.parse(await fs.readFile(path.join(dir,'matrix_calculations.json'),'utf8'));
const v=JSON.parse(await fs.readFile(path.join(dir,'two_step_validation.json'),'utf8'));
const pct=n=>(100*n).toLocaleString('de-DE',{maximumFractionDigits:2})+' %';
const labels=Object.fromEntries(JSON.parse(await fs.readFile(path.join(dir,'measurements.json'),'utf8')).summary.features.map(f=>[f.id,f.label]));
const successful=v.records.filter(r=>r.overall==='PASS');
const recovered=[...new Set(successful.map(r=>`${r.source}>${r.target}:${r.feature}`))];
const structural=['if_else','while','for','function'].map(f=>c.by_feature.find(x=>x.feature===f));
const totalStructural=Object.fromEntries(['pass','fail','unknown','known','total'].map(k=>[k,structural.reduce((s,r)=>s+r[k],0)]));
const sourceRows=c.emission_factorization.source_rejected.map((row,i)=>[c.by_source[i].language,row.reduce((a,b)=>a+b,0)]);
if(c.emission_factorization.absolute_residual!==0||c.emission_factorization.nonfactorable.length)throw new Error('Factorization not exact');
for(const f of structural){const a=c.reachability.find(r=>r.feature===f.feature);if(a.A_squared.flat().some(x=>x!==0))throw new Error('Structural graph is not nilpotent');}
if(v.summary.pass+v.summary.fail+v.summary.unknown!==67||recovered.length!==7)throw new Error('Path count mismatch');
const scenario={description:'Hypothetical fixture-only router choosing only the seven freshly verified paths. Not implemented in application. Original matrices unchanged.',
 recovered_case_ids:recovered,total:4992,known:1700,pass:386+recovered.length,fail:1314-recovered.length,unknown:3292,
 reduction_of_confirmed_failures:recovered.length/1314,error_rate_known:(1314-recovered.length)/1700,native_reference_recovered_cases:v.summary.native_source_recovered_cases,
 structural_cases:totalStructural};
await fs.writeFile(path.join(dir,'verified_routing_scenario.json'),JSON.stringify(scenario,null,2));
const featureRows=c.by_feature.map(r=>`| ${labels[r.feature]} | ${r.pass} | ${r.fail} | ${r.unknown} | ${r.error_rate_known===null?'unbekannt':pct(r.error_rate_known)} |`).join('\n');
const pathRows=successful.map(r=>`| ${r.source} → ${r.intermediate} → ${r.target} | ${r.feature} | ${r.native_source_verified?'ja':'nein, expliziter Testvertrag'} |`).join('\n');
const text=`# Durchgerechnete Fehlermatrix

Die bestehende Fehlermatrix wurde mit ganzzahligen Matrixprodukten und expliziten Beobachtungsmasken ausgewertet. Keine Gewichte oder Prioritaeten wurden eingefuehrt. Die Anwendung und die urspruengliche Excel-/Fehlermatrix sind unveraendert.

## 1. Rechenregeln und Gesamtergebnis

B ist die numerische Fehlermatrix, K die Beobachtungsmaske, P = K - B die Matrix der bestandenen konkreten Faelle. Unbekannte B-Eintraege enthalten technisch null, aber K bleibt dort null. Dimension jeweils: 156 × 32.

Mit u als Einsvektor der Laenge 32 gilt:

\`\`\`text
f = B u          Fehler je Sprachpaar
n = K u          bekannte Felder je Sprachpaar
s = P u          bestandene Faelle je Sprachpaar
q = f / n        elementweise; Nullnenner bleibt unbekannt
\`\`\`

Die Rechnung ergibt ${c.total.fail} Fehler, ${c.total.pass} bestandene Faelle und ${c.total.unknown} unbekannte Felder. Bekannt sind ${c.total.known} von ${c.total.total} Feldern (${pct(c.total.coverage)}). Unter den bekannten Feldern betraegt die Fehlerquote ${pct(c.total.error_rate_known)}.

Ueber das gesamte Raster liegt der Fehleranteil zwischen ${pct(c.total.error_lower_bound)} und ${pct(c.total.error_upper_bound)}, je nachdem, wie die unbekannten Felder spaeter ausfallen. Das sind logische Grenzen fuer diesen Testkatalog, keine statistischen Konfidenzintervalle und keine Vollsprachbewertung.

## 2. Exakte Faktorisierung der Frontend-Ablehnungen

Fuer die Uebersetzungsstufe laesst sich B_emit exakt zerlegen:

\`\`\`text
B_emit = L × S

L: 156 × 13   ordnet jeder Sprachpaar-Zeile ihre Quellsprache zu
S:  13 × 32   abgelehnte Quellbeispiele je Sprache und Kategorie

Summe(S) = 85
Summe(L × S) = 1.020
Summe(abs(B_emit - L × S)) = 0
\`\`\`

Jede Ablehnung ist fuer das betreffende Quellbeispiel bei allen zwoelf Zielen identisch. Die 1.020 fehlerhaften Sprachpaar-Felder stammen somit aus 85 abgelehnten Quellbeispielen, jeweils zwoelffach sichtbar. Das sind **nicht automatisch 85 verschiedene Programmierfehler**; mehrere Beispiele koennen dieselbe fehlende Parserfaehigkeit treffen. Die Faktorisierung lokalisiert den gemeinsamen Ausfall, sie beweist keine Kausalitaet.

| Quelle | Abgelehnte Quellbeispiele von 16 | Daraus entstehende Paarfehler |
|---|---:|---:|
${sourceRows.map(([l,n])=>`| ${l} | ${n} | ${n*12} |`).join('\n')}

Die bestaetigten Fehler zerfallen ohne Ueberschneidung nach ihrer ersten fehlerhaften Stufe:

| Stufe | Fehler | Anteil an 1.314 Fehlern |
|---|---:|---:|
${Object.entries(c.stages).map(([k,r])=>`| ${k} | ${r.fail} | ${pct(r.fail/1314)} |`).join('\n')}

## 3. Kategorien und gemeinsame Muster

| Kategorie | PASS | FAIL | UNKNOWN | Fehlerquote unter bekannten Feldern |
|---|---:|---:|---:|---:|
${featureRows}

Zusaetzlich berechnet wurden:

- BᵀB: 32 × 32, gemeinsame Fehler je Kategorienpaar.
- KᵀK: 32 × 32, gemeinsam beobachtete Felder je Kategorienpaar.
- PᵀP: 32 × 32, gemeinsame Erfolge je Kategorienpaar.
- BBᵀ und KKᵀ: je 156 × 156, entsprechende Gemeinsamkeiten zwischen Sprachpaaren.

Diese Matrizen sind in matrix_calculations.json vollstaendig enthalten. Ihre Diagonalen wurden gegen die Kategorie-/Paarzaehlungen geprueft. Gemeinsame Fehler sind keine automatisch bewiesenen gemeinsamen Ursachen; die Beobachtungsmaske muss stets mitgelesen werden.

Exakt gleiche Fehler- **und** Beobachtungsmuster haben Arithmetik, Gleitkommadivision und Klammerung. Ebenso stimmen die Muster fuer if/else und while ueberein. Die 16 offenen Kategorien sind nur deshalb identisch, weil sie vollstaendig unbekannt sind; daraus folgt keine gemeinsame Implementierung oder Kompatibilitaet.

## 4. Verbindungsmatrizen und tatsaechliche Uebersetzungswege

Fuer jede Kategorie wurde eine 13 × 13-Verbindungsmatrix A aufgebaut. A[s,t]=1 bedeutet: der direkte konkrete Test s→t ist bestanden. Eine Kante wurde fuer die Pfadrechnung nur zugelassen, wenn Sollwert und Skalarart mit dem Testvertrag der anschliessenden Quellsprache uebereinstimmen. Die Diagonale ist null; unveraenderte Kopien zaehlen nicht als Uebersetzung.

Bei Ganzzahldivision wurden dadurch drei formal bestandene Kanten von der Komposition ausgeschlossen: ein bestandener Test mit 7/2=3.5 kann nicht ohne Weiteres an einen anderen Quellvertrag mit 7/2=3 angeschlossen werden.

\`\`\`text
(A²)[s,t] = Summe ueber m von A[s,m] × A[m,t]
\`\`\`

Das Produkt zaehlt moegliche Wege ueber eine Zwischensprache. Zusaetzlich wurde die boolesche transitive Huelle berechnet. Fuer bislang fehlgeschlagene direkte Faelle ergeben sich **67 Wege fuer 42 verschiedene Paar-Kategorie-Faelle**. Die laengere Erreichbarkeitsrechnung ergibt hier keine weiteren solchen Faelle.

Das ist zunaechst nur eine Kandidatenliste: Der zweite direkte Test hatte einen handgeschriebenen Quelltext, nicht notwendigerweise den vom ersten Transpiler erzeugten Text. Deshalb wurden alle 67 Wege erneut aufgebaut und ihr tatsaechlicher Zwischen- und Zielcode geprueft. Der Quellhash stimmt mit dem vorherigen Audit ueberein.

| Pruefung der 67 Wege | PASS | FAIL | UNKNOWN / nicht erreicht |
|---|---:|---:|---:|
${Object.entries(v.summary.stages).map(([stage,r])=>`| ${stage} | ${r.PASS} | ${r.FAIL} | ${r.UNKNOWN} |`).join('\n')}
| Gesamter Weg | ${v.summary.pass} | ${v.summary.fail} | ${v.summary.unknown} |

Es bleiben sieben funktionierende Umwege fuer sieben zuvor fehlgeschlagene konkrete Faelle:

| Weg | Kategorie | Native Quell-Referenz vorhanden |
|---|---|---|
${pathRows}

Alle sieben betreffen denselben Vergleich 2 < 3 mit Ziel C++. Die Direktuebersetzung schreibt den Vergleich ohne die noetige Klammerung in einen C++-Ausgabestrom. Der Umweg ueber C erzeugt eine geklammerte Konvertierung und beseitigt in diesen Beispielen den Kompilierungsfehler. Drei Faelle besitzen eine native Quell-Referenz, vier wurden gegen den festgelegten Sollwert geprueft.

Das ist ein Hinweis auf einen begrenzten Fehler im C++-Ausgabegenerator, keine allgemeine semantische Loesung durch Zwischensprachen. Es wurde kein automatisches Routing in die Anwendung eingebaut.

## 5. Rechnerische Grenze fuer Strukturmerkmale

Fuer if/else, while, for und Funktionen gilt im beobachteten Vertragsgraphen jeweils:

\`\`\`text
A² = 0
also A^k = 0 fuer jedes k >= 2
\`\`\`

Es gibt fuer diese vier Kategorien keine Kette aus zwei bestandenen, vertraglich passenden Verbindungen. Damit findet auch eine laengere Multiplikationskette keine Loesung innerhalb dieses Graphen. Nicht beobachtete Verbindungen bleiben unbekannt; das ist kein Unmoeglichkeitsbeweis fuer alle denkbaren Transpiler.

Zusammen betreffen diese Kategorien ${totalStructural.total} Paar-Kategorie-Felder: ${totalStructural.fail} Fehler, ${totalStructural.pass} bestandene Faelle und ${totalStructural.unknown} unbekannte Faelle. Das Ergebnis stuetzt die Architekturdiagnose: Strukturen und ihre Bedeutung muessen im gemeinsamen Frontend/IR und den Zielgeneratoren vorhanden sein. Sie koennen durch das Umordnen vorhandener Verbindungen nicht entstehen.

## 6. Was eine Verwendung der sieben bestaetigten Wege aendern wuerde

Nur als Szenario fuer exakt diese Testfaelle, **nicht als Aenderung der Anwendung**:

| Messwert | Direkt | Mit den sieben geprueften Umwegen |
|---|---:|---:|
| PASS | 386 | ${scenario.pass} |
| FAIL | 1.314 | ${scenario.fail} |
| UNKNOWN | 3.292 | ${scenario.unknown} |
| Fehlerquote unter bekannten Feldern | ${pct(c.total.error_rate_known)} | ${pct(scenario.error_rate_known)} |

Die sieben Wege reduzieren die Zahl bestaetigter Fehler um ${pct(scenario.reduction_of_confirmed_failures)}. Das Szenario ist separat in verified_routing_scenario.json gespeichert. Die urspruenglichen Direktmessungen wurden nicht ueberschrieben.

## Dateien und Nachvollziehbarkeit

- matrix_calculations.json: Summen, Masken, Kategorien-/Paarprodukte, exakte Faktorisierung, Verbindungsmatrizen, Quadrate und Erreichbarkeit.
- two_step_candidates.json: alle 67 berechneten Wege mit Zwischenquellcode.
- two_step_validation.json: tatsaechlich erzeugter Zielcode, Compiler-/Laufzeitausgaben, Status und Referenzart.
- verified_routing_scenario.json: getrenntes Rechenszenario fuer die sieben erfolgreichen Faelle.

Reproduzierbare Berechnung: tools/matrix-audit/calculate.mjs, danach check-paths.mjs fuer Ausfuehrungspruefungen und calculation-report.mjs fuer diesen Bericht. Der Pfadpruefer benoetigt wie der urspruengliche Audit AUDIT_PYTHON und AUDIT_JAVA_HOME sowie die vorhandenen Compiler. Es wurden keine Abhaengigkeiten installiert.
`;
await fs.writeFile(path.join(dir,'BERECHNUNG.md'),text);
console.log(JSON.stringify({verification:'PASS',factorization_residual:0,structural_A_squared_zero:true,path_results:v.summary,scenario},null,2));
