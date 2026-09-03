import fs from 'node:fs/promises';
import path from 'node:path';
import {createHash} from 'node:crypto';

const out=path.resolve('outputs/transpiler-audit-2026-08-30');
const v5Bytes=await fs.readFile(path.join(out,'semantic_matrix_v5.json')),v8Bytes=await fs.readFile(path.join(out,'semantic_matrix_v8.json'));
const v5=JSON.parse(v5Bytes),v8=JSON.parse(v8Bytes);
const fixtures=JSON.parse(await fs.readFile(path.join(out,'fixtures.json')));
const {languages,features}=v5.axes,{structures,roles}=v8.axes;
const actions=['skip','block','assign','print','return','expression','if','else','while','for','function','call','index','module','object','exception','concurrency','reflection'];
const zeros=(r,c)=>Array.from({length:r},()=>Array(c).fill(0));
const transpose=m=>m[0].map((_,j)=>m.map(row=>row[j]));
const multiply=(a,b)=>{const z=zeros(a.length,b[0].length);for(let i=0;i<a.length;i++)for(let k=0;k<a[i].length;k++)if(a[i][k])for(let j=0;j<b[k].length;j++)z[i][j]+=a[i][k]*b[k][j];return z;};
const threshold=m=>m.map(row=>row.map(v=>v?1:0));
const structureAction=zeros(structures.length,actions.length);for(const structure of structures){const a=actions.indexOf(structure);if(a>=0)structureAction[structures.indexOf(structure)][a]=1;}
structureAction[structures.indexOf('program')][actions.indexOf('skip')]=1;
const routeActionRequired=threshold(multiply(v8.matrices.route_structure_required,structureAction));
// Initial lowerings reflect the four statement classes in the old all-to-all emitter.
const initialSupport=zeros(languages.length,actions.length),known=zeros(languages.length,actions.length);
for(let l=0;l<languages.length;l++)for(let a=0;a<actions.length;a++)known[l][a]=1;
for(const action of ['skip','assign','print','return','expression','call','index','module','object'])for(let l=0;l<languages.length;l++)initialSupport[l][actions.indexOf(action)]=1;
const missing=known.map((row,l)=>row.map((value,a)=>value-initialSupport[l][a]));
const routeTargetMissing=multiply(routeActionRequired,transpose(missing));
const actionMissingLoad=multiply(transpose(routeActionRequired),routeTargetMissing);
const tokenStructure=v8.matrices.token_structure,tokenRole=v8.matrices.token_role;
const structuralTransition=zeros(structures.length,structures.length),roleTransition=zeros(roles.length,roles.length),languageStructuralTransition=languages.map(()=>zeros(structures.length,structures.length));
const ranges=v5.matrices.fixture_token_incidence_sparse,control=v5.matrices.token_control_sparse;
const controlByFixture=ranges.map(()=>[]);let fixture=0;
for(const edge of control){while(fixture+1<ranges.length&&edge[0]>=ranges[fixture].offset+ranges[fixture].count)fixture++;controlByFixture[fixture].push(edge);}
for(let f=0;f<ranges.length;f++){
 const sourceLanguage=languages.indexOf(fixtures[f].source);
 for(const [from,to] of controlByFixture[f])for(let a=0;a<structures.length;a++)if(tokenStructure[from][a])for(let b=0;b<structures.length;b++)if(tokenStructure[to][b]){structuralTransition[a][b]+=tokenStructure[from][a]*tokenStructure[to][b];languageStructuralTransition[sourceLanguage][a][b]+=tokenStructure[from][a]*tokenStructure[to][b];}
 for(const [from,to] of controlByFixture[f])for(let a=0;a<roles.length;a++)if(tokenRole[from][a])for(let b=0;b<roles.length;b++)if(tokenRole[to][b])roleTransition[a][b]+=tokenRole[from][a]*tokenRole[to][b];
}
const roleStructure=multiply(transpose(tokenRole),tokenStructure);
const prior=v8.dimensions.expanded_columns,added=actions.length*2+structures.length*actions.length;
const result={schema_version:9,input:{v5_sha256:createHash('sha256').update(v5Bytes).digest('hex'),v8_sha256:createHash('sha256').update(v8Bytes).digest('hex')},dimensions:{routes:v5.dimensions.routes,languages:languages.length,structures:structures.length,actions:actions.length,prior_columns:prior,added_columns:added,expanded_columns:prior+added,expanded_cells:v5.dimensions.routes*(prior+added),lowering_support_cells:languages.length*actions.length,route_target_deficit_cells:v5.dimensions.routes*languages.length},axes:{languages,features,roles,structures,actions},matrices:{structure_action_projection:structureAction,route_action_required:routeActionRequired,initial_target_action_support:initialSupport,target_action_known:known,initial_target_action_missing:missing,route_target_action_deficit:routeTargetMissing,action_missing_load:actionMissingLoad,structural_transition:structuralTransition,language_structural_transition:languageStructuralTransition,role_transition:roleTransition,role_structure_incidence:roleStructure},summary:{required_action_routes:actions.map((action,j)=>({action,routes:routeActionRequired.reduce((n,row)=>n+row[j],0)})),missing_action_target_cells:missing.flat().reduce((a,b)=>a+b,0),route_target_missing_total:routeTargetMissing.flat().reduce((a,b)=>a+b,0),structural_transition_edges:structuralTransition.flat().reduce((a,b)=>a+b,0)},rules:['Structure-to-action projection is a matrix, not a parser switch.','Route action requirements equal threshold(RouteStructure times StructureAction).','Initial target deficits equal RouteActionRequirements times TargetActionMissing transpose.','Transition matrices are S transpose A S for structural token vectors and R transpose A R for role vectors.','All target languages use the same action basis and equal weights.']};
await fs.writeFile(path.join(out,'semantic_matrix_v9.json'),JSON.stringify(result));await fs.writeFile(path.join(out,'semantic_matrix_v9_summary.json'),JSON.stringify({schema_version:9,input:result.input,dimensions:result.dimensions,summary:result.summary,rules:result.rules},null,2));console.log(JSON.stringify({dimensions:result.dimensions,summary:result.summary,rules:result.rules},null,2));
