"""Create deterministic UAST coverage reports from basis, crosswalks, and bridge code."""

import csv
import json
import re
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
SCHEMA = ROOT / "internal/backend/universal_ast_schema.json"
PROJECTION = ROOT / "internal/backend/universal_ast_projection.go"
SEMANTIC_DOCUMENT = ROOT / "internal/backend/semantic_document.go"
UAST_DIRECT = ROOT / "internal/backend/uast_direct.go"
UAST_CAPABILITY = ROOT / "internal/backend/uast_capability.go"
OUT_DIR = ROOT / "outputs/uast-coverage"
CROSSWALKS = (
    ROOT / "matrices/handoffs/uast_schema/33_current_semanticprogram_to_uast_field_crosswalk.csv",
    ROOT / "matrices/schema_extensions/semanticprogram_uast_crosswalk_v2.csv",
)


def function_source(source, name):
    match = re.search(rf"func\s+{re.escape(name)}\s*\(", source)
    if not match:
        raise ValueError(f"Go function {name} not found")
    start = source.index("{", match.start())
    depth = 0
    for index in range(start, len(source)):
        if source[index] == "{":
            depth += 1
        elif source[index] == "}":
            depth -= 1
            if depth == 0:
                return source[start : index + 1]
    raise ValueError(f"Go function {name} is not balanced")


def top_level_switch_cases(function):
    # Semantic kinds are the cases indented exactly one tab in these functions;
    # nested literal switches use two tabs and therefore do not enter coverage.
    values = []
    for raw in re.findall(r'^\tcase\s+([^:]+):', function, flags=re.MULTILINE):
        values.extend(re.findall(r'"([^"]+)"', raw))
    return sorted(set(values))


def sparse_values(matrix):
    values = [[0.0] * matrix["cols"] for _ in range(matrix["rows"])]
    for row, col, value in matrix["entries"]:
        values[row][col] = value
    return values


def percent(part, total):
    return round(100.0 * part / total, 6) if total else 100.0


def main():
    payload = json.loads(SCHEMA.read_text(encoding="utf-8"))
    basis = payload["basis"]
    projection = PROJECTION.read_text(encoding="utf-8")
    semantic_document = SEMANTIC_DOCUMENT.read_text(encoding="utf-8")
    uast_direct = UAST_DIRECT.read_text(encoding="utf-8")
    uast_capability = UAST_CAPABILITY.read_text(encoding="utf-8")

    statement_kinds = top_level_switch_cases(function_source(semantic_document, "documentStatementAST"))
    expression_kinds = top_level_switch_cases(function_source(semantic_document, "documentExpressionAST"))

    mapper_source = function_source(projection, "semanticStatementStructuralKind") + function_source(projection, "semanticExpressionStructuralKind")
    projected_kinds = set(re.findall(r'return\s+"([A-Za-z][A-Za-z0-9]+)"', mapper_source))
    projected_kinds.update(re.findall(r'add\("([A-Za-z][A-Za-z0-9]+)"', projection))
    projected_kinds &= set(basis["structural_kinds"])

    emitted_relations = sorted(set(re.findall(r'"([a-z]+(?:\.[a-z_]+)+)"', projection)) & set(basis["concrete_relations"]))
    direct_relation_block = re.search(r"var\s+directlyConsumedUASTRelations\s*=\s*map\[string\]bool\s*\{(.*?)\n\}", uast_capability, flags=re.DOTALL)
    if not direct_relation_block:
        raise ValueError("direct UAST relation registry not found")
    executable_relations = sorted(set(re.findall(r'"([a-z]+(?:\.[a-z_]+)+)"', direct_relation_block.group(1))) & set(emitted_relations))
    stored_only_relations = sorted(set(emitted_relations) - set(executable_relations))
    direct_field_block = re.search(r"var\s+directUASTFields\s*=\s*map\[string\]bool\s*\{(.*?)\n\}", uast_direct, flags=re.DOTALL)
    if not direct_field_block:
        raise ValueError("direct UAST field registry not found")
    put_fields = set(re.findall(r'"([a-z_]+)"\s*:\s*true', direct_field_block.group(1)))
    node_record_fields = {"id", "kind", "source_span", "semantic_facets", "attributes", "extensions"}
    represented_fields = sorted((put_fields | node_record_fields) & set(basis["fields"]))

    crosswalk_rows = []
    for path in CROSSWALKS:
        with path.open(encoding="utf-8-sig", newline="") as handle:
            for row in csv.DictReader(handle):
                target = row["universal_field"]
                crosswalk_rows.append(
                    {
                        "source_file": path.relative_to(ROOT).as_posix(),
                        "semanticprogram_field": row["semanticprogram_field"],
                        "universal_field": target,
                        "schema_field_present": target in basis["fields"],
                        "exact_node_field_or_record_used": target in represented_fields,
                    }
                )
    unique_crosswalk_targets = sorted({row["universal_field"] for row in crosswalk_rows})
    invalid_crosswalk = sorted({field for field in unique_crosswalk_targets if field not in basis["fields"]})
    unused_crosswalk = sorted({field for field in unique_crosswalk_targets if field not in represented_fields})

    # These channels are not inferred as direct field writes. Each is credited
    # only when the named bridge tokens proving its lossless carrier are present.
    semantic_channel_evidence = {
        "arguments": ['"argument"'],
        "body": ['"body"'],
        "branches": ['"then"', '"else"'],
        "candidates": ["CallResolution"],
        "condition": ['"condition"'],
        "dialect": ["u.Dialects"],
        "dispatch": ["operation.Semantics = semantics"],
        "evaluation_order": ["operation.Semantics = semantics"],
        "exception_model": ["operation.Semantics = semantics"],
        "lifetime": ['put("type_ref"'],
        "members": ['"statement"'],
        "operands": ['"left"', '"right"'],
        "ownership": ["ParameterPassing", 'put("type_ref"'],
        "parameters": ['"parameter"'],
        "runtime_contract": ["u.Contracts"],
        "validation": ["u.Evidence"],
        "value": ["LiteralKind", "Text", '"value"', '"default"'],
    }
    crosswalk_semantic_targets_retained = set(unique_crosswalk_targets) & set(represented_fields)
    crosswalk_semantic_channels = {}
    for target, tokens in semantic_channel_evidence.items():
        proven = all(token in projection for token in tokens)
        crosswalk_semantic_channels[target] = {"tokens": tokens, "proved_in_bridge": proven}
        if proven:
            crosswalk_semantic_targets_retained.add(target)
    crosswalk_semantic_targets_retained &= set(unique_crosswalk_targets)

    structural_seed = sparse_values(basis["structural_facet_seed"])
    kind_index = {kind: row for row, kind in enumerate(basis["structural_kinds"])}
    facets_projected_on_executable_nodes = sorted(
        basis["facets"][col]
        for col in range(len(basis["facets"]))
        if any(structural_seed[kind_index[kind]][col] != 0 for kind in projected_kinds)
    )

    lower = sparse_values(basis["coverage_lower"])
    upper = sparse_values(basis["coverage_upper"])
    language_facet = sparse_values(basis["language_facet"])
    language_coverage = []
    for row, language in enumerate(basis["languages"]):
        intervals = list(zip(lower[row], upper[row]))
        present = [col for col, value in enumerate(language_facet[row]) if value != 0]
        present_intervals = [intervals[col] for col in present]
        denominator = len(intervals)
        present_denominator = len(present_intervals)
        language_coverage.append(
            {
                "language": language,
                "facets_total": denominator,
                "facets_present": len(present),
                "exact_full": sum(lo == 1 and hi == 1 for lo, hi in intervals),
                "exact_none": sum(lo == 0 and hi == 0 for lo, hi in intervals),
                "interval_partial": sum(lo < hi for lo, hi in intervals),
                "lower_mean_all": round(sum(lo for lo, _ in intervals) / denominator, 9),
                "upper_mean_all": round(sum(hi for _, hi in intervals) / denominator, 9),
                "lower_mean_present": round(sum(lo for lo, _ in present_intervals) / present_denominator, 9) if present_denominator else 0,
                "upper_mean_present": round(sum(hi for _, hi in present_intervals) / present_denominator, 9) if present_denominator else 0,
            }
        )

    dimensions = {
        "features": len(basis["features"]),
        "semantic_facets": len(basis["facets"]),
        "structural_kinds": len(basis["structural_kinds"]),
        "concrete_relations": len(basis["concrete_relations"]),
        "fields": len(basis["fields"]),
        "languages": len(basis["languages"]),
    }
    required_summary = {
        "definitions": {
            "structure.executable": "Structural kinds emitted by the current SemanticDocument bridge and consumed by its lossless reverse/executable adapter.",
            "structure.representable_only": "Catalogued structural kinds accepted as UAST data but not emitted/lowered by the executable compatibility bridge.",
            "structure.not_representable": "Kinds inside the declared 109-kind schema universe that cannot be represented; by construction this is zero.",
            "facets.projected_on_executable_nodes": "Union of StructuralFacetSeed rows for the executable structural kinds.",
            "facets.directly_executable": "Facets selected by the direct UAST execution/capability product.",
            "facets.representable": "All catalogued facets accepted by the sparse UAST facet vector.",
            "facets.not_directly_implemented": "Catalogued facets without facet-specific backend/reverse execution; this may overlap projected and representable facets.",
            "relations.projected": "Concrete relation kinds emitted by the forward bridge.",
            "relations.executable": "Projected relations read by reverse/executable adapter logic.",
            "relations.stored_only": "Projected evidence relations validated and retained but not consumed for executable reconstruction.",
            "relations.not_supported": "Catalogued relation kinds not emitted by this compatibility bridge.",
            "fields.roundtrip_retained_direct": "Schema fields represented through exact node Fields or first-class node record members.",
            "fields.roundtrip_retained_crosswalk_semantics": "Unique crosswalk targets whose SemanticDocument information is preserved either directly or through proved structural relations, composite operation/type records, or document-level records. This is a separate denominator and is not a claim that every value is written into its identically named node field.",
        },
        "structure": {
            "total": len(basis["structural_kinds"]),
            "executable": len(projected_kinds),
            "representable_only": len(basis["structural_kinds"]) - len(projected_kinds),
            "not_representable": 0,
        },
        "facets": {
            "total": len(basis["facets"]),
            "projected_on_executable_nodes": len(facets_projected_on_executable_nodes),
            "projected_on_executable_node_ids": facets_projected_on_executable_nodes,
            "directly_executable": len(facets_projected_on_executable_nodes),
            "representable": len(basis["facets"]),
            "not_directly_implemented": len(basis["facets"]) - len(facets_projected_on_executable_nodes),
        },
        "relations": {
            "total": len(basis["concrete_relations"]),
            "projected": len(emitted_relations),
            "executable": len(executable_relations),
            "executable_ids": executable_relations,
            "stored_only": len(stored_only_relations),
            "stored_only_ids": stored_only_relations,
            "not_supported": len(basis["concrete_relations"]) - len(emitted_relations),
        },
        "fields": {
            "total": len(basis["fields"]),
            "roundtrip_retained_direct_uast_field_or_record_channels": len(represented_fields),
            "roundtrip_retained_direct_ids": represented_fields,
            "crosswalk_semantic_targets_total": len(unique_crosswalk_targets),
            "roundtrip_retained_crosswalk_semantic_targets": len(crosswalk_semantic_targets_retained),
            "roundtrip_retained_crosswalk_semantic_target_ids": sorted(crosswalk_semantic_targets_retained),
            "crosswalk_semantic_channels": crosswalk_semantic_channels,
        },
    }
    if required_summary["structure"]["executable"] + required_summary["structure"]["representable_only"] + required_summary["structure"]["not_representable"] != required_summary["structure"]["total"]:
        raise ValueError("structural coverage categories do not partition the schema")
    if required_summary["relations"]["executable"] + required_summary["relations"]["stored_only"] != required_summary["relations"]["projected"]:
        raise ValueError("projected relation categories do not partition bridge emissions")
    if required_summary["relations"]["projected"] + required_summary["relations"]["not_supported"] != required_summary["relations"]["total"]:
        raise ValueError("relation coverage categories do not partition the schema")
    if len(crosswalk_semantic_targets_retained) > len(unique_crosswalk_targets) or len(represented_fields) > len(basis["fields"]):
        raise ValueError("field coverage exceeds its declared universe")
    report = {
        "schema": "code-transpiler.uast-coverage.v1",
        "basis_sha256": payload["basis_sha256"],
        "inputs": {
            "schema": SCHEMA.relative_to(ROOT).as_posix(),
            "projection": PROJECTION.relative_to(ROOT).as_posix(),
            "semantic_document": SEMANTIC_DOCUMENT.relative_to(ROOT).as_posix(),
            "crosswalks": [path.relative_to(ROOT).as_posix() for path in CROSSWALKS],
        },
        "dimensions": dimensions,
        "required_summary": required_summary,
        "executable_compatibility_projection": {
            "semantic_statement_kinds": statement_kinds,
            "semantic_expression_kinds": expression_kinds,
            "structural_kinds_emitted": sorted(projected_kinds),
            "structural_kind_coverage_percent": percent(len(projected_kinds), len(basis["structural_kinds"])),
            "relations_emitted": emitted_relations,
            "relation_coverage_percent": percent(len(emitted_relations), len(basis["concrete_relations"])),
            "fields_represented_as_node_fields_or_records": represented_fields,
            "field_coverage_percent": percent(len(represented_fields), len(basis["fields"])),
        },
        "crosswalk": {
            "rows": len(crosswalk_rows),
            "unique_targets": len(unique_crosswalk_targets),
            "schema_consistent": not invalid_crosswalk,
            "invalid_targets": invalid_crosswalk,
            "targets_without_exact_node_field_or_record_write": unused_crosswalk,
            "entries": crosswalk_rows,
        },
        "coverage_intervals": language_coverage,
        "schema_space_not_emitted_by_compatibility_projection": {
            "structural_kinds": sorted(set(basis["structural_kinds"]) - projected_kinds),
            "relations": sorted(set(basis["concrete_relations"]) - set(emitted_relations)),
            "fields": sorted(set(basis["fields"]) - set(represented_fields)),
        },
        "interpretation": {
            "structural_and_relation_percentages": "Share of the complete UAST schema emitted by the current executable SemanticDocument compatibility bridge; this is not language semantic parity.",
            "coverage_lower_upper": "Exact matrix intervals retained per language and facet; no scalar rank or priority is introduced.",
            "crosswalk_exact_use": "Reports exact target field/record writes visible in the bridge. Semantics preserved through syntax.child relations or document-level records remain distinguishable from exact target-field use.",
        },
    }

    OUT_DIR.mkdir(parents=True, exist_ok=True)
    json_path = OUT_DIR / "coverage.json"
    csv_path = OUT_DIR / "coverage.csv"
    json_path.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n", encoding="utf-8")

    rows = []
    rows.extend(
        [
            ["required_summary", "structure.executable", len(basis["structural_kinds"]), len(projected_kinds), percent(len(projected_kinds), len(basis["structural_kinds"])), "", "", "calculated", "reverse/executable adapter"],
            ["required_summary", "structure.representable_only", len(basis["structural_kinds"]), len(basis["structural_kinds"]) - len(projected_kinds), percent(len(basis["structural_kinds"]) - len(projected_kinds), len(basis["structural_kinds"])), "", "", "calculated", "schema data only"],
            ["required_summary", "structure.not_representable", len(basis["structural_kinds"]), 0, 0, "", "", "calculated", "inside declared schema universe"],
            ["required_summary", "facets.projected_on_executable_nodes", len(basis["facets"]), len(facets_projected_on_executable_nodes), percent(len(facets_projected_on_executable_nodes), len(basis["facets"])), "", "", "calculated", "union of structural seed rows"],
            ["required_summary", "facets.directly_executable", len(basis["facets"]), len(facets_projected_on_executable_nodes), percent(len(facets_projected_on_executable_nodes), len(basis["facets"])), "", "", "calculated", "direct UAST capability product"],
            ["required_summary", "facets.representable", len(basis["facets"]), len(basis["facets"]), 100, "", "", "calculated", "sparse facet vector"],
            ["required_summary", "facets.not_directly_implemented", len(basis["facets"]), len(basis["facets"]) - len(facets_projected_on_executable_nodes), percent(len(basis["facets"]) - len(facets_projected_on_executable_nodes), len(basis["facets"])), "", "", "calculated", "outside direct execution classes"],
            ["required_summary", "relations.projected", len(basis["concrete_relations"]), len(emitted_relations), percent(len(emitted_relations), len(basis["concrete_relations"])), "", "", "calculated", "forward bridge"],
            ["required_summary", "relations.executable", len(basis["concrete_relations"]), len(executable_relations), percent(len(executable_relations), len(basis["concrete_relations"])), "", "", "calculated", "reverse adapter reads"],
            ["required_summary", "relations.stored_only", len(basis["concrete_relations"]), len(stored_only_relations), percent(len(stored_only_relations), len(basis["concrete_relations"])), "", "", "calculated", "validated evidence"],
            ["required_summary", "relations.not_supported", len(basis["concrete_relations"]), len(basis["concrete_relations"]) - len(emitted_relations), percent(len(basis["concrete_relations"]) - len(emitted_relations), len(basis["concrete_relations"])), "", "", "calculated", "not emitted"],
            ["required_summary", "fields.roundtrip_retained_direct", len(basis["fields"]), len(represented_fields), percent(len(represented_fields), len(basis["fields"])), "", "", "calculated", "exact field or record channel"],
            ["required_summary", "fields.roundtrip_retained_crosswalk_semantics", len(unique_crosswalk_targets), len(crosswalk_semantic_targets_retained), percent(len(crosswalk_semantic_targets_retained), len(unique_crosswalk_targets)), "", "", "calculated", "direct, relation, composite, or document record"],
            ["summary", "structural_kinds", len(basis["structural_kinds"]), len(projected_kinds), percent(len(projected_kinds), len(basis["structural_kinds"])), "", "", "calculated", "current bridge emissions"],
            ["summary", "concrete_relations", len(basis["concrete_relations"]), len(emitted_relations), percent(len(emitted_relations), len(basis["concrete_relations"])), "", "", "calculated", "current bridge emissions"],
            ["summary", "fields", len(basis["fields"]), len(represented_fields), percent(len(represented_fields), len(basis["fields"])), "", "", "calculated", "node fields or node records"],
            ["summary", "crosswalk_targets", len(unique_crosswalk_targets), len(unique_crosswalk_targets) - len(invalid_crosswalk), percent(len(unique_crosswalk_targets) - len(invalid_crosswalk), len(unique_crosswalk_targets)), "", "", "pass" if not invalid_crosswalk else "fail", "schema membership"],
        ]
    )
    for item in language_coverage:
        rows.append(["language_coverage", item["language"], item["facets_total"], item["facets_present"], percent(item["facets_present"], item["facets_total"]), item["lower_mean_all"], item["upper_mean_all"], "matrix_interval", f'exact_full={item["exact_full"]};exact_none={item["exact_none"]};partial={item["interval_partial"]}'])
    for kind in basis["structural_kinds"]:
        rows.append(["structural_kind", kind, 1, int(kind in projected_kinds), 100 if kind in projected_kinds else 0, "", "", "emitted" if kind in projected_kinds else "not_emitted", ""])
    for relation in basis["concrete_relations"]:
        rows.append(["relation", relation, 1, int(relation in emitted_relations), 100 if relation in emitted_relations else 0, "", "", "emitted" if relation in emitted_relations else "not_emitted", ""])
    for field in basis["fields"]:
        rows.append(["field", field, 1, int(field in represented_fields), 100 if field in represented_fields else 0, "", "", "represented" if field in represented_fields else "not_represented", "node field or node record"])
    for row in crosswalk_rows:
        rows.append(["crosswalk", row["semanticprogram_field"], 1, int(row["schema_field_present"]), 100 if row["schema_field_present"] else 0, "", "", "pass" if row["schema_field_present"] else "fail", f'{row["universal_field"]};exact_use={str(row["exact_node_field_or_record_used"]).lower()};source={row["source_file"]}'])

    with csv_path.open("w", encoding="utf-8", newline="") as handle:
        writer = csv.writer(handle, lineterminator="\n")
        writer.writerow(["section", "key", "total", "supported", "coverage_percent", "lower", "upper", "status", "detail"])
        writer.writerows(rows)

    print(f"Wrote {json_path.relative_to(ROOT)} and {csv_path.relative_to(ROOT)}")
    print(f"Bridge coverage: {len(projected_kinds)}/{len(basis['structural_kinds'])} kinds, {len(emitted_relations)}/{len(basis['concrete_relations'])} relations, {len(represented_fields)}/{len(basis['fields'])} fields")
    print(f"Crosswalk: {len(crosswalk_rows)} rows, {len(unique_crosswalk_targets)} targets, {len(invalid_crosswalk)} invalid")


if __name__ == "__main__":
    main()
