import fs from 'node:fs/promises';
import path from 'node:path';
import {spawn} from 'node:child_process';
import {createHash} from 'node:crypto';
const root=process.cwd(),dir=path.resolve('outputs/transpiler-audit-2026-08-30');
const fixtures=JSON.parse(await fs.readFile(path.join(dir,'fixtures.json'),'utf8'));
const measurements=JSON.parse(await fs.readFile(path.join(dir,'measurements.json'),'utf8'));
const languages=measurements.summary.languages,features=measurements.summary.features.map(f=>f.id);
const semantics=['program','block','assign','print','return','expression','identifier','number','string','boolean','null','unary','binary','call','index','if','while','for','function','arithmetic','division','integer_division','comparison','logical','binding','reassignment','scope','closure','named_argument','lazy_evaluation','short_circuit','overflow','object','exception','generic','ownership','module','concurrency','ffi','reflection','serialization','io','effect','string_comment','string_keyword','multiline','grouping','unknown'];
const relations=['syntax','control','data','binding','effect'],stages=['emit','compile','run','output'],tokenClasses=['identifier','number','string','operator','delimiter','newline','comment'];
const lexicalAxes=['identifier','number','string','string_comment','string_keyword','return','print','if','while','for','function','object','exception','module','concurrency','reflection','arithmetic','division','integer_division','comparison','logical','binding','index','grouping','block','comment'];
const env={...process.env,GOCACHE:path.resolve('.audit-cache/go-build')};
async function command(cmd,args,input=''){return new Promise(resolve=>{let stdout='',stderr='';const p=spawn(cmd,args,{cwd:root,env,windowsHide:true,stdio:['pipe','pipe','pipe']});p.stdout.on('data',b=>stdout+=b);p.stderr.on('data',b=>stderr+=b);p.on('close',exit=>resolve({exit,stdout,stderr}));p.on('error',e=>resolve({exit:null,stdout,stderr:stderr+e.message}));p.stdin.end(input);});}
const adapter=path.resolve('.audit-cache/matrix-audit.exe');let run=await command('go',['build','-o',adapter,'./tools/matrix-audit']);if(run.exit!==0)throw new Error(run.stderr);
run=await command(adapter,[],JSON.stringify(fixtures.map((f,i)=>({id:String(i),source:f.source,code:f.code,mode:'analyze'}))));if(run.exit!==0)throw new Error(run.stderr);
const responses=JSON.parse(run.stdout),analysis=responses.map((r,i)=>{if(r.error||!r.analysis)throw new Error(`analysis ${i}: ${r.error}`);return r.analysis;});
const zeros=(r,c)=>Array.from({length:r},()=>Array(c).fill(0));
const transpose=a=>a[0].map((_,j)=>a.map(r=>r[j]));
const multiply=(a,b)=>{const out=zeros(a.length,b[0].length);for(let i=0;i<a.length;i++)for(let k=0;k<a[0].length;k++){const v=a[i][k];if(!v)continue;for(let j=0;j<b[0].length;j++)out[i][j]+=v*b[k][j];}return out;};
const addInPlace=(a,b)=>{for(let i=0;i<a.length;i++)for(let j=0;j<a[i].length;j++)a[i][j]+=b[i][j];};
const rowsOf=m=>Array.from({length:m.Rows},(_,i)=>m.Data.slice(i*m.Cols,(i+1)*m.Cols));
const allTokens=[],allLexicalAxes=[],allTokenClasses=[],fixtureRanges=[],controlEdges=[];
const semanticTransitions=zeros(semantics.length,semantics.length),classTransitions=zeros(tokenClasses.length,tokenClasses.length);
const lexicalAxisTransitions=zeros(lexicalAxes.length,lexicalAxes.length);
const sourceSemanticTransitions=languages.map(()=>zeros(semantics.length,semantics.length));
for(let f=0,offset=0;f<analysis.length;f++){
 const a=analysis[f],nodes=rowsOf(a.lexical_node_matrix).slice(1),axes=rowsOf(a.lexical_axis_matrix),control=rowsOf(a.lexical_control_matrix).slice(1).map(r=>r.slice(1));
 if(nodes.length!==a.token_classes.length||axes.length!==nodes.length||axes.some(r=>r.length!==lexicalAxes.length))throw new Error(`token dimension ${f}`);
 fixtureRanges.push({offset,count:nodes.length});allTokens.push(...nodes);allLexicalAxes.push(...axes);allTokenClasses.push(...a.token_classes);
 const transition=multiply(multiply(transpose(nodes),control),nodes);addInPlace(semanticTransitions,transition);addInPlace(sourceSemanticTransitions[languages.indexOf(fixtures[f].source)],transition);
 const classes=zeros(nodes.length,tokenClasses.length);a.token_classes.forEach((c,i)=>classes[i][c]=1);addInPlace(classTransitions,multiply(multiply(transpose(classes),control),classes));
 addInPlace(lexicalAxisTransitions,multiply(multiply(transpose(axes),control),axes));
 for(let i=0;i<control.length;i++)for(let j=0;j<control.length;j++)if(control[i][j])controlEdges.push([offset+i,offset+j]);offset+=nodes.length;
}
const tokenCount=allTokens.length,fixtureSemanticCount=zeros(fixtures.length,semantics.length),fixtureSemanticBinary=zeros(fixtures.length,semantics.length),fixtureLexicalAxisCount=zeros(fixtures.length,lexicalAxes.length),fixtureLexicalAxisBinary=zeros(fixtures.length,lexicalAxes.length),fixtureClassCount=zeros(fixtures.length,tokenClasses.length);
fixtureRanges.forEach((range,f)=>{for(let i=range.offset;i<range.offset+range.count;i++){for(let s=0;s<semantics.length;s++){fixtureSemanticCount[f][s]+=allTokens[i][s];if(allTokens[i][s])fixtureSemanticBinary[f][s]=1;}for(let a=0;a<lexicalAxes.length;a++){fixtureLexicalAxisCount[f][a]+=allLexicalAxes[i][a];if(allLexicalAxes[i][a])fixtureLexicalAxisBinary[f][a]=1;}fixtureClassCount[f][allTokenClasses[i]]++;}});
const fixtureIndex=new Map(fixtures.map((f,i)=>[f.source+':'+f.feature,i]));const routes=measurements.records.filter(r=>fixtureIndex.has(r.source+':'+r.feature));if(routes.length!==2496)throw new Error('route count');
const routeFixtureIndex=routes.map(r=>fixtureIndex.get(r.source+':'+r.feature));
const routeSemanticBinary=routeFixtureIndex.map(i=>fixtureSemanticBinary[i]),routeSemanticCount=routeFixtureIndex.map(i=>fixtureSemanticCount[i]),routeRelations=routeFixtureIndex.map(i=>analysis[i].lexical_relation_counts);
const routeLexicalAxisBinary=routeFixtureIndex.map(i=>fixtureLexicalAxisBinary[i]),routeLexicalAxisCount=routeFixtureIndex.map(i=>fixtureLexicalAxisCount[i]);
const stageFailure=routes.map(r=>stages.map(s=>r[s]==='FAIL'?1:0)),stageKnown=routes.map(r=>stages.map(s=>r[s]==='UNKNOWN'?0:1));
const sourceIncidence=zeros(routes.length,languages.length),targetIncidence=zeros(routes.length,languages.length);routes.forEach((r,i)=>{sourceIncidence[i][languages.indexOf(r.source)]=1;targetIncidence[i][languages.indexOf(r.target)]=1;});
const design=routes.map((_,i)=>[...routeSemanticBinary[i],...routeSemanticCount[i],...routeLexicalAxisBinary[i],...routeLexicalAxisCount[i],...routeRelations[i],...stageFailure[i],...stageKnown[i],...sourceIncidence[i],...targetIncidence[i]]);
const designKnown=routes.map((_,i)=>[...Array(semantics.length*2+lexicalAxes.length*2+relations.length).fill(1),...stageKnown[i],...Array(stages.length+languages.length*2).fill(1)]);
const gram=multiply(transpose(design),design),knownGram=multiply(transpose(designKnown),designKnown);
const semanticStageFailure=multiply(transpose(routeSemanticBinary),stageFailure),semanticStageKnown=multiply(transpose(routeSemanticBinary),stageKnown);
const tokenSemanticCounts=semantics.map((semantic,j)=>({semantic,count:allTokens.reduce((n,r)=>n+r[j],0),fixtures:fixtureSemanticBinary.reduce((n,r)=>n+r[j],0)}));
const parserKnown=analysis.filter(a=>!a.graph_error).length;
const result={schema_version:4,input:{measurement_source_hash:measurements.summary.source_tree_hash,measurements_sha256:createHash('sha256').update(await fs.readFile(path.join(dir,'measurements.json'))).digest('hex')},
 dimensions:{fixtures:fixtures.length,routes:routes.length,tokens:tokenCount,semantic_dimensions:semantics.length,lexical_axes:lexicalAxes.length,token_classes:tokenClasses.length,relations:relations.length,stages:stages.length,design_columns:design[0].length,design_cells:design.length*design[0].length,gram_cells:gram.length*gram[0].length,sparse_control_edges:controlEdges.length},
 axes:{languages,features,semantics,lexical_axes:lexicalAxes,token_classes:tokenClasses,relations,stages,design_columns:[...semantics.map(s=>'has_'+s),...semantics.map(s=>'count_'+s),...lexicalAxes.map(s=>'has_lex_'+s),...lexicalAxes.map(s=>'count_lex_'+s),...relations.map(r=>'edges_'+r),...stages.map(s=>'fail_'+s),...stages.map(s=>'known_'+s),...languages.map(l=>'source_'+l),...languages.map(l=>'target_'+l)]},
 matrices:{token_semantic:allTokens,token_lexical_axis:allLexicalAxes,token_class:allTokenClasses,fixture_token_incidence_sparse:fixtureRanges,token_control_sparse:controlEdges,fixture_semantic_count:fixtureSemanticCount,fixture_semantic_binary:fixtureSemanticBinary,fixture_lexical_axis_count:fixtureLexicalAxisCount,fixture_lexical_axis_binary:fixtureLexicalAxisBinary,fixture_token_class_count:fixtureClassCount,route_fixture_projection_sparse:routeFixtureIndex,route_semantic_binary:routeSemanticBinary,route_semantic_count:routeSemanticCount,route_lexical_axis_binary:routeLexicalAxisBinary,route_lexical_axis_count:routeLexicalAxisCount,route_lexical_relation_count:routeRelations,stage_failure:stageFailure,stage_known:stageKnown,source_incidence:sourceIncidence,target_incidence:targetIncidence,design,design_known:designKnown,gram,known_gram:knownGram,semantic_transition:semanticTransitions,source_semantic_transition:sourceSemanticTransitions,lexical_axis_transition:lexicalAxisTransitions,lexical_axis_semantic:multiply(transpose(allLexicalAxes),allTokens),token_class_transition:classTransitions,semantic_stage_failure:semanticStageFailure,semantic_stage_known:semanticStageKnown},
 summary:{parser_graph_known_fixtures:parserKnown,parser_graph_unknown_fixtures:fixtures.length-parserKnown,lexical_graph_known_fixtures:fixtures.length,token_semantic_counts:tokenSemanticCounts,lexical_relation_totals:relations.map((relation,j)=>({relation,edges:analysis.reduce((n,a)=>n+a.lexical_relation_counts[j],0)}))},
 rules:['All 208 lexical graphs are known independently of the old CIR parser.','Token strings are quote-aware; comment markers inside strings remain string content.','Continuation newline is set only while parenthesis or index depth is open.','Sequential token control edges are structural observations, not full control-flow semantics.','All matrices are unweighted; masks remain separate.']};
await fs.writeFile(path.join(dir,'semantic_matrix_v4.json'),JSON.stringify(result,null,2));
await fs.writeFile(path.join(dir,'semantic_matrix_v4_summary.json'),JSON.stringify({schema_version:4,input:result.input,dimensions:result.dimensions,summary:result.summary,rules:result.rules},null,2));
console.log(JSON.stringify({dimensions:result.dimensions,summary:result.summary,rules:result.rules},null,2));
