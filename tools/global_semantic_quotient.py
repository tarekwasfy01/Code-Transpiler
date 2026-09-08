#!/usr/bin/env python3
"""Build the global semantic quotient from structured local evidence.

This tool only derives matrices and proof records.  It never promotes a
name, opcode, diagnostic, or runtime helper to a canonical primitive without
an existing structured contract match.
"""
from __future__ import annotations
import csv, json, re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
EVIDENCE = ROOT / "outputs/.semantic-round15-input"
OUT = ROOT / "outputs/global-semantic-quotient-round15"

def rows(path):
    with path.open(encoding="utf-8-sig", newline="") as f:
        return list(csv.DictReader(f))

def write(name, fields, data):
    with (OUT/name).open("w", encoding="utf-8", newline="") as f:
        w=csv.DictWriter(f, fieldnames=fields); w.writeheader(); w.writerows(data)

def main():
    OUT.mkdir(parents=True, exist_ok=True)
    truths=rows(EVIDENCE/"global-semantic-truth-master-round1-14.csv")
    sources=rows(EVIDENCE/"global-semantic-source-master-round1-14.csv")
    gt=rows(ROOT/"outputs/.handoff-ground-truth-round3/ground-truth-primitive-candidates-round3.csv")
    upi=rows(ROOT/"matrices/UPI_DIRECT_IMPLEMENTATION_MATRIX.csv")
    specs=rows(ROOT/"internal/backend/semantic_primitive_specs.csv")
    primitive_ids={r.get("id","").strip().upper() for r in specs if r.get("id")}
    gt_names={r.get("candidate_family","").strip().upper() for r in gt}
    upi_by={r.get("construct","").strip().upper():r for r in upi}

    # Explicitly derived, parameterized axes from the current handoff.
    axes={
      "observation_domain":("effect/observation","program_value|control|external_io|volatile_io|machine_state|persistence|timing|security_side_channel|gc_lifecycle|validation","effects,contracts"),
      "commit_boundary":("control/transaction","pending|staged|committed|rolled_back","control,visibility"),
      "rollback_scope":("control/state","local|transaction|task|machine|persistent","restore_point,state_domains"),
      "snapshot_view":("type/relation","live|stable_version|historical","version,read_from"),
      "version_identity":("type/relation","generation|epoch|world_age|schema_version|logical_timestamp","identity,version"),
      "stability_kind":("facet","value|address|identity|version|ordering|representation","lifetime,ownership"),
      "loss_policy":("contract","preserve|drop|truncate|overwrite|error","failure,representation"),
      "equivalence_domain":("relation","value|identity|representation|canonical|observational|schema","comparison,relations"),
      "overflow_mode":("numeric contract","wrap|saturate|checked|trap|undefined","arithmetic,failure"),
      "division_mode":("numeric contract","truncate|floor|ceil|euclidean","arithmetic,result"),
      "remainder_mode":("numeric contract","dividend|divisor|euclidean","arithmetic,result"),
    }
    write("00_source_inventory.csv",["source","rows","path"],[
      {"source":"external_truth","rows":len(truths),"path":str(EVIDENCE/"global-semantic-truth-master-round1-14.csv")},
      {"source":"external_sources","rows":len(sources),"path":str(EVIDENCE/"global-semantic-source-master-round1-14.csv")},
      {"source":"ground_truth_candidates","rows":len(gt),"path":"outputs/.handoff-ground-truth-round3/ground-truth-primitive-candidates-round3.csv"},
      {"source":"current_primitive_specs","rows":len(specs),"path":"internal/backend/semantic_primitive_specs.csv"},
    ])

    projection=[]; classifications={}
    for t in truths:
        name=t.get("canonical_truth","").strip(); key=name.upper()
        cls=t.get("classification","").strip().upper(); overlap=t.get("ground_truth_overlap","")
        existing = key if key in primitive_ids else ""
        if existing: final="EXISTING_EXACT"
        elif "PARAMETER" in cls or "FACET" in cls or "CONTRACT" in cls: final="EXISTING_WITH_PARAMETERIZATION"
        elif "RELATION" in cls: final="EXISTING_WITH_NEW_RELATION"
        elif "TYPE" in cls or "VALUE" in cls or "STATE" in cls: final="EXISTING_WITH_NEW_TYPE"
        elif "CONTROL" in cls: final="NEW_CONTROL_PRIMITIVE" if "CANDIDATE" in cls else "DERIVED_OPERATION"
        elif "ATOMIC" in cls: final="NEW_ATOMIC_PRIMITIVE" if key not in gt_names else "EXISTING_WITH_NEW_CONTRACT"
        elif "TRANSFORM" in cls: final="TRANSFORM_ONLY"
        elif "RUNTIME" in cls or "MACHINE" in cls: final=cls
        elif "VALIDATION" in cls: final="VALIDATION_ONLY"
        elif "TOOLING" in cls: final="TOOLING_ONLY"
        else: final="UNRESOLVED"
        classifications[final]=classifications.get(final,0)+1
        projection.append({"canonical_truth":name,"semantic_domain":t.get("domain",""),"semantic_contract":t.get("semantic_contract",""),"ground_truth_match":overlap,"existing_primitive_match":existing,"canonical_relation_match":"","canonical_type_match":"","parameter_axes":";".join(a for a in axes if a.lower() in t.get("semantic_contract","").lower()),"derived_from":"external evidence","existing_executor":"","native_support":"","source_selfhosting_witness":"","binary_witness":"","external_evidence_count":t.get("independent_evidence_count",""),"final_classification":final,"new_atomic":str(final=="NEW_ATOMIC_PRIMITIVE").lower(),"new_control":str(final=="NEW_CONTROL_PRIMITIVE").lower(),"new_relation":str(final=="EXISTING_WITH_NEW_RELATION").lower(),"new_facet":str(final=="EXISTING_WITH_NEW_PARAMETERIZATION").lower(),"new_field":"false","new_type":str(final=="EXISTING_WITH_NEW_TYPE").lower(),"new_contract":str(final=="EXISTING_WITH_NEW_CONTRACT").lower(),"runtime_only":str(final=="RUNTIME_ONLY").lower(),"validation_only":str(final=="VALIDATION_ONLY").lower(),"tooling_only":str(final=="TOOLING_ONLY").lower(),"structural_schema_gap":"false","unresolved":str(final=="UNRESOLVED").lower(),"proof":"structured external truth + explicit classification; no name-only promotion"})
    fields=list(projection[0])
    write("01_ground_truth_projection.csv",fields,projection)

    # Current primitive exact quotient: identical contracts are represented by
    # the current spec, preserving all IDs for compatibility.
    q=[]
    for i,s in enumerate(specs):
        ident=re.sub(r"[^A-Z0-9]+","_",(s.get("rewrite","") or s.get("class","")).upper()).strip("_")
        q.append({"primitive_id":s.get("id",""),"current_name":s.get("id",""),"semantic_family":s.get("class",""),"semantic_contract":s.get("rewrite",""),"input_roles":"","output_roles":"","state_effect":"","control_effect":"","memory_effect":"","failure_contract":"","parameter_axes":"","alias_group":ident,"derived_from":"semantic_primitive_specs.csv","canonical":"true","redundant_candidate":"false","evidence":"current product registry"})
    write("02_current_primitive_semantic_quotient.csv",list(q[0]),q)

    # Contract matrices are sparse, explicit evidence joins rather than
    # inferred semantic execution claims.
    write("03_external_truth_to_ground_truth_matrix.csv",["truth","ground_truth_class","match","evidence"],[{"truth":t.get("canonical_truth",""),"ground_truth_class":t.get("ground_truth_overlap",""),"match":str(bool(t.get("ground_truth_overlap"))).lower(),"evidence":t.get("evidence_systems","")} for t in truths])
    write("04_external_truth_to_primitive_matrix.csv",["truth","primitive_id","classification","match"],[{"truth":p["canonical_truth"],"primitive_id":p["existing_primitive_match"],"classification":p["final_classification"],"match":str(bool(p["existing_primitive_match"])).lower()} for p in projection])
    write("05_external_truth_to_uast_matrix.csv",["truth","uast_structure","relation","field","status"],[{"truth":p["canonical_truth"],"uast_structure":"","relation":"","field":"","status":"UNRESOLVED" if p["unresolved"]=="true" else "STRUCTURED_CONTRACT_REVIEW"} for p in projection])
    write("06_semantic_parameter_axis_matrix.csv",["axis_id","semantic_domain","allowed_values","applies_to","existing_schema_support","existing_primitive_support","required_new_field/facet","orthogonality_evidence","combination_constraints"],[{"axis_id":k,"semantic_domain":v[0],"allowed_values":v[1],"applies_to":"parameterized semantic contracts","existing_schema_support":"review existing UAST fields/facets","existing_primitive_support":"parameterization","required_new_field/facet":"","orthogonality_evidence":"independent contract axis from handoff","combination_constraints":""} for k,v in sorted(axes.items())])
    write("07_semantic_relation_closure_global.csv",["relation","existing","evidence","status"],[{"relation":r,"existing":"unknown","evidence":"ground truth relation closure","status":"REVIEW"} for r in ["syntax.child","control.next","control.loop_back","evaluation.before","data.operand","data.def_use","happens_before","synchronizes_with","read_from","coherence_order","borrows_from","outlives","allocated_in","provenance_of","causal_before","depends_on","waits_for","wakes","parent_task","argument_binding","captures","dispatches_to","schema_alias","live_binding","definition_generation","persists_before","durable_at","snapshot_of","canonical_equivalent","representation_of","value_of"]])
    write("08_semantic_type_value_closure_global.csv",["type_or_value","domain","existing_support","status","proof"],[{"type_or_value":x,"domain":"semantic type/value","existing_support":"review canonical UAST","status":"REVIEW","proof":"structured evidence only"} for x in ["missing","undefined","poison","promise","future","continuation","capability","effect_token","ownership","region","stream","transaction","logical_timestamp","clock_domain","duration","interval","pointer_metadata","security_label","resource_handle","text_view"]])
    write("09_contract_closure_matrix.csv",["truth","required_contract","existing_contract","closure_status"],[{"truth":p["canonical_truth"],"required_contract":p["semantic_contract"],"existing_contract":"","closure_status":"CANDIDATE"} for p in projection])
    write("10_derived_operation_matrix.csv",["truth","derived_from","formula","status"],[])
    write("11_runtime_machine_plane_matrix.csv",["truth","plane","source_evidence","promotion"],[{"truth":p["canonical_truth"],"plane":"runtime/machine","source_evidence":"external semantic truth","promotion":"NO_AUTOMATIC_PROMOTION"} for p in projection if p["final_classification"] in {"RUNTIME_ONLY","MACHINE_ONLY"}])
    write("12_validation_evidence_plane_matrix.csv",["truth","evidence","status"],[{"truth":p["canonical_truth"],"evidence":"external truth corpus","status":"VALIDATION_ONLY"} for p in projection if p["final_classification"]=="VALIDATION_ONLY"])
    write("13_tooling_do_not_model_matrix.csv",["truth","reason","status"],[{"truth":p["canonical_truth"],"reason":"tool/compiler implementation detail","status":"DO_NOT_MODEL"} for p in projection if p["final_classification"] in {"TOOLING_ONLY","MACHINE_ONLY"}])
    write("14_new_atomic_primitive_proof_matrix.csv",["candidate","independent_sources","ground_truth_feature","current_primitive_candidates","decomposition_attempt","candidate_factorization","derivable","requires_state","requires_control","requires_effect","requires_failure_contract","requires_ordering_contract","atomicity_argument","executor_contract","native_lowering_contract","promotion_allowed","proof"],[{"candidate":p["canonical_truth"],"independent_sources":"","ground_truth_feature":p["ground_truth_match"],"current_primitive_candidates":p["existing_primitive_match"],"decomposition_attempt":"existing closure review","candidate_factorization":"","derivable":"unknown","requires_state":"","requires_control":"","requires_effect":"","requires_failure_contract":"","requires_ordering_contract":"","atomicity_argument":"not established","executor_contract":"","native_lowering_contract":"","promotion_allowed":"false","proof":"no automatic promotion without contract proof"} for p in projection if p["final_classification"]=="NEW_ATOMIC_PRIMITIVE"])
    write("15_new_control_primitive_proof_matrix.csv",["candidate","evidence","factorization","promotion_allowed","proof"],[])
    write("16_schema_gap_proof_matrix_global.csv",["truth","semantic_contract","required_value_semantics","required_control_semantics","required_state_transition","required_effects","required_relations","required_fields","required_facets","required_types","existing_candidate_structures","representation_attempt","information_lost","why_parameterization_insufficient","why_relation_insufficient","why_field_insufficient","why_type_insufficient","why_derived_operation_insufficient","structural_gap","proof"],[{"truth":p["canonical_truth"],"semantic_contract":p["semantic_contract"],"required_value_semantics":"","required_control_semantics":"","required_state_transition":"","required_effects":"","required_relations":"","required_fields":"","required_facets":"","required_types":"","existing_candidate_structures":"","representation_attempt":"existing UAST review","information_lost":"unknown","why_parameterization_insufficient":"not established","why_relation_insufficient":"not established","why_field_insufficient":"not established","why_type_insufficient":"not established","why_derived_operation_insufficient":"not established","structural_gap":"false","proof":"no gap without exhaustive structured counterexample"} for p in projection])
    write("17_semantic_executor_coverage_global.csv",["canonical_family","represented_in_uast","primitive_compiler_support","interpreter/executor_support","native_x64_support","PE support","C target support","Go target support","other target support","runtime_service_required","platform_service_required","unsupported_reason","next_required_contract"],[])
    write("18_selfhosting_projection_matrix.csv",["truth","selfhosting_evidence","status"],[])
    write("19_binary_lift_projection_matrix.csv",["truth","binary_evidence","promotion","status"],[])
    write("20_global_semantic_quotient.csv",fields,projection)
    summary={"external_truth_rows":len(truths),"external_unique_truths":len({t.get('canonical_truth') for t in truths}),"ground_truth_candidates":len(gt),"current_primitive_specs":len(specs),"sources":len(sources),"classifications":classifications,"parameter_axes":len(axes),"policy":"structured contract evidence only; no name-only promotion"}
    (OUT/"21_global_semantic_closure_summary.json").write_text(json.dumps(summary,indent=2),encoding="utf-8")
    (OUT/"22_global_semantic_closure_summary.md").write_text("# Global semantic quotient round 15\n\n"+json.dumps(summary,indent=2)+"\n",encoding="utf-8")
    print(json.dumps(summary,sort_keys=True))

if __name__ == "__main__": main()
