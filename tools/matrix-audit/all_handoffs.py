"""Joint, locally calculated demand from Python, Go and R handoff archives."""
import argparse
import hashlib
import io
import math
from pathlib import Path
import time
import zipfile

from python_handoff import rows, matvec, save, table, recompute


def project(vector, records, key):
    columns = list(records[0])[1:]
    if len({r[key] for r in records}) != len(records):
        raise ValueError("duplicate projection rows")
    missing = set(vector) - {r[key] for r in records}
    result = [math.fsum(vector.get(r[key], 0) * float(r[c]) for r in records) for c in columns]
    return columns, result, sorted(missing)


def normalize(values):
    if any(not math.isfinite(v) or v < 0 for v in values.values()):
        raise ValueError("invalid evidence vector")
    total = math.fsum(values.values())
    if not total:
        raise ValueError("empty evidence vector")
    return {k: v / total for k, v in values.items()}


def numeric_projection(records, key):
    columns=[]
    for column in records[0]:
        if column==key: continue
        try:
            for record in records: float(record[column])
        except (TypeError,ValueError):
            continue
        columns.append(column)
    return [{key:record[key],**{column:record[column] for column in columns}} for record in records]


def recompute_dialect(archive, out, language):
    generic = rows(archive, "05_generic_feature_gap_vector.csv")
    specific = rows(archive, f"10_{language}_specific_gap_vector.csv")
    if language=="r": importance,coverage,specific_importance,specific_coverage,specific_key="importance","coverage","source_importance","current_sp_coverage","r_semantic_feature"
    elif language=="rust": importance,coverage,specific_importance,specific_coverage,specific_key="normalized_importance","coverage_score","normalized_importance","coverage_score","rust_semantic_feature"
    elif language in ("kotlin","java"): importance,coverage,specific_importance,specific_coverage,specific_key="normalized_importance","coverage_score","normalized_importance","coverage_score","feature"
    elif language=="csharp": importance,coverage,specific_importance,specific_coverage,specific_key="importance","coverage","importance","coverage","feature"
    elif language=="clang_cpp": importance,coverage,specific_importance,specific_coverage,specific_key="importance","mean_sp_coverage","importance","mean_sp_coverage","feature"
    else: raise ValueError(f"unknown dialect configuration {language}")
    generic_names = {r["feature"] for r in generic}
    base = {r["feature"]: float(r[importance]) * (1-float(r[coverage])) for r in generic}
    for record in specific:
        name=record[specific_key]
        if name in base: raise ValueError(f"{language} specific feature collides with generic basis: {name}")
        base[name]=float(record[specific_importance])*(1-float(record[specific_coverage]))
    op = rows(archive, "13_gap_propagation_operator.csv")
    features = list(op[0])[1:]
    key = next(iter(op[0]))
    by = {r[key]: r for r in op}
    # Some handoffs namespace the combined operator basis (for example
    # GEN::assignment and CS::nullable_flow_analysis).  Derive that alignment
    # from the generic/specific partitions instead of copying or editing cells.
    if set(features) != set(base):
        namespace = {
            name: (("GEN::" if name in generic_names else "CS::") + name)
            for name in base
        } if language == "csharp" else {}
        if namespace and set(namespace.values()) == set(features) and set(by) == set(features):
            base = {namespace[name]: value for name, value in base.items()}
        else:
            raise ValueError(f"{language} operator feature basis mismatch")
    if set(features) != set(base) or set(by) != set(base):
        raise ValueError(f"{language} operator feature basis mismatch")
    matrix=[[float(by[f][g]) for g in features] for f in features]
    vector=[base[f] for f in features]
    expected_rows=rows(archive,"15_matrix_derived_clusters.csv")
    cluster_key="cluster_family" if "cluster_family" in expected_rows[0] else "cluster"
    score_key=next((key for key in ("matrix_gap_score","gap_score","propagated_gap") if key in expected_rows[0]),None)
    if score_key is None: raise ValueError(f"{language} cluster score column missing")
    expected={r[cluster_key]:float(r[score_key]) for r in expected_rows}
    candidates=[]
    for orientation,product in [("P*g",matvec(matrix,vector)),("P^T*g",matvec([list(row) for row in zip(*matrix)],vector))]:
        propagated=dict(zip(features,product))
        names,scores,missing=project(propagated,numeric_projection(rows(archive,"11_combined_feature_family_projection_matrix.csv"),"feature"),"feature")
        if missing: raise ValueError(f"{language} family projection incomplete")
        delta=max(abs(score-expected[name]) for name,score in zip(names,scores))
        candidates.append((delta,orientation,propagated,names,scores))
    delta,orientation,propagated,names,scores=min(candidates,key=lambda x:x[0])
    if delta > .002:
        raise ValueError(f"{language} cluster reproduction failed: {delta}")
    table(out / f"{language}-clusters.csv", ["cluster","gap"], sorted(zip(names,scores),key=lambda p:-p[1]))
    table(out / f"{language}-feature-gaps.csv", ["feature","base_gap","propagated_gap"], [(f,base[f],propagated[f]) for f in features])
    return specific, {"rounding_max_error":delta,"operator_orientation":orientation,"top_cluster":names[max(range(len(names)),key=lambda i:scores[i])]}


def calculate(go_path, python_path, r_path, rust_path, clang_path, kotlin_path, java_path, csharp_path, out):
    with zipfile.ZipFile(go_path) as go, zipfile.ZipFile(python_path) as py, zipfile.ZipFile(r_path) as r, zipfile.ZipFile(rust_path) as rust, zipfile.ZipFile(clang_path) as clang, zipfile.ZipFile(kotlin_path) as kotlin, zipfile.ZipFile(java_path) as java, zipfile.ZipFile(csharp_path) as csharp:
        pydir = out / "python"
        pydir.mkdir()
        pycalculation, _ = recompute(py,pydir)
        specific, rcalculation = recompute_dialect(r,out,"r")
        rust_specific, rustcalculation = recompute_dialect(rust,out,"rust")
        clang_specific, clangcalculation = recompute_dialect(clang,out,"clang_cpp")
        kotlin_specific, kotlincalculation = recompute_dialect(kotlin,out,"kotlin")
        java_specific, javacalculation = recompute_dialect(java,out,"java")
        csharp_specific, csharpcalculation = recompute_dialect(csharp,out,"csharp")
        u = rows(go,"08_feature_uast_node_matrix.csv")
        relations = rows(go,"09_feature_relation_matrix.csv")
        go_master = rows(go,"evidence_go_language_matrix/20_language_feature_master.csv")
        raw = {"go":{v["feature"]:float(v["evidence_count"]) for v in go_master}}
        axes = {"go":rows(go,"evidence_go_language_matrix/05_feature_sp_projection_matrix.csv")}
        for lang, archive, nested in [("python",py,"90_original_language_matrix_python.zip"),("r",r,"90_original_language_matrix_r.zip"),("rust",rust,"90_original_language_matrix_rust.zip"),("clang_cpp",clang,"90_original_language_matrix_misdetected_swift.zip"),("kotlin",kotlin,"90_original_language_matrix_kotlin.zip"),("java",java,"90_original_language_matrix_openjdk.zip"),("csharp",csharp,"90_original_language_matrix_csharp.zip")]:
            with zipfile.ZipFile(io.BytesIO(archive.read(nested))) as original:
                raw[lang]={v["feature"]:float(v["evidence_count"]) for v in rows(original,"20_language_feature_master.csv")}
                axes[lang]=rows(original,"05_feature_sp_projection_matrix.csv")
        # Reproduce Go's original unnormalized matrix product before joining.
        go_nodes, go_scores, missing = project(raw["go"],u,"go_feature")
        expected = {v["uast_node_kind"]:float(v["go_source_evidence_weight"]) for v in rows(go,"10_uast_weight_vector.csv")}
        go_delta = max(abs(score-expected[name]) for name,score in zip(go_nodes,go_scores))
        if missing or go_delta > 1e-6:
            raise ValueError(f"Go projection mismatch: {go_delta}, {missing}")
        languages = ["go","python","r","rust","clang_cpp","kotlin","java","csharp"]
        features = sorted(set().union(*(set(raw[l]) for l in languages)))
        if any(set(raw[l]) != set(features) for l in languages):
            raise ValueError("generic feature bases differ; explicit alignment required")
        normalized = {l:normalize(raw[l]) for l in languages}
        table(out/"language-feature.csv",["language"]+features,[[l]+[normalized[l][f] for f in features] for l in languages])
        demand = {}
        for label,projection,key in [("nodes",u,"go_feature"),("relations",relations,"go_feature")]:
            data=[]
            for l in languages:
                columns,values,missing=project(normalized[l],projection,key)
                if missing: raise ValueError(f"missing {label} projection: {missing}")
                data.append(values)
            table(out/f"language-{label}.csv",["language"]+columns,[[l]+v for l,v in zip(languages,data)])
            totals=[math.fsum(row[i] for row in data) for i in range(len(columns))]
            ranked=sorted(zip(columns,totals),key=lambda p:(-p[1],p[0]))
            table(out/f"joint-{label}-demand.csv",[label,"equal_language_demand"],ranked)
            demand[label]=ranked
        # Keep R's irreducible features separate, never zero-fill their UAST map.
        rvector=normalize({v["r_semantic_feature"]:float(v["source_importance"]) for v in specific})
        names,values,missing=project(rvector,rows(r,"08_r_specific_semanticprogram_projection_matrix.csv"),"r_semantic_feature")
        if missing:raise ValueError("R dialect axes missing")
        table(out/"r-dialect-axis-demand.csv",["axis","demand"],sorted(zip(names,values),key=lambda p:-p[1]))
        table(out/"unmapped-dialect-uast.csv",["feature","reason"],[(f,"No universal-node projection supplied; retain R dialect requirement") for f in sorted(rvector)])
        rustvector=normalize({v["rust_semantic_feature"]:float(v["normalized_importance"]) for v in rust_specific})
        names,values,missing=project(rustvector,rows(rust,"08_rust_specific_semanticprogram_projection_matrix.csv"),"rust_semantic_feature")
        if missing:raise ValueError("Rust dialect axes missing")
        table(out/"rust-dialect-axis-demand.csv",["axis","demand"],sorted(zip(names,values),key=lambda p:-p[1]))
        clangvector=normalize({v["feature"]:float(v["importance"]) for v in clang_specific})
        kotlinvector=normalize({v["feature"]:float(v["normalized_importance"]) for v in kotlin_specific})
        javavector=normalize({v["feature"]:float(v["normalized_importance"]) for v in java_specific})
        csharpvector=normalize({v["feature"]:float(v["importance"]) for v in csharp_specific})
        dialect_vectors={"r":rvector,"rust":rustvector,"clang_cpp":clangvector,"kotlin":kotlinvector,"java":javavector,"csharp":csharpvector}
        # Preserve every supplied dialect feature in one named matrix. Namespaces
        # make equal spellings independent and the observation vector distinguishes
        # an unavailable profile from a measured zero.
        dialect_features=sorted(f"{language}::{feature}" for language,vector in dialect_vectors.items() for feature in vector)
        dialect_rows=[]
        for language in languages:
            vector=dialect_vectors.get(language,{})
            dialect_rows.append([language]+[vector.get(feature.split("::",1)[1],0) if feature.startswith(language+"::") else 0 for feature in dialect_features])
        table(out/"language-dialect-feature.csv",["language"]+dialect_features,dialect_rows)
        table(out/"language-dialect-observed.csv",["language","observed"],[(language,1 if language in dialect_vectors else 0) for language in languages])
        for language,archive,vector,key in [("clang_cpp",clang,clangvector,"feature"),("kotlin",kotlin,kotlinvector,"feature"),("java",java,javavector,"feature"),("csharp",csharp,csharpvector,"feature")]:
            projection=numeric_projection(rows(archive,f"08_{language}_specific_semanticprogram_projection_matrix.csv"),key)
            names,values,missing=project(vector,projection,key)
            if missing:raise ValueError(f"{language} dialect axes missing")
            table(out/f"{language}-dialect-axis-demand.csv",["axis","demand"],sorted(zip(names,values),key=lambda p:-p[1]))
        table(out/"unmapped-dialect-uast.csv",["language","feature","reason"],[(language,f,"No supplied universal-node projection; mapping remains open") for language,vector in dialect_vectors.items() for f in sorted(vector)])
        # Generate a complete task inventory from all cluster tables, not hand-entered cells.
        tasks=[]
        for language, archive, filename in [
            ("go",go,"07_matrix_implementation_clusters.csv"),
            ("python",py,"14_codex_cluster_implementation_matrix.csv"),
            ("r",r,"16_codex_cluster_implementation_matrix.csv"),
            ("rust",rust,"16_codex_cluster_implementation_matrix.csv"),
            ("clang_cpp",clang,"16_codex_cluster_implementation_matrix.csv"),
            ("kotlin",kotlin,"16_codex_cluster_implementation_matrix.csv"),
            ("java",java,"16_codex_cluster_implementation_matrix.csv"),
            ("csharp",csharp,"16_codex_cluster_implementation_matrix.csv")]:
            for record in rows(archive,filename):
                tasks.append({"language":language,"source":filename,"instruction":record,"status":"not_verified_complete"})
        report={"schema":"code-transpiler.joint-handoff.v2","languages":languages,"feature_count":len(features),
                "node_count":len(demand["nodes"]),"relation_count":len(demand["relations"]),
                "dialect_feature_counts":{language:len(vector) for language,vector in dialect_vectors.items()},"top_nodes":demand["nodes"][:10],"top_relations":demand["relations"][:10],
                "reproduction":{"go_max_error":go_delta,"python_max_error":pycalculation["rounding_max_error"],"r":rcalculation,"rust":rustcalculation,"clang_cpp":clangcalculation,"kotlin":kotlincalculation,"java":javacalculation,"csharp":csharpcalculation},
                "tasks":tasks,"archive_sha256":{l:hashlib.sha256(p.read_bytes()).hexdigest() for l,p in [("go",go_path),("python",python_path),("r",r_path),("rust",rust_path),("clang_cpp",clang_path),("kotlin",kotlin_path),("java",java_path),("csharp",csharp_path)]},
                "limits":["Equal language weights after L1 normalization; demand is not verified coverage or a completion score.",
                          "The shared UAST/relation projection comes from the Go handoff; reuse for other languages is a planning hypothesis.",
                          "Generic evidence uses imported collector counts, not a fresh source scan.",
                          "Language-specific features retain separate dialect demand; no fabricated universal mapping.",
                          "Baseline gap estimates remain unchanged; tasks are complete only with implementation evidence."]}
        save(out/"report.json",report)
        (out/"SUMMARY.md").write_text("# Joint handoff calculation\n\n"
            f"{len(languages)} languages x {len(features)} shared features -> {len(demand['nodes'])} node kinds and {len(demand['relations'])} relations.\n\n"
            f"Additional dialect requirements: R {len(rvector)}, Rust {len(rustvector)}, Clang/C++ {len(clangvector)}, Kotlin {len(kotlinvector)}, Java {len(javavector)}, C# {len(csharpvector)}. Imported task clusters: {len(tasks)}.\n\n"
            +"Highest node demands:\n\n"+"\n".join(f"- {name}: {value:.6f}" for name,value in demand["nodes"][:8])
            +"\n\nThese are recomputed demand rankings, not implemented features. See report.json for reproducibility checks and evidence limits.\n",encoding="utf-8")
        return report


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument("--go",type=Path,required=True)
    p.add_argument("--python",type=Path,required=True)
    p.add_argument("--r",type=Path,required=True)
    p.add_argument("--rust",type=Path,required=True)
    p.add_argument("--clang-cpp",dest="clang_cpp",type=Path,required=True)
    p.add_argument("--kotlin",type=Path,required=True)
    p.add_argument("--java",type=Path,required=True)
    p.add_argument("--csharp",type=Path,required=True)
    p.add_argument("--out",type=Path,default=Path("outputs/all-handoffs")/time.strftime("%Y%m%d-%H%M%S"))
    a=p.parse_args();a.out.mkdir(parents=True,exist_ok=False)
    report=calculate(a.go,a.python,a.r,a.rust,a.clang_cpp,a.kotlin,a.java,a.csharp,a.out)
    print((a.out/"SUMMARY.md").read_text(encoding="utf-8"))
    print("Report:",a.out.resolve())

if __name__=="__main__":main()
