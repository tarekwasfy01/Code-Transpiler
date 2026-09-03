from pathlib import Path
import csv, json, shutil, zipfile, math
from collections import defaultdict, Counter, deque
import numpy as np

SRC = Path('/mnt/data/universal_semantic_matrix_v2')
OUT = Path('/mnt/data/universal_ast_schema_matrix_v1')
ZIP = Path('/mnt/data/universal_ast_schema_matrix_v1_codex_handoff.zip')
if OUT.exists(): shutil.rmtree(OUT)
OUT.mkdir(parents=True)

# ---------- helpers ----------
def read_matrix(name):
    with (SRC/name).open('r', encoding='utf-8-sig', newline='') as f:
        rows=list(csv.reader(f))
    cols=rows[0][1:]
    idx=[r[0] for r in rows[1:]]
    arr=np.array([[float(x or 0) for x in r[1:]] for r in rows[1:]], dtype=float)
    return idx, cols, arr

def write_matrix(path, row_labels, col_labels, a, fmt=None):
    with path.open('w', encoding='utf-8', newline='') as f:
        w=csv.writer(f); w.writerow(['row']+list(col_labels))
        for i,r in enumerate(row_labels):
            vals=[]
            for x in a[i]:
                if fmt: vals.append(fmt(x))
                elif isinstance(x, (np.integer,int)): vals.append(int(x))
                elif abs(float(x)-round(float(x)))<1e-12: vals.append(int(round(float(x))))
                else: vals.append(f'{float(x):.8f}')
            w.writerow([r]+vals)

def write_rows(path, header, rows):
    with path.open('w',encoding='utf-8',newline='') as f:
        w=csv.writer(f); w.writerow(header); w.writerows(rows)

def b(a): return (a>0).astype(np.int8)

def bool_mm(a,bm): return b(a.astype(np.int64) @ bm.astype(np.int64))

features, axes, S = read_matrix('04_feature_semantic_axis_matrix.csv')
features2, rel_axes, R = read_matrix('20_feature_relation_matrix.csv')
langs, features3, P = read_matrix('02_language_feature_presence_matrix.csv')
langs2, features4, Clo = read_matrix('06a_language_feature_sp_coverage_lower_matrix.csv')
langs3, features5, Chi = read_matrix('06b_language_feature_sp_coverage_upper_matrix.csv')
features6, structural_nodes, U_seed = read_matrix('19_feature_uast_node_matrix.csv')
structural_nodes2, rel_axes2, N_seed = read_matrix('21_uast_node_relation_matrix.csv')
features7, features8, K_feature = read_matrix('11_feature_coupling_union_max_matrix.csv')
assert features==features2==features3==features4==features5==features6==features7==features8
assert langs==langs2==langs3
assert structural_nodes==structural_nodes2 and rel_axes==rel_axes2
S=b(S); R=b(R); P=b(P); U_seed=b(U_seed); N_seed=b(N_seed)

# Feature metadata
with (SRC/'01_feature_catalog.csv').open('r',encoding='utf-8-sig',newline='') as f:
    meta=list(csv.DictReader(f))
meta_by={r['feature']:r for r in meta}
origins=sorted(set((r['origin'] or 'unknown') for r in meta))
categories=sorted(set((r['category'] or 'uncategorized') for r in meta))

# ---------- semantic facet quotient: exact row equivalence of [S|R] ----------
sig=np.concatenate([S,R],axis=1).astype(np.int8)
groups=defaultdict(list)
for i,row in enumerate(sig): groups[tuple(int(x) for x in row)].append(i)
# deterministic ordering by signature axes/relations then feature name
def group_key(item):
    signature, inds=item
    sa=tuple(axes[i] for i,x in enumerate(signature[:len(axes)]) if x)
    ra=tuple(rel_axes[i] for i,x in enumerate(signature[len(axes):]) if x)
    return (sa,ra,min(features[i] for i in inds))
ordered=sorted(groups.items(), key=group_key)
classes=[f'UASF_{i+1:04d}' for i in range(len(ordered))]
Q=np.zeros((len(features),len(classes)),dtype=np.int8)
class_rows=[]
for ci,(signature,inds) in enumerate(ordered):
    Q[inds,ci]=1
    sa=[axes[i] for i,x in enumerate(signature[:len(axes)]) if x]
    ra=[rel_axes[i] for i,x in enumerate(signature[len(axes):]) if x]
    feats=[features[i] for i in inds]
    cats=sorted(set(meta_by[f]['category'] or 'uncategorized' for f in feats))
    orgs=sorted(set(meta_by[f]['origin'] or 'unknown' for f in feats))
    lang_sig=[]
    for li,l in enumerate(langs):
        if any(P[li,i] for i in inds): lang_sig.append(l)
    class_rows.append([
        classes[ci], len(inds), ';'.join(sa), ';'.join(ra), ';'.join(cats), ';'.join(orgs),
        ';'.join(lang_sig), ';'.join(feats)
    ])

ClassAxis=b(Q.T@S)
ClassRelAxis=b(Q.T@R)
LanguageClassCount=(P@Q).astype(int)
LanguageClassPresence=b(LanguageClassCount)
FeatureEquiv=(Q@Q.T).astype(np.int8)
ExistingNodeClass=(U_seed.T@Q).astype(int)
ClassExistingNode=b(ExistingNodeClass.T)

# class coverage fractions from feature coverage intervals
present_count=LanguageClassCount.astype(float)
covered_lo=(P*Clo)@Q
covered_hi=(P*Chi)@Q
with np.errstate(divide='ignore', invalid='ignore'):
    ClassCovLo=np.divide(covered_lo,present_count,out=np.zeros_like(covered_lo),where=present_count>0)
    ClassCovHi=np.divide(covered_hi,present_count,out=np.zeros_like(covered_hi),where=present_count>0)

# ---------- universal layers ----------
layers=[
 'structure.declaration','structure.expression','type.value','binding.scope','control.flow','data.flow',
 'effect','evaluation','exception','memory.lifetime','concurrency','compiletime.meta','module.dispatch',
 'abi.layout','lowering.runtime.validation','language.contract','dialect.extension'
]
AxisLayer=np.zeros((len(axes),len(layers)),dtype=np.int8)
def al(axis,*ls):
    i=axes.index(axis)
    for x in ls: AxisLayer[i,layers.index(x)]=1
# explicit, multi-membership where semantically appropriate
for a in axes:
    if a=='nodes.declaration': al(a,'structure.declaration')
    elif a in ('nodes.expression','operations'): al(a,'structure.expression','data.flow')
    elif a in ('types.origin','types.structure','conversion.semantics','value.category'): al(a,'type.value')
    elif a in ('binding.graph','capture.shadowing','scope.binding','name.resolution'): al(a,'binding.scope')
    elif a in ('control.flow',): al(a,'control.flow')
    elif a in ('data.flow',): al(a,'data.flow')
    elif a in ('effect.graph','effects.control','purity'): al(a,'effect')
    elif a in ('evaluation.order','initialization.order'): al(a,'evaluation')
    elif a in ('exception.unwind',): al(a,'exception')
    elif a in ('memory.model','object.lifetime','ownership.lifetime','pointer.provenance','storage.duration'): al(a,'memory.lifetime')
    elif a in ('concurrency.model','memory.ordering','synchronization','coroutine.state_machine'): al(a,'concurrency')
    elif a in ('compiletime.evaluation','compiletime.semantics','template.instantiation','preprocessor.semantics'): al(a,'compiletime.meta')
    elif a in ('module.semantics','linkage.odr','overload.resolution'): al(a,'module.dispatch')
    elif a in ('abi.ffi','abi.layout'): al(a,'abi.layout')
    elif a in ('lowering.pipeline','runtime.contract','validation.contract'): al(a,'lowering.runtime.validation')
    elif a in ('language.implementation_defined','language.undefined_behavior','language.unspecified_behavior'): al(a,'language.contract')
    elif a in ('dialect.extension',): al(a,'dialect.extension')
    else: raise RuntimeError('unclassified axis '+a)
ClassLayer=b(ClassAxis@AxisLayer)
NodeAxisSeed=b(U_seed.T@S)
NodeLayer=b(NodeAxisSeed@AxisLayer)

# ---------- concrete relation vocabulary ----------
# Start from existing relation schema then extend gaps needed by semantic axes.
with (SRC/'26_universal_relation_schema.csv').open('r',encoding='utf-8-sig',newline='') as f:
    base_rel=list(csv.DictReader(f))
relation_catalog={r['relation']:[r['meaning'],r['applies_to'],r['layer']] for r in base_rel}
ext_rel={
 'annotation.applies':('annotation -> semantic target','annotations','core'),
 'compiletime.depends':('compile-time computation -> dependency','compile-time','analysis'),
 'contract.requires':('semantic entity -> required contract','contracts','core'),
 'runtime.requires':('semantic entity -> runtime contract','runtime','analysis'),
 'lifetime.outlives':('region/lifetime -> region/lifetime','lifetimes','core'),
 'module.imports':('module -> module/symbol','modules','core'),
 'module.exports':('module -> exported symbol','modules','core'),
 'name.resolves':('reference/name -> declaration/binding','name resolution','core'),
 'linkage.links':('declaration -> linkage identity','linkage','core'),
 'pointer.provenance':('pointer/reference -> provenance object/region','pointer semantics','core'),
 'initialization.before':('initialization -> initialization','initialization order','core'),
 'exception.unwinds_to':('throw/failure -> handler/cleanup','exceptions','core'),
 'template.instantiates':('generic/template -> instantiation','generics','core'),
 'overload.candidate':('call/operator -> candidate declaration','overload resolution','core'),
 'storage.resides_in':('value/object -> storage region','storage duration','core'),
 'value.category':('expression/value -> value category','value semantics','core'),
 'preprocessor.expands':('macro/directive -> expansion','preprocessor','dialect'),
 'coroutine.suspends':('suspension point -> continuation/state','coroutines','core'),
 'validation.proves':('evidence -> validated contract','validation','analysis'),
 'conversion.converts':('source value/type -> target value/type','conversions','core'),
 'type.origin':('typed entity -> source/inference origin','type evidence','analysis'),
 'operation.kind':('operation node -> semantic operation kind','operations','core'),
 'language.contract':('node/operation -> language-defined/undefined contract','language semantics','core')
}
for k,v in ext_rel.items(): relation_catalog.setdefault(k,list(v))
concrete_relations=sorted(relation_catalog)

# Crosswalk relation-axis -> concrete relations
RA_to_CR={
 'abi.graph':['abi.calls'],
 'annotation.graph':['annotation.applies'],
 'binding.graph':['binding.declares','binding.refers','binding.shadows','name.resolves'],
 'call.graph':['call.calls'],
 'capture.graph':['capture.captures'],
 'compiletime.graph':['compiletime.depends'],
 'concurrency.graph':['concurrency.spawns','concurrency.communicates','concurrency.synchronizes','concurrency.atomic_order'],
 'constraint.graph':['type.constraint','contract.requires'],
 'contract.graph':['contract.requires'],
 'control.flow':['control.next','control.true','control.false','control.loop_back'],
 'data.flow':['data.def_use','data.operand','data.result'],
 'dialect.extension':['dialect.requires'],
 'dispatch.graph':['dispatch.resolves','overload.candidate'],
 'effect.graph':['effect.has'],
 'layout.graph':['layout.field'],
 'lifetime.graph':['lifetime.outlives','memory.borrows'],
 'lowering.graph':['lowering.requires'],
 'memory.graph':['memory.owns','memory.borrows','memory.aliases','storage.resides_in'],
 'ownership.graph':['memory.owns','memory.borrows','lifetime.outlives'],
 'runtime.graph':['runtime.requires'],
 'scope.binding':['scope.parent','binding.declares','binding.refers','binding.shadows','capture.captures','name.resolves'],
 'scope.graph':['scope.parent'],
 'types.graph':['type.has','type.parameter','type.constraint','type.convert','type.origin']
}
RA_CR=np.zeros((len(rel_axes),len(concrete_relations)),dtype=np.int8)
for ra,crs in RA_to_CR.items():
    for cr in crs: RA_CR[rel_axes.index(ra),concrete_relations.index(cr)]=1
# semantic-axis -> concrete relation additions
SA_to_CR={
 'evaluation.order':['evaluation.before'], 'initialization.order':['initialization.before'],
 'exception.unwind':['exception.unwinds_to'], 'template.instantiation':['template.instantiates'],
 'preprocessor.semantics':['preprocessor.expands'], 'module.semantics':['module.imports','module.exports'],
 'name.resolution':['name.resolves'], 'linkage.odr':['linkage.links'], 'pointer.provenance':['pointer.provenance'],
 'storage.duration':['storage.resides_in'], 'value.category':['value.category'],
 'conversion.semantics':['conversion.converts'], 'coroutine.state_machine':['coroutine.suspends'],
 'validation.contract':['validation.proves'], 'runtime.contract':['runtime.requires'],
 'types.origin':['type.origin'], 'operations':['operation.kind'],
 'language.implementation_defined':['language.contract'], 'language.undefined_behavior':['language.contract'],
 'language.unspecified_behavior':['language.contract'], 'dialect.extension':['dialect.requires']
}
SA_CR=np.zeros((len(axes),len(concrete_relations)),dtype=np.int8)
for sa,crs in SA_to_CR.items():
    for cr in crs: SA_CR[axes.index(sa),concrete_relations.index(cr)]=1
ClassConcreteRel=b(ClassRelAxis@RA_CR + ClassAxis@SA_CR)
NodeConcreteRel=b(N_seed@RA_CR + NodeAxisSeed@SA_CR)

# ---------- field schema ----------
base_fields=['id','kind','source_span','semantic_facets','attributes','extensions']
semantic_fields=[
 'scope_id','binding_refs','name','symbol','type_ref','type_origin','type_shape','value_category','operation','value','operands','results',
 'callee','arguments','parameters','receiver','body','condition','branches','pattern','members','constraints','conversion','dispatch','candidates',
 'effects','evaluation_order','initialization_order','exception_model','unwind','ownership','lifetime','provenance','storage','memory_model',
 'memory_order','synchronization','concurrency_contract','coroutine_state','compiletime_contract','compiletime_value','module','linkage',
 'layout','abi_contract','calling_convention','lowering','runtime_contract','validation','language_contract','dialect'
]
fields=base_fields+semantic_fields
SA_Field=np.zeros((len(axes),len(fields)),dtype=np.int8)
def sf(sa,*fs):
    for x in fs: SA_Field[axes.index(sa),fields.index(x)]=1
sf('abi.ffi','abi_contract','calling_convention'); sf('abi.layout','layout','abi_contract')
sf('binding.graph','binding_refs'); sf('capture.shadowing','binding_refs')
sf('compiletime.evaluation','compiletime_value'); sf('compiletime.semantics','compiletime_contract')
sf('concurrency.model','concurrency_contract'); sf('control.flow','condition','branches')
sf('conversion.semantics','conversion','type_ref'); sf('coroutine.state_machine','coroutine_state')
sf('data.flow','operands','results'); sf('dialect.extension','dialect'); sf('effect.graph','effects'); sf('effects.control','effects')
sf('evaluation.order','evaluation_order'); sf('exception.unwind','exception_model','unwind'); sf('initialization.order','initialization_order')
sf('language.implementation_defined','language_contract'); sf('language.undefined_behavior','language_contract'); sf('language.unspecified_behavior','language_contract')
sf('linkage.odr','linkage'); sf('lowering.pipeline','lowering'); sf('memory.model','memory_model'); sf('memory.ordering','memory_order')
sf('module.semantics','module'); sf('name.resolution','symbol','binding_refs'); sf('nodes.declaration','name'); sf('nodes.expression','operation','value')
sf('object.lifetime','lifetime'); sf('operations','operation','operands','results'); sf('overload.resolution','dispatch','candidates')
sf('ownership.lifetime','ownership','lifetime'); sf('pointer.provenance','provenance'); sf('preprocessor.semantics','compiletime_contract')
sf('purity','effects'); sf('runtime.contract','runtime_contract'); sf('scope.binding','scope_id','binding_refs'); sf('storage.duration','storage')
sf('synchronization','synchronization'); sf('template.instantiation','parameters','constraints'); sf('types.origin','type_origin')
sf('types.structure','type_ref','type_shape'); sf('validation.contract','validation'); sf('value.category','value_category')
# relation -> fields
CR_Field=np.zeros((len(concrete_relations),len(fields)),dtype=np.int8)
def rf(cr,*fs):
    for x in fs: CR_Field[concrete_relations.index(cr),fields.index(x)]=1
for cr in concrete_relations:
    if cr.startswith('binding.') or cr=='name.resolves': rf(cr,'binding_refs','symbol')
    if cr=='scope.parent': rf(cr,'scope_id')
    if cr=='capture.captures': rf(cr,'binding_refs')
    if cr.startswith('type.') or cr=='conversion.converts': rf(cr,'type_ref')
    if cr.startswith('data.') or cr=='operation.kind': rf(cr,'operands','results')
    if cr.startswith('control.'): rf(cr,'branches','condition')
    if cr=='call.calls': rf(cr,'callee','arguments')
    if cr.startswith('dispatch.') or cr=='overload.candidate': rf(cr,'dispatch','candidates')
    if cr=='effect.has': rf(cr,'effects')
    if cr.startswith('memory.') or cr in ('pointer.provenance','storage.resides_in','lifetime.outlives'): rf(cr,'ownership','lifetime','provenance','storage')
    if cr=='layout.field': rf(cr,'layout','members')
    if cr=='abi.calls': rf(cr,'abi_contract','calling_convention')
    if cr.startswith('concurrency.'): rf(cr,'concurrency_contract','memory_order','synchronization')
    if cr=='evaluation.before': rf(cr,'evaluation_order')
    if cr=='initialization.before': rf(cr,'initialization_order')
    if cr=='exception.unwinds_to': rf(cr,'exception_model','unwind')
    if cr.startswith('compiletime.') or cr=='preprocessor.expands': rf(cr,'compiletime_contract')
    if cr.startswith('module.'): rf(cr,'module')
    if cr=='linkage.links': rf(cr,'linkage')
    if cr=='coroutine.suspends': rf(cr,'coroutine_state')
    if cr=='runtime.requires': rf(cr,'runtime_contract')
    if cr=='validation.proves': rf(cr,'validation')
    if cr=='language.contract': rf(cr,'language_contract')
    if cr=='dialect.requires': rf(cr,'dialect')
    if cr=='lowering.requires': rf(cr,'lowering')
    if cr=='annotation.applies': rf(cr,'attributes')

ClassField=b(ClassAxis@SA_Field + ClassConcreteRel@CR_Field)
NodeField=b(NodeAxisSeed@SA_Field + NodeConcreteRel@CR_Field)
# mandatory base fields are always present
for fi in range(len(base_fields)):
    ClassField[:,fi]=1; NodeField[:,fi]=1

# ---------- class coupling + layer connected components ----------
# Project existing feature coupling to quotient classes. Values are sums; boolean form used for connectivity.
ClassCoupling=(Q.T@K_feature@Q)
ClassSemanticOverlap=(ClassAxis@ClassAxis.T).astype(int)
ClassRelationOverlap=(ClassConcreteRel@ClassConcreteRel.T).astype(int)
ClassLanguageOverlap=(LanguageClassPresence.T@LanguageClassPresence).astype(int)

component_cols=[]; comp_members=[]
for li,layer in enumerate(layers):
    members=np.where(ClassLayer[:,li]>0)[0]
    if len(members)==0: continue
    mset=set(int(x) for x in members)
    # Algebraic adjacency restricted to the layer; relation/axis sharing OR projected feature coupling.
    A=((ClassSemanticOverlap>0)|(ClassRelationOverlap>0)|(ClassCoupling>0)).astype(np.int8)
    A=np.logical_and(A, np.outer(ClassLayer[:,li]>0,ClassLayer[:,li]>0)).astype(np.int8)
    np.fill_diagonal(A,1)
    seen=set()
    local_num=0
    for start in members:
        start=int(start)
        if start in seen: continue
        local_num+=1
        q=deque([start]); seen.add(start); comp=[]
        while q:
            u=q.popleft(); comp.append(u)
            nbrs=np.where(A[u]>0)[0]
            for v in nbrs:
                v=int(v)
                if v in mset and v not in seen:
                    seen.add(v); q.append(v)
        cid=f'{layer}::C{local_num:03d}'
        component_cols.append(cid); comp_members.append(comp)
ClassComponent=np.zeros((len(classes),len(component_cols)),dtype=np.int8)
for j,comp in enumerate(comp_members): ClassComponent[comp,j]=1

# ---------- instance schema layout ----------
record_layout=[
 ['UniversalASTDocument','schema_version','1','scalar','required'],
 ['UniversalASTDocument','language_projection','Language×Feature / Language×Facet matrices','matrix refs','required'],
 ['UniversalASTDocument','nodes','NodeInstance[]','table','required'],
 ['UniversalASTDocument','relations','RelationInstance[]','table','required'],
 ['UniversalASTDocument','semantic_facets','UASF vector space','matrix','required'],
 ['UniversalASTDocument','evidence','source/compiler evidence refs','matrix/table','required'],
 ['NodeInstance','id','integer/string stable id','scalar','required'],
 ['NodeInstance','structural_kind','one of 109 structural node kinds','one-hot/index','required'],
 ['NodeInstance','semantic_facets','334-dimensional UASF membership','sparse vector','required'],
 ['NodeInstance','field_mask','calculated field applicability','bit vector','required'],
 ['NodeInstance','source_span','source location','record','optional'],
 ['NodeInstance','attributes','extensible metadata','map','optional'],
 ['RelationInstance','kind','universal concrete relation kind','index','required'],
 ['RelationInstance','from','NodeInstance id','reference','required'],
 ['RelationInstance','to','NodeInstance/contract/type/value id','reference','required'],
 ['RelationInstance','attributes','relation-specific data','map','optional'],
]

# ---------- current SemanticProgram -> universal field crosswalk ----------
sp_rows=[
 ['SemanticStatement.ID','id'],['SemanticStatement.Kind','kind'],['SemanticStatement.Scope','scope_id'],['SemanticStatement.Type','type_ref'],
 ['SemanticStatement.Semantics','operation'],['SemanticStatement.Effects','effects'],['SemanticStatement.Source','source_span'],['SemanticStatement.Attributes','attributes'],['SemanticStatement.Extensions','extensions'],
 ['SemanticExpression.ID','id'],['SemanticExpression.Kind','kind'],['SemanticExpression.Scope','scope_id'],['SemanticExpression.Type','type_ref'],['SemanticExpression.Binding','binding_refs'],
 ['SemanticExpression.Semantics','operation'],['SemanticExpression.Effects','effects'],['SemanticExpression.Source','source_span'],['SemanticExpression.Attributes','attributes'],['SemanticExpression.Extensions','extensions'],
 ['SemanticExpression.Arguments','arguments'],['SemanticFunction.Parameters','parameters'],['SemanticParameter.Passing','ownership'],['SemanticType.TypeOrigin','type_origin'],
 ['SemanticType.Ownership','ownership'],['SemanticType.Lifetime','lifetime'],['SemanticSemantics.EvaluationOrder','evaluation_order'],['SemanticSemantics.Dispatch','dispatch'],
 ['SemanticSemantics.ErrorModel','exception_model'],['SemanticDialect','dialect'],['SemanticContracts','runtime_contract'],['SemanticEvidence','validation']
]

# ---------- write outputs ----------
write_rows(OUT/'01_semantic_facet_catalog.csv',
 ['facet_class','feature_count','semantic_axes','relation_axes','categories','origins','languages','features'], class_rows)
write_matrix(OUT/'02_feature_to_semantic_facet_matrix.csv',features,classes,Q)
write_matrix(OUT/'03_feature_schema_signature_matrix.csv',features,axes+rel_axes,sig)
write_matrix(OUT/'04_feature_equivalence_matrix.csv',features,features,FeatureEquiv)
write_matrix(OUT/'05_semantic_facet_axis_matrix.csv',classes,axes,ClassAxis)
write_matrix(OUT/'06_semantic_facet_relation_axis_matrix.csv',classes,rel_axes,ClassRelAxis)
write_matrix(OUT/'07_language_semantic_facet_count_matrix.csv',langs,classes,LanguageClassCount)
write_matrix(OUT/'08_language_semantic_facet_presence_matrix.csv',langs,classes,LanguageClassPresence)
write_matrix(OUT/'09_semantic_facet_sp_coverage_lower_matrix.csv',langs,classes,ClassCovLo,fmt=lambda x:f'{float(x):.6f}')
write_matrix(OUT/'10_semantic_facet_sp_coverage_upper_matrix.csv',langs,classes,ClassCovHi,fmt=lambda x:f'{float(x):.6f}')
write_matrix(OUT/'11_structural_node_semantic_facet_seed_matrix.csv',structural_nodes,classes,ExistingNodeClass)
write_matrix(OUT/'12_semantic_facet_structural_node_seed_matrix.csv',classes,structural_nodes,ClassExistingNode)
write_rows(OUT/'13_universal_layer_catalog.csv',['layer','description'],[
 ['structure.declaration','declarations and named structural entities'],['structure.expression','expression and operation structure'],['type.value','types, conversion and value categories'],
 ['binding.scope','scope, binding, capture and name resolution'],['control.flow','control-flow semantics'],['data.flow','def-use, operands and results'],['effect','side effects and purity'],
 ['evaluation','evaluation and initialization order'],['exception','throw/unwind/cleanup semantics'],['memory.lifetime','ownership, lifetime, storage, provenance and memory model'],
 ['concurrency','tasks, synchronization, atomics, coroutine state'],['compiletime.meta','compile-time evaluation, templates/macros/preprocessing'],['module.dispatch','modules, linkage and overload/dispatch'],
 ['abi.layout','ABI, FFI and physical layout'],['lowering.runtime.validation','lowering, runtime contracts and validation evidence'],['language.contract','defined/undefined/unspecified language contracts'],
 ['dialect.extension','language/domain-specific extension boundary']])
write_matrix(OUT/'14_semantic_axis_to_layer_matrix.csv',axes,layers,AxisLayer)
write_matrix(OUT/'15_semantic_facet_layer_matrix.csv',classes,layers,ClassLayer)
write_matrix(OUT/'16_structural_node_layer_matrix.csv',structural_nodes,layers,NodeLayer)
write_rows(OUT/'17_concrete_relation_catalog.csv',['relation','meaning','applies_to','layer'],[[k]+relation_catalog[k] for k in concrete_relations])
write_matrix(OUT/'18_relation_axis_to_concrete_relation_matrix.csv',rel_axes,concrete_relations,RA_CR)
write_matrix(OUT/'19_semantic_axis_to_concrete_relation_matrix.csv',axes,concrete_relations,SA_CR)
write_matrix(OUT/'20_semantic_facet_concrete_relation_matrix.csv',classes,concrete_relations,ClassConcreteRel)
write_matrix(OUT/'21_structural_node_concrete_relation_matrix.csv',structural_nodes,concrete_relations,NodeConcreteRel)
write_rows(OUT/'22_universal_field_catalog.csv',['field','base_or_semantic'],[[f,'base' if f in base_fields else 'semantic'] for f in fields])
write_matrix(OUT/'23_semantic_axis_to_field_matrix.csv',axes,fields,SA_Field)
write_matrix(OUT/'24_concrete_relation_to_field_matrix.csv',concrete_relations,fields,CR_Field)
write_matrix(OUT/'25_semantic_facet_field_matrix.csv',classes,fields,ClassField)
write_matrix(OUT/'26_structural_node_field_matrix.csv',structural_nodes,fields,NodeField)
write_matrix(OUT/'27_semantic_facet_coupling_matrix.csv',classes,classes,ClassCoupling,fmt=lambda x:f'{float(x):.6f}')
write_matrix(OUT/'28_semantic_facet_semantic_overlap_matrix.csv',classes,classes,ClassSemanticOverlap)
write_matrix(OUT/'29_semantic_facet_relation_overlap_matrix.csv',classes,classes,ClassRelationOverlap)
write_matrix(OUT/'30_semantic_facet_language_overlap_matrix.csv',classes,classes,ClassLanguageOverlap)
write_matrix(OUT/'31_semantic_facet_layer_component_matrix.csv',classes,component_cols,ClassComponent)
write_rows(OUT/'32_schema_record_layout.csv',['entity','field','domain','representation','cardinality'],record_layout)
write_rows(OUT/'33_current_semanticprogram_to_uast_field_crosswalk.csv',['semanticprogram_field','universal_field'],sp_rows)

# sparse tensor Language x Facet x Axis
with (OUT/'34_language_semantic_facet_axis_tensor.csv').open('w',encoding='utf-8',newline='') as f:
    w=csv.writer(f); w.writerow(['language','facet_class','semantic_axis','value'])
    for li,l in enumerate(langs):
        for ci,c in enumerate(classes):
            if not LanguageClassPresence[li,ci]: continue
            for ai,a in enumerate(axes):
                if ClassAxis[ci,ai]: w.writerow([l,c,a,1])

# instance dimensions + invariant matrix
invariants=[
 ('INV001','feature_exactly_one_facet','Each feature maps to exactly one UASF quotient class.'),
 ('INV002','facet_signature_exact','All features in a facet have identical SemanticAxis+RelationAxis vectors.'),
 ('INV003','structural_kind_one_hot','Each structural AST node instance has exactly one structural_kind.'),
 ('INV004','facet_vector_sparse','Each node instance may carry zero or more semantic facets.'),
 ('INV005','relation_kind_closed','Every relation instance uses a relation from the concrete relation catalog.'),
 ('INV006','field_mask_derived','Applicable semantic fields derive from facet/axis/relation matrices.'),
 ('INV007','coverage_interval_preserved','Unknown/partial SemanticProgram coverage remains lower/upper interval data.'),
 ('INV008','no_priority_operator','No ranking/priority vector participates in schema construction.')
]
write_rows(OUT/'35_schema_invariants.csv',['id','name','rule'],invariants)

# structural kind catalog with seed semantics
node_rows=[]
for ni,n in enumerate(structural_nodes):
    sa=';'.join(axes[i] for i in np.where(NodeAxisSeed[ni]>0)[0])
    ra=';'.join(concrete_relations[i] for i in np.where(NodeConcreteRel[ni]>0)[0])
    ly=';'.join(layers[i] for i in np.where(NodeLayer[ni]>0)[0])
    fs=';'.join(fields[i] for i in np.where(NodeField[ni]>0)[0])
    node_rows.append([n,int(U_seed[:,ni].sum()),sa,ra,ly,fs])
write_rows(OUT/'36_structural_node_kind_catalog.csv',['structural_kind','seed_feature_count','semantic_axes','concrete_relations','layers','fields'],node_rows)

# component catalog
comp_rows=[]
for j,cid in enumerate(component_cols):
    inds=np.where(ClassComponent[:,j]>0)[0]
    layer=cid.split('::C')[0]
    comp_rows.append([cid,layer,len(inds),';'.join(classes[i] for i in inds)])
write_rows(OUT/'37_layer_component_catalog.csv',['component','layer','facet_count','facets'],comp_rows)

# matrix labels and NPZ
labels={'languages':langs,'features':features,'semantic_axes':axes,'relation_axes':rel_axes,'structural_nodes':structural_nodes,
        'semantic_facets':classes,'layers':layers,'concrete_relations':concrete_relations,'fields':fields,'layer_components':component_cols}
(OUT/'matrix_labels.json').write_text(json.dumps(labels,indent=2),encoding='utf-8')
np.savez_compressed(OUT/'universal_ast_schema_matrices.npz',
    feature_to_facet=Q, feature_signature=sig, feature_equivalence=FeatureEquiv,
    facet_axis=ClassAxis, facet_relation_axis=ClassRelAxis, language_facet_count=LanguageClassCount,
    language_facet_presence=LanguageClassPresence, facet_coverage_lower=ClassCovLo, facet_coverage_upper=ClassCovHi,
    structural_node_facet_seed=ExistingNodeClass, facet_structural_node_seed=ClassExistingNode,
    axis_layer=AxisLayer, facet_layer=ClassLayer, structural_node_layer=NodeLayer,
    relation_axis_concrete_relation=RA_CR, semantic_axis_concrete_relation=SA_CR,
    facet_concrete_relation=ClassConcreteRel, structural_node_concrete_relation=NodeConcreteRel,
    semantic_axis_field=SA_Field, concrete_relation_field=CR_Field, facet_field=ClassField, structural_node_field=NodeField,
    facet_coupling=ClassCoupling, facet_semantic_overlap=ClassSemanticOverlap,
    facet_relation_overlap=ClassRelationOverlap, facet_language_overlap=ClassLanguageOverlap,
    facet_layer_component=ClassComponent)

# Calculator copied as self-contained reproducibility reference
calc = r'''@echo off
setlocal
where py >nul 2>nul
if errorlevel 1 (
  echo Python launcher 'py' was not found.
  exit /b 1
)
py -3 calculate_uast_schema.py
pause
'''
(OUT/'RUN_UAST_SCHEMA_CALC.bat').write_text(calc,encoding='utf-8')

# Recalculation script (compact: validates exact quotient and core formulas against files)
pycalc = r'''from pathlib import Path
import csv, json, numpy as np
ROOT=Path(__file__).resolve().parent

def m(name):
    with (ROOT/name).open(encoding='utf-8-sig',newline='') as f: rows=list(csv.reader(f))
    return rows[0][1:], [r[0] for r in rows[1:]], np.array([[float(x or 0) for x in r[1:]] for r in rows[1:]])
features,_,Q=m('02_feature_to_semantic_facet_matrix.csv')
_,_,sig=m('03_feature_schema_signature_matrix.csv')
_,_,A=m('05_semantic_facet_axis_matrix.csv')
_,_,R=m('06_semantic_facet_relation_axis_matrix.csv')
_,_,LP=m('08_language_semantic_facet_presence_matrix.csv')
assert np.allclose(Q.sum(axis=1),1), 'feature quotient is not total/function-like'
assert len({tuple(row) for row in sig.astype(int)})==Q.shape[1], 'facet count differs from exact signature quotient'
assert A.shape[0]==Q.shape[1] and R.shape[0]==Q.shape[1]
assert LP.shape[1]==Q.shape[1]
print('Schema algebra valid')
print('features=',Q.shape[0],'facets=',Q.shape[1],'semantic_axes=',A.shape[1],'relation_axes=',R.shape[1])
'''
(OUT/'calculate_uast_schema.py').write_text(pycalc,encoding='utf-8')

# README and Codex contract
readme=f'''# Universal AST Schema Matrix v1 — algebraic construction

This package is derived from the 8-language Universal Semantic Matrix (Python, R, Rust, C/C++, Kotlin, Java, C#, Go).
It contains no priority/ranking calculation.

## Exact construction

Let:
- `P` = Language × Feature presence, shape `{P.shape[0]} × {P.shape[1]}`
- `S` = Feature × SemanticAxis, shape `{S.shape[0]} × {S.shape[1]}`
- `R` = Feature × RelationAxis, shape `{R.shape[0]} × {R.shape[1]}`
- `U` = seeded Feature × StructuralNode projection, shape `{U_seed.shape[0]} × {U_seed.shape[1]}`

Define the schema signature matrix:

`Z = [S | R]`

Features are quotient-equivalent iff their rows of `Z` are exactly equal. The quotient projection `Q` is Feature × SemanticFacet.
This produces exactly **{len(classes)} semantic facet classes** (`UASF_0001...`) from {len(features)} features.

Then:
- `FacetAxis = bool(Q.T @ S)`
- `FacetRelationAxis = bool(Q.T @ R)`
- `LanguageFacet = bool(P @ Q)`
- `StructuralNodeFacetSeed = U.T @ Q`
- `FacetLayer = bool(FacetAxis @ AxisLayer)`
- `FacetConcreteRelation = bool(FacetRelationAxis @ RelationAxisConcreteRelation + FacetAxis @ SemanticAxisConcreteRelation)`
- `FacetField = bool(FacetAxis @ SemanticAxisField + FacetConcreteRelation @ ConcreteRelationField)`

No nearest-neighbor assignment is used. No language-specific feature is guessed onto an unrelated structural node.

## Concrete schema model

The universal AST is hybrid:
1. **Structural kind space:** {len(structural_nodes)} existing structural node kinds.
2. **Semantic facet space:** {len(classes)} exact quotient classes derived from language semantics.
3. **Semantic axis space:** {len(axes)} axes.
4. **Concrete relation space:** {len(concrete_relations)} relation kinds.
5. **Field space:** {len(fields)} schema fields.
6. **Layer space:** {len(layers)} orthogonal schema layers.

A node instance therefore has one structural kind and a sparse semantic-facet vector instead of requiring every source-language semantic distinction to become a different syntax-node struct.

## Why this matters

Only {int((U_seed.sum(axis=1)>0).sum())} of {len(features)} features had direct structural-node seed mappings. The quotient/facet construction keeps all {len(features)} features without inventing mappings for the remaining {len(features)-int((U_seed.sum(axis=1)>0).sum())}.

## Codex rule

Do not iterate feature rows as a TODO list. Implement the matrix schema and transformations. SemanticProgram remains the executable semantic representation; this package specifies how to generalize it into a universal structural-node + semantic-facet + relation model.
'''
(OUT/'00_README_UAST_SCHEMA_MATRIX.md').write_text(readme,encoding='utf-8')

codex=f'''# Codex implementation contract — Universal AST Schema Matrix v1

Implement the schema algebra, not a priority list.

## Required representation

Preserve these independent dimensions in code/data:
- StructuralNodeKind: {len(structural_nodes)} labels from `36_structural_node_kind_catalog.csv`.
- SemanticFacet: {len(classes)} quotient classes from `01_semantic_facet_catalog.csv`.
- SemanticAxis: {len(axes)} axes.
- ConcreteRelationKind: {len(concrete_relations)} relations.
- UniversalField: {len(fields)} fields.
- SchemaLayer: {len(layers)} layers.

Do **not** create one Go struct per language feature. A node should carry a structural kind plus sparse facets/relations/evidence.

## Matrix invariants

1. `Q.sum(axis=1) == 1` for Feature→SemanticFacet.
2. A semantic facet is exactly one equivalence class of identical `[SemanticAxis | RelationAxis]` rows.
3. Existing structural node mappings are seeds only (`11`/`12` matrices); do not infer missing source-language nodes by nearest-neighbor guesses.
4. Field applicability comes from `25_semantic_facet_field_matrix.csv` and `26_structural_node_field_matrix.csv`.
5. Relation applicability comes from `20_semantic_facet_concrete_relation_matrix.csv` and `21_structural_node_concrete_relation_matrix.csv`.
6. Preserve SemanticProgram coverage as lower/upper interval matrices; do not replace unknown/partial with a fabricated scalar.
7. Do not use priorities, weights, rankings, or sorted implementation order.

## Suggested repository adaptation

Generalize current SemanticDocument/SemanticEvidence so the serialized program can carry:
- structural kind id
- sparse semantic facet ids
- sparse relation instances
- field mask / typed properties
- source spans
- existing type/effect/control/data/binding/evaluation evidence
- dialect/extensions without silently dropping unsupported semantics

`33_current_semanticprogram_to_uast_field_crosswalk.csv` gives the compatibility bridge from current SemanticProgram fields.

Acceptance is algebraic: serialization round-trip must preserve structural kind, semantic-facet vector, concrete relations, and applicable field data exactly.
'''
(OUT/'CODEX_IMPLEMENTATION_CONTRACT.md').write_text(codex,encoding='utf-8')

# Include the algebra source package, but not nested language handoffs twice: add original v2 ZIP as one input artifact.
input_dir=OUT/'input'
input_dir.mkdir()
shutil.copy2('/mnt/data/universal_semantic_matrix_algebra_v2_with_go.zip', input_dir/'universal_semantic_matrix_algebra_v2_with_go.zip')

# manifest
manifest=[]
for p in sorted(OUT.rglob('*')):
    if p.is_file(): manifest.append([str(p.relative_to(OUT)),p.stat().st_size])
write_rows(OUT/'99_manifest.csv',['path','size_bytes'],manifest)

# sanity checks
assert Q.shape==(553,len(classes))
assert np.all(Q.sum(axis=1)==1)
assert len(classes)==len(groups)
assert ClassAxis.shape==(len(classes),44)
assert ClassRelAxis.shape==(len(classes),23)
assert ClassConcreteRel.shape[0]==len(classes)
assert ClassField.shape==(len(classes),len(fields))

# zip
if ZIP.exists(): ZIP.unlink()
with zipfile.ZipFile(ZIP,'w',zipfile.ZIP_DEFLATED,compresslevel=9) as z:
    for p in sorted(OUT.rglob('*')):
        if p.is_file(): z.write(p,p.relative_to(OUT))
print(json.dumps({
 'zip':str(ZIP),'out':str(OUT),'languages':len(langs),'features':len(features),'semantic_facets':len(classes),
 'structural_nodes':len(structural_nodes),'semantic_axes':len(axes),'relation_axes':len(rel_axes),
 'concrete_relations':len(concrete_relations),'fields':len(fields),'layers':len(layers),
 'layer_components':len(component_cols),'seed_mapped_features':int((U_seed.sum(axis=1)>0).sum()),
 'zip_bytes':ZIP.stat().st_size
},indent=2))
