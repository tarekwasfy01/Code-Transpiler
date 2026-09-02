import fs from 'node:fs/promises';
import path from 'node:path';
import {Workbook,SpreadsheetFile} from '@oai/artifact-tool';

const out=path.resolve('outputs/transpiler-audit-2026-08-30');
const data=JSON.parse(await fs.readFile(path.join(out,'measurements.json'),'utf8'));
const {summary,records}=data;
let delta=null;
try{delta=JSON.parse(await fs.readFile(path.join(out,'cpp_precedence_delta.json'),'utf8'));}catch{}
const {languages,features}=summary;
const index=new Map(records.map(r=>[r.id,r]));
const pairs=languages.flatMap(source=>languages.filter(target=>target!==source).map(target=>({source,target})));
const error=s=>s==='FAIL'?1:s==='PASS'?0:null;
const grids=pairs.map(p=>[p.source,p.target,...features.map(f=>error(index.get(`${p.source}>${p.target}:${f.id}`).overall))]);
const wb=Workbook.create();
const names=['Ueberblick','Delta','Paare','Fehler','Bekannt','Kategorien','Messwerte','Anleitung'];
const sheets=Object.fromEntries(names.map(n=>[n,wb.worksheets.add(n)]));
const col=n=>{let s='';while(n>0){n--;s=String.fromCharCode(65+n%26)+s;n=Math.floor(n/26);}return s;};
function write(s,row,values){s.getRangeByIndexes(row-1,0,values.length,values[0].length).values=values;}
function style(s,lastRow,lastCol){
 s.showGridLines=false;s.freezePanes.freezeRows(4);s.freezePanes.freezeColumns(2);
 const r=s.getRangeByIndexes(0,0,lastRow,lastCol);r.format.font.name='Aptos';r.format.font.size=11;r.format.rowHeight=21;r.format.columnWidth=15;
 const titleCols=lastCol>14?10:lastCol===12?9:lastCol;
 s.getRangeByIndexes(0,0,1,titleCols).merge();s.getRange('A1').format.font.size=20;s.getRange('A1').format.font.bold=true;s.getRange('A1').format.font.color='#17365D';s.getRange('A1').format.rowHeight=34;
 s.getRangeByIndexes(1,0,1,titleCols).merge();s.getRange('A2').format.font.color='#64748B';s.getRange('A2').format.rowHeight=38;s.getRange('A2').format.wrapText=true;
 s.getRangeByIndexes(3,0,1,lastCol).format={fill:'#17365D',font:{bold:true,color:'#FFFFFF'},wrapText:true,rowHeight:44};
}
function cf(s,range){const r=s.getRange(range);r.conditionalFormats.add('cellIs',{operator:'equal',formula:1,format:{fill:'#FEE2E2',font:{color:'#991B1B'}}});r.conditionalFormats.add('cellIs',{operator:'equal',formula:0,format:{fill:'#DCFCE7',font:{color:'#166534'}}});r.conditionalFormats.add('containsBlanks',{format:{fill:'#F1F5F9'}});}

for(const n of ['Fehler','Bekannt']){
 const s=sheets[n];style(s,160,34);
 s.getRange('A1').values=[[n==='Fehler'?'Fehlermatrix | 156 Sprachpaare × 32 Kategorien':'Beobachtungsmaske | Unbekannt bleibt unbekannt']];
 s.getRange('A2').values=[[n==='Fehler'?'0 = konkreter Fall bestanden · 1 = Fehler in mindestens einer Stufe · leer = Ergebnis unbekannt':'1 = Fehlerwert vorhanden · 0 = unbekannt. Diese Maske muss bei jeder Rechnung mitgefuehrt werden.']];
 write(s,4,[['Quelle','Ziel',...features.map(f=>f.id)]]);
 if(n==='Fehler')write(s,5,grids);
 else {
  write(s,5,pairs.map(p=>[p.source,p.target]));
  s.getRange('C5:AH160').formulas=pairs.map((p,i)=>features.map((f,j)=>`=IF(ISNUMBER('Fehler'!${col(j+3)}${i+5}),1,0)`));
 }
 s.getRange('C5:AH160').setNumberFormat('0');
 if(n==='Fehler')cf(s,'C5:AH160');else s.getRange('C5:AH160').conditionalFormats.add('cellIs',{operator:'equal',formula:0,format:{fill:'#F1F5F9',font:{color:'#94A3B8'}}});
 s.tables.add('A4:AH160',true,n+'Tabelle');
}
{
 const s=sheets.Kategorien;style(s,36,4);s.getRange('A1').values=[['Pruefkategorien | Keine Prioritaetsgewichtung']];s.getRange('A2').values=[['Ein Fall je Kategorie und Quelle. Gewicht = 1 fuer alle; 16 Kategorien haben noch keinen Testfall.']];
 write(s,4,[['ID','Kategorie','Pruefvertrag / Grenze','Testfall vorhanden']]);write(s,5,features.map(f=>[f.id,f.label,f.contract,f.fixture_available?'JA':'NEIN']));
 s.getRange('A4:A36').format.columnWidth=23;s.getRange('B4:B36').format.columnWidth=34;s.getRange('C4:C36').format.columnWidth=47;s.getRange('D4:D36').format.columnWidth=23;
 s.tables.add('A4:D36',true,'KategorienTabelle');
}
{
 const s=sheets.Paare;style(s,160,10);s.getRange('A1').values=[['Sprachpaare | Messung und Ungewissheit']];s.getRange('A2').values=[['Fehlerquote bezieht sich ausschliesslich auf bekannte Felder. Min/Max begrenzen den Anteil ueber alle 32 Kategorien.']];
 write(s,4,[['Quelle','Ziel','Kategorien','Bestanden','Fehler','Unbekannt','Abdeckung','Fehlerquote bekannt','Fehleranteil min','Fehleranteil max']]);
 write(s,5,pairs.map(p=>[p.source,p.target]));
 s.getRange('C5:J160').formulas=pairs.map((p,i)=>{const r=i+5;return [`=COLUMNS('Fehler'!C${r}:AH${r})`,`=COUNTIF('Fehler'!C${r}:AH${r},0)`,`=SUM('Fehler'!C${r}:AH${r})`,`=C${r}-SUM('Bekannt'!C${r}:AH${r})`,`=(C${r}-F${r})/C${r}`,`=IF(C${r}-F${r}=0,"",E${r}/(C${r}-F${r}))`,`=E${r}/C${r}`,`=(E${r}+F${r})/C${r}`];});
 s.getRange('C5:F160').setNumberFormat('0');s.getRange('G5:J160').setNumberFormat('0.0%');s.getRange('G4:J160').format.columnWidth=20;
 s.tables.add('A4:J160',true,'PaareTabelle');
}
{
 const s=sheets.Messwerte;style(s,records.length+4,12);s.getRange('A1').values=[['Einzelmessungen | Vier getrennte Pruefstufen']];s.getRange('A2').values=[['PASS/FAIL/UNKNOWN · Native Quelle bestaetigt bedeutet Referenzausfuehrung; andernfalls gilt nur der angegebene Testvertrag. Vollstaendige Belege: measurements.json.']];
 write(s,4,[['Quelle','Ziel','Kategorie','Uebersetzen','Kompilieren','Ausfuehren','Ergebnis','Gesamt','Quelle nativ geprueft','Sollwert','Istwert','Diagnose (Auszug)']]);
 const literal=v=>typeof v==='string'&&v.startsWith('=')?"'"+v:v;
 write(s,5,records.map(r=>[r.source,r.target,r.feature,r.emit,r.compile,r.run,r.output,r.overall,r.source_validated?'JA':'NEIN',literal(r.expected??null),literal(r.actual?.trim()??null),literal((r.reason||'').slice(0,500))]));
 s.getRange(`C4:C${records.length+4}`).format.columnWidth=24;s.getRange(`I4:I${records.length+4}`).format.columnWidth=25;s.getRange(`K4:K${records.length+4}`).format.columnWidth=23;s.getRange(`L4:L${records.length+4}`).format.columnWidth=64;
 for(const [text,fill] of [['FAIL','#FEE2E2'],['PASS','#DCFCE7'],['UNKNOWN','#F1F5F9']])s.getRange(`D5:H${records.length+4}`).conditionalFormats.add('containsText',{text,format:{fill}});
 s.tables.add(`A4:L${records.length+4}`,true,'MesswerteTabelle');
}
{
 const s=sheets.Ueberblick;style(s,43,14);s.freezePanes.unfreeze();s.getRange('A1').values=[['Universal Code Transpiler | Fehlermatrix']];s.getRange('A2').values=[['Quellstand 30.08.2026 · 13 Sprachen · 156 gerichtete Sprachpaare · Kein Nachweis vollstaendiger Sprachkompatibilitaet']];
 write(s,4,[['Sprachpaare','Kategorien','Felder','PASS','FAIL','UNBEKANNT','Abdeckung',...Array(7).fill(null)]]);
 s.getRange('A5:G5').formulas=[[`=COUNTA('Paare'!A5:A160)`,`=COUNTA('Kategorien'!A5:A36)`,`=A5*B5`,`=SUM('Paare'!D5:D160)`,`=SUM('Paare'!E5:E160)`,`=SUM('Paare'!F5:F160)`,`=(D5+E5)/C5`]];
 s.getRange('A5:F5').setNumberFormat('#,##0');s.getRange('G5').setNumberFormat('0.0%');s.getRange('A5:G5').format.font.size=19;s.getRange('A5:G5').format.rowHeight=35;
 s.getRange('A7:N7').merge();s.getRange('A7').values=[['Fehlerquote unter den bekannten Feldern | Zeile = Quelle, Spalte = Ziel']];s.getRange('A7').format.font.bold=true;
 write(s,8,[['Quelle / Ziel',...languages]]);write(s,9,languages.map(l=>[l]));
 const rowFor=(a,b)=>pairs.findIndex(p=>p.source===a&&p.target===b)+5;
 s.getRange('B9:N21').formulas=languages.map(a=>languages.map(b=>a===b?'="—"':`=IF('Paare'!C${rowFor(a,b)}-'Paare'!F${rowFor(a,b)}=0,"",'Paare'!H${rowFor(a,b)})`));
 s.getRange('B9:N21').setNumberFormat('0%');s.getRange('B9:N21').conditionalFormats.add('colorScale',{colors:['#DCFCE7','#FDE68A','#FCA5A5'],thresholds:[0,0.5,1]});
 s.getRange('A23:N23').merge();s.getRange('A23').values=[['Messabdeckung | Anteil bekannter Felder an allen 32 Kategorien']];s.getRange('A23').format.font.bold=true;
 write(s,24,[['Quelle / Ziel',...languages]]);write(s,25,languages.map(l=>[l]));
 s.getRange('B25:N37').formulas=languages.map(a=>languages.map(b=>a===b?'="—"':`='Paare'!G${rowFor(a,b)}`));s.getRange('B25:N37').setNumberFormat('0%');
 s.getRange('B25:N37').conditionalFormats.add('colorScale',{colors:['#F1F5F9','#BAE6FD','#0284C7'],thresholds:[0,0.5,1]});
 for(const r of [8,24])s.getRange(`A${r}:N${r}`).format={fill:'#E2E8F0',font:{bold:true},rowHeight:24};
 s.getRange('A39:N40').merge();s.getRange('A39').values=[['Eine gruene Zelle bestaetigt nur den geprueften Fall. Fehler in einer fruehen Stufe verhindern spaetere Pruefungen; diese bleiben UNKNOWN. Compiler/Laufzeit aktiviert: Python, Go, Rust, C, C++, Java.']];s.getRange('A39:N40').format.wrapText=true;
 s.getRange('B4:N43').format.columnWidth=11;s.getRange('A4:A43').format.columnWidth=20;
}
{
 const s=sheets.Delta;style(s,20,8);s.freezePanes.unfreeze();s.getRange('A1').values=[['Vorher / Nachher | C++-Ausgabeprioritaet']];s.getRange('A2').values=[['Gleicher Testkatalog und gleiche Toolchains. Nur der direkte C++-Ausdruck wurde geklammert.']];
 write(s,4,[['Messwert','Vorher','Nachher','Differenz','Beleg','Quelle','Ziel','Kategorie']]);
 const before=delta?{pass:386,fail:1314,unknown:3292}:{pass:null,fail:null,unknown:null};
 const after=summary.overall;
 write(s,5,[['PASS',before.pass,after.PASS,after.PASS-before.pass,'Gesamtmatrix','','',''],['FAIL',before.fail,after.FAIL,after.FAIL-before.fail,'Gesamtmatrix','','',''],['UNKNOWN',before.unknown,after.UNKNOWN,after.UNKNOWN-before.unknown,'Gesamtmatrix','','',''],['Regressionen',0,delta?.regressions??null,delta?.regressions??null,'Elementweiser Statusvergleich','','','']]);
 const improved=(delta?.improved_ids??[]).map(id=>{const [pair,feature]=id.split(':'),[source,target]=pair.split('>');return ['FAIL → PASS',1,1,0,'Compiler + Ausfuehrung',source,target,feature];});
 if(improved.length)write(s,10,improved);
 s.getRange('A4:H20').format.columnWidth=18;s.getRange('E4:E20').format.columnWidth=30;s.getRange('A4:H20').format.wrapText=true;
 s.getRange('A5:D13').conditionalFormats.add('containsText',{text:'FAIL',format:{fill:'#FEE2E2'}});s.getRange('A5:D13').conditionalFormats.add('containsText',{text:'PASS',format:{fill:'#DCFCE7'}});
}
{
 const s=sheets.Anleitung;style(s,22,4);s.freezePanes.unfreeze();s.getRange('A1').values=[['Rechnen mit Fehlern, ohne Unbekanntes zu verstecken']];s.getRange('A2').values=[['Die Matrix misst das aktuelle Programm. Sie ist kein Ersatz fuer Parser oder semantische Uebersetzungsregeln.']];
 write(s,4,[['Begriff','Definition','Rechnung / Folge','Grenze']]);
 const rows=[
 ['F','Fehler: 0 bestanden, 1 Fehler, leer unbekannt','156 Zeilen × 32 Kategorien','Gilt nur fuer die konkreten Testfaelle'],
 ['K','Beobachtungsmaske: 1 bekannt, 0 unbekannt','K = ISNUMBER(F)','Fehlendes Wissen ist kein bestandener Test'],
 ['B','Numerische Fehlermatrix: unbekannt mit 0 fuellen','Immer gemeinsam mit K auswerten','B allein ist irrefuehrend'],
 ['Fehler je Paar','B × Einsvektor','Summe bestaetigter Fehler je Zeile','Alle Kategorien gleich gewichtet'],
 ['Bekannt je Paar','K × Einsvektor','Zahl auswertbarer Kategorien','Kein Ersatz fuer die Zahl der Testfaelle'],
 ['Fehlerquote','(B × 1) / (K × 1), elementweise','Nenner 0 => unbekannt','Kein Gesamtsprach-Kompatibilitaetswert'],
 ['Ungewissheit','Fehler / 32 bis (Fehler + Unbekannt) / 32','Untere und obere Grenze je Sprachpaar','Keine statistischen Konfidenzintervalle'],
 ['Vier Stufen','Emit, Compile, Run, Output','Je Stufe eigene Fehler- und Beobachtungswerte','Frueher Fehler => spaetere Stufe UNKNOWN'],
 ['Oracle','Native Referenz oder festgelegter Testvertrag','Quelle nativ geprueft steht in Messwerte','7 Quellsprachen ohne Referenzausfuehrung'],
 ['Vergleich','Skalare Werte; Zahlen mit Toleranz 1e-10','R-Anzeige [1] und String-Anfuehrungen normalisiert','Kein Vergleich von Speicher, IO oder Performance'],
 ['Grenzen','16 vorhandene + 16 offene Kategorien','Keine Gewichtungen aus alten CSVs uebernommen','Ein PASS belegt nicht alle Faelle der Kategorie'],
 ['Identitaet','Quelle = Ziel ausgeschlossen','156 statt 169 Paare','Copy-through beweist keine Uebersetzung'],
 ['Build','GUI/CLI kann derzeit nicht frisch gebaut werden','internal/runtimeassets fehlt','Audit ruft denselben Kern ohne GUI auf'],
 ['Artefakt','Vorhandene dist/CodeTranspiler.exe unveraendert','Quellkern separat gebaut und geprueft','Keine Aussage zur Gleichheit von EXE und Quelle'],
 ['Belege','measurements.json und source_baselines.json','Quellcode, Zielcode, Exitcodes und Ausgaben','outputs/transpiler-audit-2026-08-30'],
 ['Reproduzieren','tools/matrix-audit/audit.mjs','Go-Adapter plus vorhandene Compiler','tools/matrix-audit/README.md']
 ];write(s,5,rows);s.getRange('A4:A22').format.columnWidth=23;s.getRange('B4:B22').format.columnWidth=52;s.getRange('C4:C22').format.columnWidth=54;s.getRange('D4:D22').format.columnWidth=52;s.getRange('A5:D22').format.wrapText=true;s.getRange('A5:D22').format.rowHeight=39;
}

// Machine-readable numeric arrays: unknowns require the accompanying mask.
const numeric={schema_version:1,languages,features:features.map(f=>f.id),pairs,
 B:grids.map(r=>r.slice(2).map(x=>x??0)),K:grids.map(r=>r.slice(2).map(x=>x===null?0:1)),
 F:grids.map(r=>r.slice(2)),
 stages:Object.fromEntries(summary.stages.map(stage=>[stage,{B:pairs.map(p=>features.map(f=>error(index.get(`${p.source}>${p.target}:${f.id}`)[stage])??0)),K:pairs.map(p=>features.map(f=>index.get(`${p.source}>${p.target}:${f.id}`)[stage]==='UNKNOWN'?0:1))}])),
 note:'B contains zero for unknown; always use K. Results describe these fixtures, not full-language semantics.'};
await fs.writeFile(path.join(out,'numeric_matrices.json'),JSON.stringify(numeric,null,2));
function csv(rows){return rows.map(r=>r.map(v=>v==null?'':typeof v==='number'?String(v):'"'+String(v).replaceAll('"','""')+'"').join(',')).join('\r\n')+'\r\n';}
await fs.writeFile(path.join(out,'error_matrix.csv'),csv([['source','target',...features.map(f=>f.id)],...grids]));
await fs.writeFile(path.join(out,'known_mask.csv'),csv([['source','target',...features.map(f=>f.id)],...grids.map(r=>[r[0],r[1],...r.slice(2).map(v=>v===null?0:1)])]));
await fs.writeFile(path.join(out,'stage_results.csv'),csv([['source','target','feature','stage','status','B','K','source_validated'],...records.flatMap(r=>summary.stages.map(stage=>[r.source,r.target,r.feature,stage,r[stage],error(r[stage])??0,r[stage]==='UNKNOWN'?0:1,r.source_validated?1:0]))]));

const check=await wb.inspect({kind:'table',range:'Ueberblick!A4:G5',include:'values,formulas',tableMaxRows:2,tableMaxCols:7,maxChars:4000});console.log(check.ndjson);
const errors=await wb.inspect({kind:'match',searchTerm:'#REF!|#DIV/0!|#VALUE!|#NAME\\?|#N/A',options:{useRegex:true,maxResults:20},summary:'Formula error check',maxChars:3000});console.log(errors.ndjson);
await fs.mkdir(path.join(out,'previews'),{recursive:true});
for(const n of names){const range=n==='Ueberblick'?'A1:N21':n==='Delta'?'A1:H17':n==='Anleitung'?'A1:D12':n==='Kategorien'?'A1:D14':n==='Messwerte'?'A1:I14':n==='Paare'?'A1:J14':'A1:J14';const blob=await wb.render({sheetName:n,range,scale:1,format:'png'});await fs.writeFile(path.join(out,'previews',n+'.png'),new Uint8Array(await blob.arrayBuffer()));}
const file=await SpreadsheetFile.exportXlsx(wb);await file.save(path.join(out,'Fehlermatrix.xlsx'));
const actual=sheets.Ueberblick.getRange('A5:G5').values[0];const expected=[156,32,4992,summary.overall.PASS,summary.overall.FAIL,summary.overall.UNKNOWN,(summary.overall.PASS+summary.overall.FAIL)/4992];
for(let i=0;i<actual.length;i++)if(Math.abs(actual[i]-expected[i])>1e-10)throw new Error(`Workbook reconciliation failed at ${i}: ${actual[i]} != ${expected[i]}`);
console.log('Workbook counts reconciled:',JSON.stringify(actual));
