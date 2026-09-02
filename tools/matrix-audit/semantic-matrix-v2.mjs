import fs from 'node:fs/promises';
import path from 'node:path';
import {spawn} from 'node:child_process';
import {createHash} from 'node:crypto';

const root=process.cwd(),dir=path.resolve('outputs/transpiler-audit-2026-08-30');
const fixtures=JSON.parse(await fs.readFile(path.join(dir,'fixtures.json'),'utf8'));
const measurements=JSON.parse(await fs.readFile(path.join(dir,'measurements.json'),'utf8'));
const languages=measurements.summary.languages;
const features=measurements.summary.features.map(f=>f.id);
const semantics=['program','block','assign','print','return','expression','identifier','number','string','boolean','null','unary','binary','call','index','if','while','for','function','arithmetic','division','integer_division','comparison','logical','binding','reassignment','scope','closure','named_argument','lazy_evaluation','short_circuit','overflow','object','exception','generic','ownership','module','concurrency','ffi','reflection','serialization','io','effect','string_comment','string_keyword','multiline','grouping','unknown'];
const relations=['syntax','control','data','binding','effect'],stages=['emit','compile','run','output'];
const env={...process.env,GOCACHE:path.resolve('.audit-cache/go-build')};
async function command(cmd,args,input=''){return new Promise(resolve=>{let stdout='',stderr='';const p=spawn(cmd,args,{cwd:root,env,windowsHide:true,stdio:['pipe','pipe','pipe']});p.stdout.on('data',b=>stdout+=b);p.stderr.on('data',b=>stderr+=b);p.on('close',exit=>resolve({exit,stdout,stderr}));p.on('error',e=>resolve({exit:null,stdout,stderr:stderr+e.message}));p.stdin.end(input);});}
const adapter=path.resolve('.audit-cache/matrix-audit.exe');
let run=await command('go',['build','-o',adapter,'./tools/matrix-audit']);if(run.exit!==0)throw new Error(run.stderr);
run=await command(adapter,[],JSON.stringify(fixtures.map((f,i)=>({id:String(i),source:f.source,code:f.code,mode:'analyze'}))));if(run.exit!==0)throw new Error(run.stderr);
const responses=JSON.parse(run.stdout);if(responses.length!==fixtures.length)throw new Error('analysis count mismatch');
const analysis=responses.map((r,i)=>{if(r.error||!r.analysis)throw new Error(`analysis ${i}: ${r.error}`);return r.analysis;});
if(analysis.some(a=>a.semantic.length!==semantics.length||a.features.length!==features.length||a.relation_counts.length!==relations.length))throw new Error('analysis dimension mismatch');
const fixtureIndex=new Map(fixtures.map((f,i)=>[f.source+':'+f.feature,i]));
const routes=measurements.records.filter(r=>fixtureIndex.has(r.source+':'+r.feature));
if(routes.length!==2496)throw new Error(`route count ${routes.length}, want 2496`);
const zeros=(r,c)=>Array.from({length:r},()=>Array(c).fill(0));
const transpose=a=>a[0].map((_,j)=>a.map(r=>r[j]));
const multiply=(a,b)=>{const bt=transpose(b),out=zeros(a.length,b[0].length);for(let i=0;i<a.length;i++)for(let k=0;k<a[0].length;k++){const v=a[i][k];if(!v)continue;for(let j=0;j<bt.length;j++)out[i][j]+=v*b[k][j];}return out;};
const hadamard=(a,b)=>a.map((r,i)=>r.map((v,j)=>v*b[i][j]));
const fixtureSemantic=analysis.map(a=>a.semantic),fixtureFeatures=analysis.map(a=>a.features),fixtureRelations=analysis.map(a=>a.relation_counts),fixtureRelationKnown=analysis.map(a=>a.relation_known);
// L is stored sparsely but multiplied as a real one-hot projection matrix.
const L=zeros(routes.length,fixtures.length);routes.forEach((r,i)=>L[i][fixtureIndex.get(r.source+':'+r.feature)]=1);
const routeSemantic=multiply(L,fixtureSemantic),routeFeatures=multiply(L,fixtureFeatures),routeRelations=multiply(L,fixtureRelations),routeRelationKnown=multiply(L,fixtureRelationKnown);
const stageFailure=routes.map(r=>stages.map(s=>r[s]==='FAIL'?1:0)),stageKnown=routes.map(r=>stages.map(s=>r[s]==='UNKNOWN'?0:1));
const overallFailure=routes.map(r=>[r.overall==='FAIL'?1:0]),overallKnown=routes.map(r=>[r.overall==='UNKNOWN'?0:1]);
const sourceIncidence=zeros(routes.length,languages.length),targetIncidence=zeros(routes.length,languages.length);
routes.forEach((r,i)=>{sourceIncidence[i][languages.indexOf(r.source)]=1;targetIncidence[i][languages.indexOf(r.target)]=1;});
const repeatColumn=(column,width)=>column.map(([v])=>Array(width).fill(v));
const semanticFailure=hadamard(routeSemantic,repeatColumn(overallFailure,semantics.length));
const semanticKnown=hadamard(routeSemantic,repeatColumn(overallKnown,semantics.length));
const semanticPass=semanticKnown.map((r,i)=>r.map((v,j)=>v-semanticFailure[i][j]));
const relationKnownExpanded=routeRelationKnown;
const design=routes.map((_,i)=>[
 ...routeSemantic[i],...routeRelations[i],...stageFailure[i],...stageKnown[i],...sourceIncidence[i],...targetIncidence[i]
]);
const designKnown=routes.map((_,i)=>[
 ...Array(semantics.length).fill(1),...relationKnownExpanded[i],...stageKnown[i],...stageKnown[i].map(()=>1),...Array(languages.length*2).fill(1)
]);
const gram=multiply(transpose(design),design),knownGram=multiply(transpose(designKnown),designKnown);
const semanticByStageFailure=multiply(transpose(routeSemantic),stageFailure);
const semanticByStageKnown=multiply(transpose(routeSemantic),stageKnown);
const semanticByTargetFailure=multiply(transpose(targetIncidence),semanticFailure);
const semanticByTargetKnown=multiply(transpose(targetIncidence),semanticKnown);
const semanticBySourceFailure=multiply(transpose(sourceIncidence),semanticFailure);
const semanticBySourceKnown=multiply(transpose(sourceIncidence),semanticKnown);
const featureSemantic=multiply(transpose(fixtureFeatures),fixtureSemantic);
const semanticRelation=multiply(transpose(fixtureSemantic),fixtureRelations);
const sparseL={rows:L.length,cols:L[0].length,row_fixture_index:routes.map(r=>fixtureIndex.get(r.source+':'+r.feature))};
const semanticSummary=semantics.map((semantic,j)=>{const required=routeSemantic.reduce((n,r)=>n+r[j],0),known=semanticKnown.reduce((n,r)=>n+r[j],0),fail=semanticFailure.reduce((n,r)=>n+r[j],0);return {semantic,required_routes:required,known,pass:known-fail,fail,unknown:required-known,error_rate_known:known?fail/known:null};});
const relationCoverage=relations.map((relation,j)=>({relation,known_fixtures:fixtureRelationKnown.reduce((n,r)=>n+r[j],0),unknown_fixtures:fixtures.length-fixtureRelationKnown.reduce((n,r)=>n+r[j],0),edge_count:fixtureRelations.reduce((n,r)=>n+r[j],0)}));
const result={schema_version:2,input:{measurement_source_hash:measurements.summary.source_tree_hash,measurements_sha256:createHash('sha256').update(await fs.readFile(path.join(dir,'measurements.json'))).digest('hex')},
 dimensions:{fixtures:fixtures.length,routes:routes.length,semantic_dimensions:semantics.length,relations:relations.length,stages:stages.length,design_columns:design[0].length,design_cells:design.length*design[0].length,gram_cells:gram.length*gram[0].length},
 axes:{languages,features,semantics,relations,stages,design_columns:[...semantics,...relations,...stages.map(s=>'fail_'+s),...stages.map(s=>'known_'+s),...languages.map(l=>'source_'+l),...languages.map(l=>'target_'+l)]},
 matrices:{fixture_semantic:fixtureSemantic,fixture_feature:fixtureFeatures,fixture_relation_count:fixtureRelations,fixture_relation_known:fixtureRelationKnown,route_projection_sparse:sparseL,route_semantic:routeSemantic,route_feature:routeFeatures,route_relation_count:routeRelations,route_relation_known:routeRelationKnown,stage_failure:stageFailure,stage_known:stageKnown,source_incidence:sourceIncidence,target_incidence:targetIncidence,design,design_known:designKnown,gram,known_gram:knownGram,semantic_stage_failure:semanticByStageFailure,semantic_stage_known:semanticByStageKnown,semantic_target_failure:semanticByTargetFailure,semantic_target_known:semanticByTargetKnown,semantic_source_failure:semanticBySourceFailure,semantic_source_known:semanticBySourceKnown,feature_semantic:featureSemantic,semantic_relation:semanticRelation},
 fixture_analysis:fixtures.map((f,i)=>({source:f.source,feature:f.feature,node_count:analysis[i].node_count,graph_error:analysis[i].graph_error??'',semantic:analysis[i].semantic,relation_counts:analysis[i].relation_counts,relation_known:analysis[i].relation_known})),semantic_summary:semanticSummary,relation_coverage:relationCoverage,
 rules:['All axes are unweighted.','Unknown graph relations use a separate mask; zero is not interpreted as a known absence.','Semantic co-failure counts overlap because one route can require several semantic dimensions.','The raw structural scanner is quote-aware but is not full language parsing.']};
await fs.writeFile(path.join(dir,'semantic_matrix_v2.json'),JSON.stringify(result,null,2));
await fs.writeFile(path.join(dir,'semantic_matrix_v2_summary.json'),JSON.stringify({schema_version:2,input:result.input,dimensions:result.dimensions,semantic_summary:semanticSummary,relation_coverage:relationCoverage,rules:result.rules},null,2));
console.log(JSON.stringify({dimensions:result.dimensions,relations:relationCoverage,semantics:semanticSummary},null,2));
