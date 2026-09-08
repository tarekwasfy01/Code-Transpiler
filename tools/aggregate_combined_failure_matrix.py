import csv, collections, glob, os

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
OUT = os.path.join(ROOT, "outputs", "combined-miner-sitepackages-matrix")
os.makedirs(OUT, exist_ok=True)

rows = []
miner = os.path.join(ROOT, "outputs", "miner-v4.3-all-to-all", "transpile_attempts.csv")
if os.path.exists(miner):
    with open(miner, encoding="utf-8", newline="") as f:
        for r in csv.DictReader(f):
            if r.get("status") == "failed":
                rows.append({"evidence_source":"miner", "source_language":r.get("source_language",""),
                             "target":r.get("target",""), "failure_signature":r.get("failure_signature","")})

site = os.path.join(ROOT, "outputs", "site-packages-alltoall-matrix", "failure_matrix.csv")
if os.path.exists(site):
    with open(site, encoding="utf-8", newline="") as f:
        for r in csv.DictReader(f):
            for _ in range(int(r.get("occurrences", "0") or 0)):
                rows.append({"evidence_source":"site-packages", "source_language":r.get("source_language",""),
                             "target":r.get("target",""), "failure_signature":r.get("failure_signature","")})

counts = collections.Counter((r["evidence_source"], r["source_language"], r["target"], r["failure_signature"]) for r in rows)
matrix = [{"evidence_source":a,"source_language":b,"target":c,"failure_signature":d,"occurrences":n}
          for (a,b,c,d),n in sorted(counts.items())]
def write(name, fields, data):
    with open(os.path.join(OUT, name), "w", encoding="utf-8", newline="") as f:
        w=csv.DictWriter(f, fieldnames=fields); w.writeheader(); w.writerows(data)
write("failure_matrix.csv", ["evidence_source","source_language","target","failure_signature","occurrences"], matrix)

rows_by_vector = collections.Counter((r["source_language"], r["target"], r["failure_signature"]) for r in rows)
write("exact_failure_row_classes.csv", ["row_class","source_language","target","failure_signature","occurrences"],
      [{"row_class":f"ROW_{i:05d}","source_language":a,"target":b,"failure_signature":c,"occurrences":n}
       for i,((a,b,c),n) in enumerate(sorted(rows_by_vector.items()),1)])
cols = collections.Counter(r["failure_signature"] for r in rows)
write("exact_failure_column_classes.csv", ["column_class","failure_signature","occurrences"],
      [{"column_class":f"COL_{i:05d}","failure_signature":s,"occurrences":n} for i,(s,n) in enumerate(cols.most_common(),1)])

print(f"FAILURE_ROWS={len(rows)} EXACT_ROW_CLASSES={len(rows_by_vector)} EXACT_COLUMN_CLASSES={len(cols)}")
print("SOURCES=" + ",".join(sorted({r['evidence_source'] for r in rows})))
print("LANGUAGES=" + ",".join(sorted({r['source_language'] for r in rows})))
