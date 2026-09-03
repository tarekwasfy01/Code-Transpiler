"""Build the embedded SemanticProgram feature space from calculated CSV matrices."""
import argparse, csv, hashlib, json
from pathlib import Path

def csv_rows(path):
    with path.open(encoding="utf-8-sig", newline="") as handle:
        return list(csv.DictReader(handle))

def dense(rows, columns):
    return {"Rows":len(rows), "Cols":len(columns),
            "Data":[float(row[column]) for row in rows for column in columns]}

def projection(path, row_key, expected_rows):
    records=csv_rows(path)
    columns=[key for key in records[0] if key != row_key]
    by={record[row_key]:record for record in records}
    if set(by) != set(expected_rows):
        raise ValueError(f"projection basis mismatch in {path}")
    ordered=[by[name] for name in expected_rows]
    return columns,dense(ordered,columns)

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--report",type=Path)
    parser.add_argument("--out",type=Path,default=Path("internal/backend/semantic_feature_space.json"))
    args=parser.parse_args()
    report=args.report
    if report is None:
        candidates=sorted(Path("outputs/all-handoffs").glob("*/language-dialect-feature.csv"))
        if not candidates: raise FileNotFoundError("no calculated all-handoffs report")
        report=candidates[-1].parent
    generic=csv_rows(report/"language-feature.csv")
    languages=[row["language"] for row in generic]
    features=[key for key in generic[0] if key != "language"]
    dialect=csv_rows(report/"language-dialect-feature.csv")
    dialect_features=[key for key in dialect[0] if key != "language"]
    observed={row["language"]:float(row["observed"]) for row in csv_rows(report/"language-dialect-observed.csv")}
    nodes,feature_node=projection(Path("matrices/handoffs/go/08_feature_uast_node_matrix.csv"),"go_feature",features)
    relations,feature_relation=projection(Path("matrices/handoffs/go/09_feature_relation_matrix.csv"),"go_feature",features)
    provenance=json.loads((report/"report.json").read_text(encoding="utf-8"))["archive_sha256"]
    basis={"schema":"code-transpiler.semantic-feature-basis.v1","languages":languages,"features":features,
           "language_feature":dense(generic,features),"dialect_features":dialect_features,
           "language_dialect_feature":dense(dialect,dialect_features),
           "dialect_observed":[observed[name] for name in languages],"node_kinds":nodes,
           "feature_node":feature_node,"relation_kinds":relations,"feature_relation":feature_relation,
           "provenance":provenance}
    canonical=json.dumps(basis,sort_keys=True,separators=(",",":"),ensure_ascii=False).encode()
    payload={"basis_sha256":hashlib.sha256(canonical).hexdigest(),"basis":basis}
    args.out.parent.mkdir(parents=True,exist_ok=True)
    args.out.write_text(json.dumps(payload,sort_keys=True,separators=(",",":"),ensure_ascii=False)+"\n",encoding="utf-8")
    print(f"Wrote {len(languages)}x{len(features)} generic, {len(languages)}x{len(dialect_features)} dialect, {len(features)}x{len(nodes)} node and {len(features)}x{len(relations)} relation matrices to {args.out.resolve()}")

if __name__ == "__main__": main()
