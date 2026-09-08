import fs from 'node:fs/promises';
import path from 'node:path';
import { spawn } from 'node:child_process';
import { createHash } from 'node:crypto';
import { sourceIdentity } from './source-identity.mjs';

const root = process.cwd();
const identityAtStart=await sourceIdentity(root);
const v16 = process.argv.includes('--v16');
const v15 = process.argv.includes('--v15') || v16;
const v14 = process.argv.includes('--v14') || v15;
const v13 = process.argv.includes('--v13') || v14;
const v12 = process.argv.includes('--v12') || v13;
const extended = process.argv.includes('--extended') || v12;
const out = path.resolve(process.env.AUDIT_OUTPUT || path.join(root, 'outputs', v16 ? 'transpiler-audit-v16' : v15 ? 'transpiler-audit-v15' : v14 ? 'transpiler-audit-v14' : v13 ? 'transpiler-audit-v13' : v12 ? 'transpiler-audit-v12' : extended ? 'transpiler-audit-v11' : 'transpiler-audit-2026-08-30'));
const work = path.join(root, '.audit-cache', 'cases');
await fs.mkdir(out, {recursive:true});
await fs.mkdir(work, {recursive:true});
const languages = ['r','go','rust','cpp','c','python','zig','julia','nim','csharp','java','kotlin','swift'];
const features = [
 ['arithmetic','Arithmetik','2 + 3 * 4'], ['binding','Variable anlegen','x = 7'],
 ['reassignment','Variable erneut zuweisen','x = 1; x = 2'], ['division','Gleitkommadivision','7.0 / 2.0'],
 ['boolean','Boolescher Wert','true'], ['string_comment','Kommentarzeichen in Text','https://a#b'],
 ['string_keywords','Schluesselwoerter in Text','true false'], ['comparison','Vergleich','2 < 3'],
 ['multiline','Mehrzeiliger Ausdruck','x = (2 + newline 3)'], ['grouping','Klammerung','(2 + 3) * 4'],
 ['if_else','Verzweigung','x becomes 2'], ['while','While-Schleife','count to 3'],
 ['for','For-Schleife','sum 1 through 4'], ['function','Funktion und Rueckgabe','twice(3)'],
 ['index','Listenindex','second element = 20'], ['integer_division','Ganzzahldivision','source-specific division of 7 by 2'],
 ['scope','Geltungsbereiche','reserved'], ['closure','Closure und Capture','reserved'],
 ['named_args','Benannte Argumente','reserved'], ['lazy_eval','Verzoegerte Auswertung','reserved'],
 ['short_circuit','Kurzschluss und Seiteneffekte','reserved'], ['overflow','Zahlenbreite und Ueberlauf','reserved'],
 ['null_na','Null, NA und NaN','reserved'], ['objects','Objekte und Dispatch','reserved'],
 ['exceptions','Ausnahmen und Cleanup','reserved'], ['generics','Generics und Templates','reserved'],
 ['ownership','Referenzen und Ownership','reserved'], ['modules','Imports und Module','reserved'],
 ['concurrency','Threads und Async','reserved'], ['ffi','Native ABI und FFI','reserved'],
 ['reflection','Reflection und Eval','reserved'], ['serialization','Serialisierung und IO','reserved']
].map(([id,label,contract],i)=>({id,label,contract,weight:1,fixture_available:i<16}));
if (extended) features.push(...[
 ['text_tokens','Text mit Transformationswoertern','True False and or div (double) @as(i64, 2)'],
 ['text_delimiters','Klammern und Unicode in Text','Grüße [0] {1, 2} ( )'],
 ['two_indexes','Zwei Indexausdruecke','first + second = 30'],
 ['expression_index','Berechneter Index','index variable and arithmetic'],
 ['function_zero','Null als Funktionsrueckgabe','zero(3) = 0'],
 ['call_composition','Verschachtelte Aufrufe','twice(twice(3)) = 12'],
 ['symbolic_range','Symbolische Schleifengrenze','sum 1 through variable n = 10'],
 ['statement_sequence','Mehrere Anweisungen pro Zeile','x = 2; x = x + 3; print(x)'],
 ['discarded_argument','Seiteneffekt in unbenutztem Argument','eager languages print 99 then 7; R lazy evaluation prints 7']
].map(([id,label,contract])=>({id,label,contract,weight:1,fixture_available:true})));
if(v12)features.push(...[
 ['unary_negative','Negierter Ausdruck','-((2 + 3) * 4) = -20'],
 ['logical_not','Negierter Vergleich','not (2 < 3) = false'],
 ['logical_combination','Verknuepfte Vergleiche','(2 < 3) and (4 < 5) = true'],
 ['short_circuit_effect','Bedingte Seiteneffekte','false and probe() must not print 99'],
 ['used_effect_argument','Verwendetes Argument mit Seiteneffekt','print 99 once, then 6'],
 ['repeated_effect_argument','Mehrfach verwendetes Argument','print 99 once, then 6 despite x + x'],
 ['function_local','Lokale Bindung in Funktion','y = x + 1; return y * 2'],
 ['nested_logic','Verschachtelte Boolesche Ausdruecke','not false and (true or false) = true']
].map(([id,label,contract])=>({id,label,contract,weight:1,fixture_available:true})));
const ext = {r:'.R',go:'.go',rust:'.rs',cpp:'.cpp',c:'.c',python:'.py',zig:'.zig',julia:'.jl',nim:'.nim',csharp:'.cs',java:'.java',kotlin:'.kt',swift:'.swift'};
if(v13)features.push(...[
 ['function_branch_true','Funktionszweig wahr','positive input returns 6'],
 ['function_branch_false','Funktionszweig falsch','negative input returns 7'],
 ['function_early_true','Fruehe Rueckgabe wahr','positive input returns before continuation'],
 ['function_early_false','Fruehe Rueckgabe falsch','negative input reaches continuation'],
 ['function_branch_local','Lokale Bindungen im Zweig','selected branch returns 8'],
 ['function_branch_effect','Seiteneffekt nur im gewaehlten Zweig','print 99 then 6, never 77'],
 ['function_branch_join','Zusammenfuehrung lokaler Werte','branch updates y; continuation returns 8'],
 ['function_nested_return','Verschachtelte Rueckgabepfade','nested true branch returns 9']
].map(([id,label,contract])=>({id,label,contract,weight:1,fixture_available:true})));
const semi = l => ['r','python','go','julia','nim','kotlin','swift'].includes(l)?'':';';
if(v14)features.push(...[
 ['function_while_sum','Schleife in Funktion','sum 1 through 3 = 6'],
 ['function_while_zero','Null Schleifendurchlaeufe','zero iterations return 0'],
 ['function_while_break','Schleifenabbruch','break at 2 returns accumulated 1'],
 ['function_while_continue','Iteration ueberspringen','skip 2 returns 4'],
 ['function_while_return','Rueckgabe aus Schleife','return at 2 yields 20'],
 ['function_while_nested','Verschachtelte Schleifen','two by two iterations yield 4'],
 ['function_while_effect','Seiteneffekte pro Iteration','print 1, 2, 3 then 6'],
 ['function_while_branch','Verzweigung in Schleife','selected updates yield 11']
].map(([id,label,contract])=>({id,label,contract,weight:1,fixture_available:true})));
if(v15)features.push(...[
 ['compare_equal','Gleichheit','2 == 2'],
 ['compare_not_equal','Ungleichheit','2 != 3'],
 ['compare_less_equal','Kleiner gleich','2 <= 3'],
 ['compare_greater_equal','Groesser gleich','3 >= 3']
].map(([id,label,contract])=>({id,label,contract,weight:1,fixture_available:true})));
if(v16)features.push(...[
 ['function_for_sum','Iterationsvektor in Funktion','sum 1 through 4 = 10'],
 ['function_for_break','Iterationsvektor abbrechen','break at 3 returns 3'],
 ['function_for_continue','Iterationsposition bei continue','skip 2 returns 8'],
 ['function_for_return','Rueckgabe aus Iteration','return at 3 yields 30'],
 ['function_for_nested','Verschachtelte Iterationsvektoren','four by four iterations yield 16'],
 ['function_for_effect','Seiteneffekte der Iteration','print 1, 2, 3, 4 then 10']
].map(([id,label,contract])=>({id,label,contract,weight:1,fixture_available:true})));
function decl(l,n,e,mut=false) {
 const prefix={r:'',go:'',rust:mut?'let mut ':'let ',cpp:'auto ',c:'int ',python:'',zig:mut?'var ':'const ',julia:'',nim:mut?'var ':'let ',csharp:'var ',java:'int ',kotlin:mut?'var ':'val ',swift:mut?'var ':'let '}[l];
 return `${prefix}${n}${l==='zig'?': i64':''} ${l==='r'?'<-':l==='go'?':=':'='} ${e}${semi(l)}`;
}
function set(l,n,e) { return `${n} ${l==='r'?'<-':'='} ${e}${semi(l)}`; }
function print(l,e,string=false) {
 return ({r:`print(${e})`,go:`fmt.Println(${e})`,rust:`println!("{}", ${e});`,cpp:`std::cout << (${e}) << std::endl;`,c:string?`printf("%s\\n", ${e});`:`printf("%g\\n", (double)(${e}));`,python:`print(${e})`,zig:`std.debug.print("${string?'{s}':'{}'}\\n", .{${e}});`,julia:`println(${e})`,nim:`echo ${e}`,csharp:`Console.WriteLine(${e});`,java:`System.out.println(${e});`,kotlin:`println(${e})`,swift:`print(${e})`})[l];
}
function wrap(l,body,pre='') {
 const wrappers={go:['package main\nimport "fmt"\n','func main() {\n','}\n'],rust:['','fn main() {\n','}\n'],cpp:['#include <iostream>\n#include <vector>\n','int main() {\n','return 0;\n}\n'],c:['#include <stdio.h>\n#include <stdbool.h>\n','int main(void) {\n','return 0;\n}\n'],zig:['const std = @import("std");\n','pub fn main() !void {\n','}\n'],java:['public class Main {\n','public static void main(String[] args) {\n','}\n}\n'],csharp:['using System;\nclass Program {\n','static void Main() {\n','}\n}\n'],kotlin:['','fun main() {\n','}\n']};
 const w=wrappers[l]; return w?w[0]+pre+w[1]+body+'\n'+w[2]:pre+body+'\n';
}
function structure(l,kind,condition,yes,no='') {
 if (l==='python'||l==='nim') return `${kind} ${condition}:\n    ${yes}${no?'\nelse:\n    '+no:''}`;
 if (l==='julia') return `${kind} ${condition}\n    ${yes}${no?'\nelse\n    '+no:''}\nend`;
 const head=['r','c','cpp','java','csharp','kotlin'].includes(l)?`${kind} (${condition})`:`${kind} ${condition}`;
 return `${head} {\n    ${yes}\n}${no?' else {\n    '+no+'\n}':''}`;
}
function fixture(l,f) {
 let body='',pre='',expect=0,kind='number';
 const bool=l==='r'?'TRUE':l==='python'?'True':'true';
 switch(f) {
 case 'function_for_sum':case 'function_for_break':case 'function_for_continue':
 case 'function_for_return':case 'function_for_nested':case 'function_for_effect': {
  const base=fixture(l,'function'),ret=e=>l==='r'?`return(${e})`:`return ${e}${semi(l)}`;
  const heads={r:'for (i in 1:4)',go:'for i := 1; i <= 4; i++',rust:'for i in 1..=4',cpp:'for (int i = 1; i <= 4; ++i)',c:'for (int i = 1; i <= 4; ++i)',python:'for i in range(1, 5):',zig:'for (1..5) |i|',julia:'for i in 1:4',nim:'for i in 1..4:',csharp:'for (int i = 1; i <= 4; i++)',java:'for (int i = 1; i <= 4; i++)',kotlin:'for (i in 1..4)',swift:'for i in 1...4'};
  const loop=(name,content)=>heads[l].replaceAll(/\bi\b/g,name)+(['python','nim','julia'].includes(l)?'\n    '+content.replaceAll('\n','\n    ')+(l==='julia'?'\nend':''):' {\n    '+content.replaceAll('\n','\n    ')+'\n}');
  const branch=(condition,content)=>structure(l,'if',condition,content);
  const value=l==='zig'?'@as(i64, @intCast(i))':'i';
  let inner=[];
  if(f==='function_for_break')inner.push(branch('i == 3','break'+semi(l)));
  if(f==='function_for_continue')inner.push(branch('i == 2',(l==='r'?'next':'continue')+semi(l)));
  if(f==='function_for_return')inner.push(branch('i == 3',ret('30')));
  if(f==='function_for_effect')inner.push(print(l,value));
  if(f==='function_for_nested')inner.push(loop('j',set(l,'total','total + 1')));
  else inner.push(set(l,'total','total + '+value));
  let replacement=[decl(l,'total','0',true),loop('i',inner.join('\n')),ret(f==='function_for_return'?'99':'total')].join('\n');
  if(l==='python'||l==='nim')replacement=replacement.replaceAll('\n','\n    ');
  let code=base.code.replace(ret('x * 2'),replacement);
  if(l==='r')code=code.replace(' }\n','\n}\n');
  if(l==='zig')code=code.replace('twice(x:', 'twice(_:');
  const expected={function_for_sum:10,function_for_break:3,function_for_continue:8,function_for_return:30,function_for_nested:16,function_for_effect:'1\n2\n3\n4\n10'}[f];
  return {...base,feature:f,code,expected,kind:f==='function_for_effect'?'lines':'number'};
 }
 case 'compare_equal':case 'compare_not_equal':case 'compare_less_equal':case 'compare_greater_equal': {
  const expression={compare_equal:'2 == 2',compare_not_equal:'2 != 3',compare_less_equal:'2 <= 3',compare_greater_equal:'3 >= 3'}[f];
  body=print(l,expression);kind='boolean';expect=true;break;
 }
 case 'function_while_sum':case 'function_while_zero':case 'function_while_break':case 'function_while_continue':
 case 'function_while_return':case 'function_while_nested':case 'function_while_effect':case 'function_while_branch': {
  const base=fixture(l,'function'),ret=e=>l==='r'?`return(${e})`:`return ${e}${semi(l)}`;
  const lines=(...v)=>v.join('\n');
  const block=(kind,condition,yes,no='')=>structure(l,kind,condition,yes.replaceAll('\n','\n    '),no.replaceAll('\n','\n    '));
  const loop=content=>block(l==='go'?'for':'while','i < x',content);
  let inner=lines(set(l,'i','i + 1'));
  if(f==='function_while_break')inner+='\n'+block('if','i == 2','break'+semi(l));
  if(f==='function_while_continue')inner+='\n'+block('if','i == 2',(l==='r'?'next':'continue')+semi(l));
  if(f==='function_while_return')inner+='\n'+block('if','i == 2',ret('20'));
  if(f==='function_while_effect')inner+='\n'+print(l,'i');
  if(f==='function_while_nested')inner+='\n'+lines(decl(l,'j','0',true),block(l==='go'?'for':'while','j < x',lines(set(l,'j','j + 1'),set(l,'total','total + 1'))));
  else if(f==='function_while_branch')inner+='\n'+block('if','i > 1',set(l,'total','total + i * 2'),set(l,'total','total + i'));
  else inner+='\n'+set(l,'total','total + i');
  let replacement=lines(decl(l,'i','0',true),decl(l,'total','0',true),loop(inner),ret(f==='function_while_return'?'99':'total'));
  if(l==='python'||l==='nim')replacement=replacement.replaceAll('\n','\n    ');
  let code=base.code.replace(ret('x * 2'),replacement);
  if(f==='function_while_zero')code=code.replace('twice(3)','twice(0)');
  if(f==='function_while_nested')code=code.replace('twice(3)','twice(2)');
  const expected={function_while_sum:6,function_while_zero:0,function_while_break:1,function_while_continue:4,function_while_return:20,function_while_nested:4,function_while_effect:'1\n2\n3\n6',function_while_branch:11}[f];
  return {...base,feature:f,code,expected,kind:f==='function_while_effect'?'lines':'number'};
 }
 case 'function_branch_true':case 'function_branch_false':case 'function_early_true':case 'function_early_false':
 case 'function_branch_local':case 'function_branch_effect':case 'function_branch_join':case 'function_nested_return': {
  const base=fixture(l,'function');
  const ret=e=>l==='r'?`return(${e})`:`return ${e}${semi(l)}`;
  const lines=(...parts)=>parts.join('\n');
  // Each source uses its own native block syntax; the oracle is specified
  // independently of any generated target or control-flow implementation.
  const branch=(condition,yes,no='')=>structure(l,'if',condition,yes.replaceAll('\n','\n    '),no.replaceAll('\n','\n    '));
  let replacement;
  if(f==='function_branch_local')replacement=branch('x > 0',lines(decl(l,'y','x + 1'),ret('y * 2')),lines(decl(l,'y','9'),ret('y * 2')));
  else if(f==='function_branch_effect')replacement=branch('x > 0',lines(print(l,'99'),ret('x * 2')),lines(print(l,'77'),ret('7')));
  else if(f==='function_branch_join')replacement=lines(decl(l,'y','x',true),branch('x > 0',set(l,'y','y + 1'),set(l,'y','y - 1')),ret('y * 2'));
  else if(f==='function_nested_return')replacement=branch('x > 0',branch('x > 2',ret('9'),ret('8')),ret('7'));
  else if(f.startsWith('function_early'))replacement=lines(branch('x > 0',ret('x * 2')),ret('7'));
  else replacement=branch('x > 0',ret('x * 2'),ret('7'));
  if(l==='python'||l==='nim')replacement=replacement.replaceAll('\n','\n    ');
  const old=ret('x * 2');
  let code=base.code.replace(old,replacement);
  // R needs a line break between a closing conditional and its enclosing }.
  if(l==='r')code=code.replace(' }\n','\n}\n');
  if(f.endsWith('_false'))code=code.replace('twice(3)','twice(-3)');
  const expected=f==='function_branch_effect'?'99\n6':f==='function_nested_return'?9:f==='function_branch_local'||f==='function_branch_join'?8:f.endsWith('_false')?7:6;
  return {...base,feature:f,code,expected,kind:f==='function_branch_effect'?'lines':'number'};
 }
 case 'unary_negative':body=print(l,'-((2 + 3) * 4)');expect=-20;break;
 case 'logical_not':case 'logical_combination':case 'nested_logic': {
  const not=['python','nim','zig'].includes(l)?'not ':'!';
  const and=['python','nim','zig'].includes(l)?'and':'&&',or=['python','nim','zig'].includes(l)?'or':'||';
  const e=f==='logical_not'?`${not}(2 < 3)`:f==='logical_combination'?`(2 < 3) ${and} (4 < 5)`:`${not}(2 > 3) ${and} ((4 < 5) ${or} (5 < 4))`;
  body=print(l,e);kind='boolean';expect=f!=='logical_not';break;
 }
 case 'used_effect_argument':case 'repeated_effect_argument':case 'short_circuit_effect': {
  const base=fixture(l,'discarded_argument');let code=base.code;
  if(f==='short_circuit_effect'){
   const and=['python','nim','zig'].includes(l)?'and':'&&';
   code=code.replace('twice(probe(3))',`(2 > 3) ${and} (probe(3) > 0)`);
   return {...base,feature:f,code,kind:'boolean',expected:false};
  }
  code=code.replace(l==='r'?'return(7)':'return 7',l==='r'?`return(${f==='used_effect_argument'?'x * 2':'x + x'})`:`return ${f==='used_effect_argument'?'x * 2':'x + x'}`);
  if(l==='zig')code=code.replace('twice(_:', 'twice(x:');
  return {...base,feature:f,code,expected:'99\n6'};
 }
 case 'function_local':{
  const base=fixture(l,'function');
  const local=decl(l,'y','x + 1');
  const old=l==='r'?'return(x * 2)':'return x * 2';
  const replacement=l==='r'?'y <- x + 1; return(y * 2)':local+'\n'+(l==='python'||l==='nim'?'    ':'')+'return y * 2';
  return {...base,feature:f,code:base.code.replace(old,replacement),expected:8};
 }
 case 'text_tokens': case 'text_delimiters':
  expect=features.find(x=>x.id===f).contract;kind='string';body=print(l,JSON.stringify(expect),true);break;
 case 'two_indexes': case 'expression_index': {
  const base=fixture(l,'index');
  const first=['r','julia'].includes(l)?1:0,second=first+1;
  const before=print(l,`x[${second}]`);
  const after=f==='two_indexes'?print(l,`x[${first}] + x[${second}]`):decl(l,'i',String(first))+'\n'+print(l,'x[i + 1]');
  return {...base,feature:f,code:base.code.replace(before,after),expected:f==='two_indexes'?30:20};
 }
 case 'function_zero': case 'call_composition': {
  const base=fixture(l,'function');
  let code=f==='function_zero'?base.code.replace('x * 2','0'):base.code.replace('twice(3)', 'twice(twice(3))');
  if(l==='zig'&&f==='function_zero')code=code.replace('twice(x:', 'twice(_:');
  return {...base,feature:f,code,expected:f==='function_zero'?0:12};
 }
 case 'symbolic_range': {
  const base=fixture(l,'for');
  // Replace only the loop boundary, leaving the known-good wrapper unchanged.
  let code=base.code.replace('x <- 0', 'n <- 4\nx <- 0');
  if(l!=='r') {
   const initial=decl(l,'x','0',true);
   code=base.code.replace(initial,decl(l,'n',l==='python'||l==='zig'?'5':'4')+'\n'+initial);
  }
  code=code.replace('1:4','1:n').replace('1..=4','1..=n').replace('1...4','1...n').replace('1..4','1..n').replace('1..5','1..n').replace('range(1, 5)','range(1, n)').replace('i <= 4','i <= n');
  return {...base,feature:f,code};
 }
 case 'statement_sequence': body=decl(l,'x','2',true)+'; '+set(l,'x','x + 3')+'; '+print(l,'x');body=body.replaceAll(';;',';');expect=5;break;
 case 'discarded_argument': {
  const base=fixture(l,'function');
  const start=base.code.indexOf(l==='r'?'twice <-':l==='go'?'func twice':l==='rust'?'fn twice':l==='python'?'def twice':l==='julia'?'function twice':l==='nim'?'proc twice':l==='kotlin'?'fun twice':l==='swift'?'func twice':l==='zig'?'fn twice':l==='java'||l==='csharp'?'static int twice':'int twice');
  const call=base.code.lastIndexOf('twice(3)');
  // Reuse each language's signature and wrapper; keep probe as a separate function.
  const endBody=base.code.indexOf(l==='python'||l==='nim'?'\n':l==='julia'?'end':'}',base.code.indexOf('return',start));
  const end=endBody+(l==='julia'?3:l==='python'||l==='nim'?0:1);
  const definition=base.code.slice(start,end);
  let constant=definition.replace('x * 2','7');
  if(l==='zig')constant=constant.replace('twice(x:', 'twice(_:');
  const probe=l==='r'?'probe <- function(x) { print(99); return(x) }':definition.replaceAll('twice','probe').replace('return x * 2',print(l,'99')+'\n'+(l==='python'||l==='nim'?'    ':'')+'return x');
  let code=base.code.slice(0,start)+constant+'\n'+probe+base.code.slice(end,call)+'twice(probe(3))'+base.code.slice(call+'twice(3)'.length);
  // R's original one-line return needs a statement separator after printing.
  if(l==='r')code=code.replace('print(99)\nreturn (x)', 'print(99); return(x)');
  return {...base,feature:f,code,kind:'lines',expected:l==='r'?'7':'99\n7'};
 }
 case 'arithmetic': body=print(l,'2 + 3 * 4');expect=14;break;
 case 'binding':body=decl(l,'x','7')+'\n'+print(l,'x');expect=7;break;
 case 'reassignment':body=decl(l,'x','1',true)+'\n'+set(l,'x','2')+'\n'+print(l,'x');expect=2;break;
 case 'division':body=print(l,'7.0 / 2.0');expect=3.5;break;
 case 'integer_division': {
  const e=l==='zig'?'@divTrunc(@as(i64, 7), 2)':l==='nim'?'7 div 2':'7 / 2';
  expect=['r','python','julia'].includes(l)?3.5:3; body=print(l,e);break;
 }
 case 'boolean': body=print(l,bool);kind='boolean';expect=true;break;
 case 'string_comment':expect='https://a#b';kind='string';body=print(l,JSON.stringify(expect),true);break;
 case 'string_keywords':expect='true false';kind='string';body=print(l,JSON.stringify(expect),true);break;
 case 'comparison':body=print(l,'2 < 3');kind='boolean';expect=true;break;
 case 'multiline':body=decl(l,'x','(2 +\n3)')+'\n'+print(l,'x');expect=5;break;
 case 'grouping':body=print(l,'(2 + 3) * 4');expect=20;break;
 case 'if_else': body=decl(l,'x','1',true)+'\n'+structure(l,'if','2 < 3',set(l,'x','2'),set(l,'x','9'))+'\n'+print(l,'x');expect=2;break;
 case 'while':body=decl(l,'x','0',true)+'\n'+structure(l,l==='go'?'for':'while','x < 3',set(l,'x','x + 1'))+'\n'+print(l,'x');expect=3;break;
 case 'for': {
  const heads={r:'for (i in 1:4)',go:'for i := 1; i <= 4; i++',rust:'for i in 1..=4',cpp:'for (int i = 1; i <= 4; ++i)',c:'for (int i = 1; i <= 4; ++i)',python:'for i in range(1, 5):',zig:'for (1..5) |i|',julia:'for i in 1:4',nim:'for i in 1..4:',csharp:'for (int i = 1; i <= 4; i++)',java:'for (int i = 1; i <= 4; i++)',kotlin:'for (i in 1..4)',swift:'for i in 1...4'};
  const add=set(l,'x',l==='zig'?'x + @as(i64, @intCast(i))':'x + i');
  let loop=heads[l]+(['python','nim','julia'].includes(l)?'\n    '+add+(l==='julia'?'\nend':''):' {\n    '+add+'\n}');
  // Julia top-level loop assignment uses explicit global scope in a script.
  if(l==='julia') loop=loop.replace('    x =','    global x =');
  body=decl(l,'x','0',true)+'\n'+loop+'\n'+print(l,'x');expect=10;break;
 }
 case 'function': {
  const defs={r:'twice <- function(x) { return(x * 2) }',go:'func twice(x int) int {\nreturn x * 2\n}',rust:'fn twice(x: i32) -> i32 {\nreturn x * 2;\n}',cpp:'int twice(int x) {\nreturn x * 2;\n}',c:'int twice(int x) {\nreturn x * 2;\n}',python:'def twice(x):\n    return x * 2',zig:'fn twice(x: i64) i64 {\nreturn x * 2;\n}',julia:'function twice(x)\nreturn x * 2\nend',nim:'proc twice(x: int): int =\n    return x * 2',csharp:'static int twice(int x) {\nreturn x * 2;\n}',java:'static int twice(int x) {\nreturn x * 2;\n}',kotlin:'fun twice(x: Int): Int {\nreturn x * 2\n}',swift:'func twice(_ x: Int) -> Int {\nreturn x * 2\n}'};
  pre=defs[l]+'\n';body=print(l,'twice(3)');expect=6;break;
 }
 case 'index': {
  const ds={r:'x <- c(10, 20)',go:'x := []int{10, 20}',rust:'let x = [10, 20];',cpp:'std::vector<int> x = {10, 20};',c:'int x[] = {10, 20};',python:'x = [10, 20]',zig:'const x = [_]i64{10, 20};',julia:'x = [10, 20]',nim:'let x = [10, 20]',csharp:'int[] x = {10, 20};',java:'int[] x = {10, 20};',kotlin:'val x = intArrayOf(10, 20)',swift:'let x = [10, 20]'};
  body=ds[l]+'\n'+print(l,`x[${['r','julia'].includes(l)?2:1}]`);expect=20;break;
 }
 default:return null;
 }
 if(l==='julia'&&f==='while')body=body.replace('    x =','    global x =');
 return {source:l,feature:f,code:wrap(l,body,pre),expected:expect,kind};
}
const env={...process.env,GOCACHE:path.join(root,'.audit-cache','go-build'),PYTHONDONTWRITEBYTECODE:'1',PYTHONIOENCODING:'utf-8'};
async function command(cmd,args,opts={}) {
 return await new Promise(resolve=>{
  let stdout='',stderr='',settled=false;
  const child=spawn(cmd,args,{cwd:opts.cwd||root,env,windowsHide:true,stdio:['pipe','pipe','pipe']});
  const finish=r=>{if(settled)return;settled=true;clearTimeout(timer);resolve({...r,stdout,stderr,command:[cmd,...args]});};
  const timer=setTimeout(()=>{child.kill();finish({exit:null,timeout:true});},opts.timeout||45000);
  child.stdout.setEncoding('utf8');child.stderr.setEncoding('utf8');
  child.stdout.on('data',b=>{stdout+=b;}); child.stderr.on('data',b=>{stderr+=b;});
  child.on('error',e=>{stderr+=e.message;finish({exit:null,unavailable:e.code==='ENOENT'});});
  child.on('close',exit=>finish({exit}));
  child.stdin.on('error',()=>{}); child.stdin.end(opts.input||'');
 });
}
const python=process.env.AUDIT_PYTHON;
if(!python)throw new Error('Set AUDIT_PYTHON to the bundled Python executable.');
const available={python,go:'go',rust:'rustc',cpp:'g++',c:'gcc'};
const nim=process.env.AUDIT_NIM;
if(nim)available.nim=nim;
// Native validation is deliberately limited to detected toolchains; unavailable ones stay UNKNOWN.
const java=process.env.AUDIT_JAVA_HOME;
if(java)available.java=path.join(java,'bin','javac.exe');
const toolchains={};
for(const [lang,cmd] of Object.entries(available)) toolchains[lang]=await command(cmd,[lang==='python'?'--version':lang==='java'?'-version':'--version']);
toolchains.go=await command('go',['version']);
for(const [l,r] of Object.entries(toolchains))if(r.exit!==0)delete available[l];
const cache=new Map();
function execute(l,code) {
 const key=l+'-'+createHash('sha256').update(code).digest('hex').slice(0,20);
 if(cache.has(key))return cache.get(key);
 const p=executeUncached(l,code,key);cache.set(key,p);return p;
}
async function executeUncached(l,code,key) {
 if(!available[l])return {compile:'UNKNOWN',run:'UNKNOWN',reason:'target toolchain not enabled in this audit'};
 const dir=path.join(work,key);await fs.mkdir(dir,{recursive:true});
 const file=path.join(dir,l==='java'?'Main.java':'program'+ext[l]);await fs.writeFile(file,code);
 const exe=path.join(dir,'program.exe');let c,r;
 if(l==='python'){
  c=await command(python,['-c','import ast,sys; ast.parse(open(sys.argv[1], encoding="utf-8").read())',file]);
 }else if(l==='go')c=await command('go',['build','-o',exe,file]);
 else if(l==='rust')c=await command('rustc',['-A','warnings',file,'-o',exe]);
 else if(l==='cpp')c=await command('g++',['-std=c++17',file,'-o',exe]);
 else if(l==='c')c=await command('gcc',['-std=c11',file,'-o',exe]);
 else if(l==='java')c=await command(available.java,['-encoding','UTF-8',file]);
 else if(l==='nim')c=await command(nim,['c','--cc:gcc','--hints:off','--warnings:off','--colors:off','--nimcache:'+path.join(dir,'nimcache'),'--out:'+exe,file]);
 if(c.exit!==0)return {compile:c.timeout||c.unavailable?'UNKNOWN':'FAIL',run:'UNKNOWN',reason:c.stderr,compile_detail:c};
 r=l==='python'?await command(python,[file],{timeout:6000}):l==='java'?await command(path.join(java,'bin','java.exe'),['-Dfile.encoding=UTF-8','-Dstdout.encoding=UTF-8','-cp',dir,'Main'],{timeout:6000}):await command(exe,[],{timeout:6000});
 return {compile:'PASS',run:r.timeout||r.unavailable?'UNKNOWN':r.exit===0?'PASS':'FAIL',stdout:r.stdout,stderr:r.stderr,compile_detail:c,run_detail:r};
}
function matches(text,f) {
 let s=text.trim().replace(/^\[1\]\s*/, '');
 if(f.kind==='lines')return s.replace(/\r/g,'').replace(/^\[1\]\s*/gm,'')===f.expected;
 if(f.kind==='string')return s===f.expected||s===JSON.stringify(f.expected);
 if(f.kind==='boolean')return ['true','1','TRUE','True'].includes(s)===f.expected && /^(true|false|TRUE|FALSE|True|False|0|1)$/.test(s);
 return s!==''&&Number.isFinite(Number(s))&&Math.abs(Number(s)-f.expected)<1e-10;
}
async function pool(items,fn) {let next=0;await Promise.all(Array.from({length:4},async()=>{while(next<items.length){const i=next++;await fn(items[i],i);}}));}
const fixtures=languages.flatMap(l=>features.map(f=>fixture(l,f.id)).filter(Boolean));
await fs.writeFile(path.join(out,'fixtures.json'),JSON.stringify(fixtures,null,2));
const baselines={};
await pool(fixtures,async f=>{const e=await execute(f.source,f.code);baselines[f.source+':'+f.feature]={...e,expected_match:e.run==='PASS'?matches(e.stdout,f):null};});
const invalid=Object.entries(baselines).filter(([,r])=>r.compile==='FAIL'||r.run==='FAIL'||r.expected_match===false);
await fs.writeFile(path.join(out,'source_baselines.json'),JSON.stringify(baselines,null,2));
if(invalid.length){console.log('INVALID FIXTURES',JSON.stringify(invalid.map(([id,r])=>({id,compile:r.compile,run:r.run,stdout:r.stdout,reason:r.reason?.slice(0,1200)})),null,2));throw new Error('Fix native source fixtures before attributing target errors.');}
console.log(`Source fixtures: ${fixtures.length}; native baselines validated: ${Object.values(baselines).filter(x=>x.expected_match===true).length}`);
const requests=fixtures.flatMap(f=>languages.filter(t=>t!==f.source).map(t=>({id:`${f.source}>${t}:${f.feature}`,source:f.source,target:t,code:f.code})));
const bridge=path.join(root,'.audit-cache','matrix-audit.exe');
const bridgeBuild=await command('go',['build','-o',bridge,'./tools/matrix-audit']);
if(bridgeBuild.exit!==0)throw new Error('matrix audit adapter build failed: '+bridgeBuild.stderr);
const batch=await command(bridge,[],{input:JSON.stringify(requests),timeout:60000});
if(batch.exit!==0)throw new Error(batch.stderr);
const adapter_sha256=createHash('sha256').update(await fs.readFile(bridge)).digest('hex');
const responses=new Map(JSON.parse(batch.stdout).map(x=>[x.id,x]));
const records=[];
await pool(requests,async(r,i)=>{
 const response=responses.get(r.id),f=fixtures.find(x=>x.source===r.source&&r.id.endsWith(':'+x.feature));
 const baseline=baselines[f.source+':'+f.feature];
 const e=response.error?{compile:'UNKNOWN',run:'UNKNOWN',reason:response.error}:await execute(r.target,response.code);
 const output=e.run==='PASS'?(matches(e.stdout,f)?'PASS':'FAIL'):'UNKNOWN';
 const emit=response.error?'FAIL':'PASS';
 const statuses=[emit,e.compile,e.run,output];
 const overall=statuses.includes('FAIL')?'FAIL':statuses.every(x=>x==='PASS')?'PASS':'UNKNOWN';
 records.push({id:r.id,source:r.source,target:r.target,feature:f.feature,emit,compile:e.compile,run:e.run,output,overall,
  oracle:baseline.expected_match===true?'native source execution + fixture contract':'fixture contract; source toolchain unavailable',
  source_validated:baseline.expected_match===true,expected:f.expected,actual:e.stdout??null,
  reason:response.error||e.reason||e.stderr||'',source_code:f.code,target_code:response.code||null,execution:e});
 if((i+1)%240===0)console.log(`Checked ${i+1}/${requests.length} route fixtures`);
});
for(const source of languages)for(const target of languages)if(source!==target)for(const f of features)if(!f.fixture_available)records.push({id:`${source}>${target}:${f.id}`,source,target,feature:f.id,emit:'UNKNOWN',compile:'UNKNOWN',run:'UNKNOWN',output:'UNKNOWN',overall:'UNKNOWN',oracle:'none',source_validated:false,reason:'No executable fixture in this first audit; no compatibility claim.'});
records.sort((a,b)=>languages.indexOf(a.source)-languages.indexOf(b.source)||languages.indexOf(a.target)-languages.indexOf(b.target)||features.findIndex(f=>f.id===a.feature)-features.findIndex(f=>f.id===b.feature));
const identityAtEnd=await sourceIdentity(root);
if(identityAtEnd.source_tree_hash!==identityAtStart.source_tree_hash)throw new Error('Sources changed during audit; do not publish mixed-version evidence.');
const summary={languages,features,total_pairs:156,feature_cells:records.length,stages:['emit','compile','run','output'],stage_cells:records.length*4,fixture_count:fixtures.length,route_fixtures:requests.length,native_baselines:Object.values(baselines).filter(x=>x.expected_match===true).length,
 overall:Object.fromEntries(['PASS','FAIL','UNKNOWN'].map(s=>[s,records.filter(x=>x.overall===s).length])),
 stages_counts:Object.fromEntries(['emit','compile','run','output'].map(k=>[k,Object.fromEntries(['PASS','FAIL','UNKNOWN'].map(s=>[s,records.filter(x=>x[k]===s).length]))])),
 toolchains,fixture_policy:`${features.filter(f=>f.fixture_available).length} equally weighted cases per source; 16 reserved semantic categories explicitly UNKNOWN. Not full-language certification.`,
 target_execution_policy:`Native targets and sources: ${Object.keys(available).join(', ')}. All 13 emitters exercised. Remaining fixture oracles are specified expected values, not a native-language differential proof.`,
 ...identityAtStart,adapter_sha256,
 timestamp:new Date().toISOString()};
await fs.writeFile(path.join(out,'measurements.json'),JSON.stringify({summary,records},null,2));
console.log(JSON.stringify({...summary,toolchains:undefined,features:undefined,source_manifest:undefined},null,2));
