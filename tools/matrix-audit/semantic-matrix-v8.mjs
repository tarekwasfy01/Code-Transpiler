import fs from 'node:fs/promises';
import path from 'node:path';
import {createHash} from 'node:crypto';

const out = path.resolve('outputs/transpiler-audit-2026-08-30');
const v5Bytes = await fs.readFile(path.join(out, 'semantic_matrix_v5.json'));
const v7Bytes = await fs.readFile(path.join(out, 'semantic_matrix_v7.json'));
const measurementBytes = await fs.readFile(path.join(out, 'measurements.json'));
const v5 = JSON.parse(v5Bytes), v7 = JSON.parse(v7Bytes), measurements = JSON.parse(measurementBytes);
const axes = v5.axes.lexical_axes, semantics = v5.axes.semantics, features = v5.axes.features, languages = v5.axes.languages;
const roles = ['code', 'scaffold', 'trivia'];
const structures = ['program','block','assign','print','return','expression','if','else','while','for','function','call','index','module','object','exception','concurrency','reflection'];
const zeros = (r,c) => Array.from({length:r},()=>Array(c).fill(0));
const multiply = (a,b) => { const z=zeros(a.length,b[0].length); for(let i=0;i<a.length;i++)for(let k=0;k<a[i].length;k++)if(a[i][k])for(let j=0;j<b[k].length;j++)z[i][j]+=a[i][k]*b[k][j]; return z; };
const threshold = m => m.map(row=>row.map(value=>value?1:0));
const transpose=m=>m[0].map((_,j)=>m.map(row=>row[j]));
const roleProjection=zeros(axes.length,roles.length);
for(let i=0;i<axes.length;i++) roleProjection[i][roles.indexOf('code')]=1;
for(const axis of ['module','object']) { roleProjection[axes.indexOf(axis)][roles.indexOf('scaffold')]=1; roleProjection[axes.indexOf(axis)][roles.indexOf('code')]=0; }
roleProjection[axes.indexOf('comment')][roles.indexOf('trivia')]=1; roleProjection[axes.indexOf('comment')][roles.indexOf('code')]=0;
const structureProjection=zeros(axes.length,structures.length);
const links={block:'block',binding:'assign',print:'print',return:'return',if:'if',while:'while',for:'for',function:'function',call:'call',index:'index',module:'module',object:'object',exception:'exception',concurrency:'concurrency',reflection:'reflection'};
for(const [axis,structure] of Object.entries(links)) structureProjection[axes.indexOf(axis)][structures.indexOf(structure)]=1;
const tokenRole=threshold(multiply(v5.matrices.token_lexical_axis,roleProjection));
const tokenStructure=threshold(multiply(v5.matrices.token_lexical_axis,structureProjection));
const fixtureRoleCount=zeros(v5.dimensions.fixtures,roles.length),fixtureStructureCount=zeros(v5.dimensions.fixtures,structures.length);
v5.matrices.fixture_token_incidence_sparse.forEach((range,f)=>{for(let i=range.offset;i<range.offset+range.count;i++){for(let j=0;j<roles.length;j++)fixtureRoleCount[f][j]+=tokenRole[i][j];for(let j=0;j<structures.length;j++)fixtureStructureCount[f][j]+=tokenStructure[i][j];}});
const fixtureRoleBinary=threshold(fixtureRoleCount),fixtureStructureBinary=threshold(fixtureStructureCount);
const contractStructureProjection=zeros(features.length,structures.length);
const featureStructure={binding:'assign',reassignment:'assign',if_else:'if',while:'while',for:'for',function:'function',index:'index',objects:'object',exceptions:'exception',modules:'module',concurrency:'concurrency',reflection:'reflection'};
for(const [feature,structure] of Object.entries(featureStructure))contractStructureProjection[features.indexOf(feature)][structures.indexOf(structure)]=1;
const fixtureContractStructure=threshold(multiply(v5.matrices.fixture_contract,contractStructureProjection));
const fixtureStructureRequired=fixtureStructureBinary.map((row,i)=>row.map((value,j)=>value||fixtureContractStructure[i][j]?1:0));
const routeFixture=v5.matrices.route_fixture_projection_sparse;
const routeRoleCount=routeFixture.map(i=>fixtureRoleCount[i]),routeRoleBinary=routeFixture.map(i=>fixtureRoleBinary[i]);
const routeStructureCount=routeFixture.map(i=>fixtureStructureCount[i]),routeStructureRequired=routeFixture.map(i=>fixtureStructureRequired[i]);
const stageFailure=v5.matrices.stage_failure,stageKnown=v5.matrices.stage_known;
const structureFailure=multiply(transpose(routeStructureRequired),stageFailure);
const structureKnown=multiply(transpose(routeStructureRequired),stageKnown);
// Aggregate the measured pair-feature tensor into target-feature capability and mask matrices.
const successCount=zeros(languages.length,features.length),knownCount=zeros(languages.length,features.length);
for(const record of measurements.records){const t=languages.indexOf(record.target),f=features.indexOf(record.feature);if(t<0||f<0)continue;if(record.overall!=='UNKNOWN')knownCount[t][f]++;if(record.overall==='PASS')successCount[t][f]++;}
const targetKnown=threshold(knownCount),targetSupport=threshold(successCount);
const targetMissing=targetKnown.map((row,i)=>row.map((known,j)=>known&&!targetSupport[i][j]?1:0));
const targetUnknown=targetKnown.map(row=>row.map(known=>known?0:1));
const fixtureRequirements=v5.matrices.fixture_contract;
// Explicit transposes avoid hiding the two deficit equations.
const knownMissingDeficit=multiply(fixtureRequirements,transpose(targetMissing));
const unknownDeficit=multiply(fixtureRequirements,transpose(targetUnknown));
const priorColumns=v7.dimensions.expanded_columns;
const addedColumns=roles.length*2+structures.length*2+roles.length*5;
const result={schema_version:8,input:{v5_sha256:createHash('sha256').update(v5Bytes).digest('hex'),v7_sha256:createHash('sha256').update(v7Bytes).digest('hex'),measurements_sha256:createHash('sha256').update(measurementBytes).digest('hex')},
 dimensions:{tokens:v5.dimensions.tokens,fixtures:v5.dimensions.fixtures,routes:v5.dimensions.routes,roles:roles.length,structures:structures.length,target_capability_cells:languages.length*features.length,prior_columns:priorColumns,added_columns:addedColumns,expanded_columns:priorColumns+addedColumns,expanded_cells:v5.dimensions.routes*(priorColumns+addedColumns)},
 axes:{languages,features,lexical_axes:axes,roles,structures},matrices:{lexical_role_projection:roleProjection,lexical_structure_projection:structureProjection,token_role:tokenRole,token_structure:tokenStructure,fixture_role_count:fixtureRoleCount,fixture_role_binary:fixtureRoleBinary,fixture_structure_count:fixtureStructureCount,fixture_structure_observed:fixtureStructureBinary,contract_structure_projection:contractStructureProjection,fixture_structure_required:fixtureStructureRequired,route_role_count:routeRoleCount,route_role_binary:routeRoleBinary,route_structure_count:routeStructureCount,route_structure_required:routeStructureRequired,structure_stage_failure:structureFailure,structure_stage_known:structureKnown,target_feature_success_count:successCount,target_feature_known_count:knownCount,target_feature_support:targetSupport,target_feature_known:targetKnown,known_missing_deficit:knownMissingDeficit,unknown_deficit:unknownDeficit},
 summary:{target_known_cells:targetKnown.flat().reduce((a,b)=>a+b,0),target_supported_cells:targetSupport.flat().reduce((a,b)=>a+b,0),known_missing_deficit_total:knownMissingDeficit.flat().reduce((a,b)=>a+b,0),unknown_deficit_total:unknownDeficit.flat().reduce((a,b)=>a+b,0),role_token_counts:roles.map((role,j)=>({role,count:tokenRole.reduce((n,row)=>n+row[j],0)})),structure_fixture_counts:structures.map((structure,j)=>({structure,observed:fixtureStructureBinary.reduce((n,row)=>n+row[j],0),required:fixtureStructureRequired.reduce((n,row)=>n+row[j],0)}))},
 rules:['Token roles equal threshold(Lexical axes times lexical-role projection).','Structural requirements equal Boolean union of observed structure and contract-projected structure.','Target support and target known masks are separate; UNKNOWN is never converted to failure or success.','Known missing deficit is Requirements times TargetMissing transpose; unknown deficit is Requirements times TargetUnknown transpose.','No feature weights or route priorities are used.']};
await fs.writeFile(path.join(out,'semantic_matrix_v8.json'),JSON.stringify(result));await fs.writeFile(path.join(out,'semantic_matrix_v8_summary.json'),JSON.stringify({schema_version:8,input:result.input,dimensions:result.dimensions,summary:result.summary,rules:result.rules},null,2));console.log(JSON.stringify({dimensions:result.dimensions,summary:result.summary,rules:result.rules},null,2));
