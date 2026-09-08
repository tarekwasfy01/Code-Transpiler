"""Validate the handoff algebra without rankings and build the embedded UAST basis."""
import csv, hashlib, json
from pathlib import Path

ROOT=Path("matrices/handoffs/uast_schema")
OUT=Path("internal/backend/universal_ast_schema.json")

def matrix(name):
    with (ROOT/name).open(encoding="utf-8-sig",newline="") as handle: rows=list(csv.reader(handle))
    columns=rows[0][1:]; labels=[]; entries=[]
    for r,row in enumerate(rows[1:]):
        labels.append(row[0])
        for c,value in enumerate(row[1:]):
            number=float(value or 0)
            if number: entries.append([r,c,number])
    return labels,columns,{"rows":len(labels),"cols":len(columns),"storage":"coo","entries":entries}

def row_sets(m):
    out=[set() for _ in range(m["rows"])]
    for r,c,v in m["entries"]:
        if v: out[r].add(c)
    return out

def boolean_product(left,right):
    rr=row_sets(right); out=[set() for _ in range(left["rows"])]
    for r,k,v in left["entries"]:
        if v: out[r].update(rr[k])
    return out

def expect(name,got,want):
    if got != want: raise ValueError(f"{name} algebra mismatch")

def base_field_indexes(fields):
    with (ROOT/"22_universal_field_catalog.csv").open(encoding="utf-8-sig",newline="") as handle:
        records=list(csv.DictReader(handle))
    domain={record["field"]:record["base_or_semantic"] for record in records}
    return {i for i,name in enumerate(fields) if domain[name]=="base"}

def global_relations():
    with (ROOT/"17_concrete_relation_catalog.csv").open(encoding="utf-8-sig",newline="") as handle:
        return [record["relation"] for record in csv.DictReader(handle) if record["applies_to"]=="all nodes"]

def crosswalk_fields():
    fields=[]
    for path in (ROOT/"33_current_semanticprogram_to_uast_field_crosswalk.csv",Path("matrices/schema_extensions/semanticprogram_uast_crosswalk_v2.csv")):
        with path.open(encoding="utf-8-sig",newline="") as handle:
            fields.extend(record["universal_field"] for record in csv.DictReader(handle))
    return list(dict.fromkeys(fields))

def main():
    features,facets,q=matrix("02_feature_to_semantic_facet_matrix.csv")
    sig_features,signature_axes,signature=matrix("03_feature_schema_signature_matrix.csv")
    facet_rows,semantic_axes,facet_axis=matrix("05_semantic_facet_axis_matrix.csv")
    _,relation_axes,facet_relation_axis=matrix("06_semantic_facet_relation_axis_matrix.csv")
    languages,language_facets,language_facet=matrix("08_language_semantic_facet_presence_matrix.csv")
    _,_,coverage_lower=matrix("09_semantic_facet_sp_coverage_lower_matrix.csv")
    _,_,coverage_upper=matrix("10_semantic_facet_sp_coverage_upper_matrix.csv")
    seed_kinds,seed_facets,structural_facet_seed=matrix("11_structural_node_semantic_facet_seed_matrix.csv")
    _,layers,facet_layer=matrix("15_semantic_facet_layer_matrix.csv")
    structural_kinds,_,structural_layer=matrix("16_structural_node_layer_matrix.csv")
    _,concrete_relations,facet_concrete=matrix("20_semantic_facet_concrete_relation_matrix.csv")
    _,_,structural_concrete=matrix("21_structural_node_concrete_relation_matrix.csv")
    _,fields,facet_field=matrix("25_semantic_facet_field_matrix.csv")
    _,_,structural_field=matrix("26_structural_node_field_matrix.csv")
    _,axis_layers,axis_layer=matrix("14_semantic_axis_to_layer_matrix.csv")
    _,relation_concrete,relation_axis_concrete=matrix("18_relation_axis_to_concrete_relation_matrix.csv")
    _,semantic_concrete,semantic_axis_concrete=matrix("19_semantic_axis_to_concrete_relation_matrix.csv")
    _,semantic_fields,semantic_axis_field=matrix("23_semantic_axis_to_field_matrix.csv")
    _,concrete_fields,concrete_relation_field=matrix("24_concrete_relation_to_field_matrix.csv")
    if features != sig_features or facets != facet_rows or facets != language_facets: raise ValueError("basis labels disagree")
    if seed_kinds != structural_kinds or seed_facets != facets: raise ValueError("structural facet seed labels disagree")
    if semantic_axes+relation_axes != signature_axes: raise ValueError("signature axes are not S|R")
    if layers != axis_layers or concrete_relations != relation_concrete or concrete_relations != semantic_concrete or fields != semantic_fields or fields != concrete_fields: raise ValueError("projection labels disagree")
    qrows=row_sets(q)
    if any(len(row)!=1 for row in qrows): raise ValueError("Q is not one-hot per feature")
    signatures=[]
    sigrows=row_sets(signature)
    signature_to_facet={}
    for i,row in enumerate(sigrows):
        key=tuple(sorted(row)); facet=next(iter(qrows[i]))
        if key in signature_to_facet and signature_to_facet[key]!=facet: raise ValueError("equal signatures split across facets")
        signature_to_facet[key]=facet;signatures.append(key)
    if len(signature_to_facet)!=len(facets): raise ValueError("facet count differs from exact signature quotient")
    derived_axis=[set() for _ in facets];derived_rel=[set() for _ in facets]
    for i,facetset in enumerate(qrows):
        facet=next(iter(facetset))
        derived_axis[facet].update(c for c in sigrows[i] if c<len(semantic_axes))
        derived_rel[facet].update(c-len(semantic_axes) for c in sigrows[i] if c>=len(semantic_axes))
    expect("facet semantic axis",derived_axis,row_sets(facet_axis));expect("facet relation axis",derived_rel,row_sets(facet_relation_axis))
    expect("facet layer",boolean_product(facet_axis,axis_layer),row_sets(facet_layer))
    relation_from_axes=boolean_product(facet_relation_axis,relation_axis_concrete)
    relation_from_semantics=boolean_product(facet_axis,semantic_axis_concrete)
    combined=[a|b for a,b in zip(relation_from_axes,relation_from_semantics)]
    expect("facet concrete relation",combined,row_sets(facet_concrete))
    field_from_semantics=boolean_product(facet_axis,semantic_axis_field)
    field_from_relations=boolean_product(facet_concrete,concrete_relation_field)
    base_fields=base_field_indexes(fields)
    expect("facet field",[a|b|base_fields for a,b in zip(field_from_semantics,field_from_relations)],row_sets(facet_field))
    basis={"schema":"code-transpiler.universal-ast-basis.v1","features":features,"facets":facets,
           "semantic_axes":semantic_axes,"relation_axes":relation_axes,"languages":languages,
           "structural_kinds":structural_kinds,"concrete_relations":concrete_relations,"fields":fields,"layers":layers,
           "global_relations":global_relations(),
           "crosswalk_fields":crosswalk_fields(),
           "feature_facet":q,"feature_signature":signature,"facet_axis":facet_axis,"facet_relation_axis":facet_relation_axis,
           "language_facet":language_facet,"coverage_lower":coverage_lower,"coverage_upper":coverage_upper,
           "structural_facet_seed":structural_facet_seed,
           "facet_layer":facet_layer,"structural_layer":structural_layer,"facet_concrete_relation":facet_concrete,
           "structural_concrete_relation":structural_concrete,"facet_field":facet_field,"structural_field":structural_field}
    canonical=json.dumps(basis,sort_keys=True,separators=(",",":")).encode()
    payload={"basis_sha256":hashlib.sha256(canonical).hexdigest(),"basis":basis}
    OUT.write_text(json.dumps(payload,sort_keys=True,separators=(",",":"))+"\n",encoding="utf-8")
    print(f"Schema algebra valid: {len(features)} features -> {len(facets)} facets; {len(structural_kinds)} kinds, {len(concrete_relations)} relations, {len(fields)} fields, {len(layers)} layers")
    print("Wrote",OUT.resolve())

if __name__=="__main__": main()
