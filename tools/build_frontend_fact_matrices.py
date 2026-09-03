import csv, glob, json, os, collections

root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
capture_dir = os.path.join(root, 'outputs', 'frontend-fact-captures')
out_dir = os.path.join(root, 'outputs', 'frontend-fact-matrices')
os.makedirs(out_dir, exist_ok=True)

rows = []
for path in sorted(glob.glob(os.path.join(capture_dir, '*.semantic.json'))):
    try:
        doc = json.load(open(path, encoding='utf-8'))
    except (OSError, ValueError):
        continue
    origin = doc.get('origin') or {}
    language = origin.get('source_language', '')
    for node in doc.get('nodes') or []:
        fields = node.get('fields') or {}
        rows.append({
            'capture': os.path.basename(path),
            'language': language,
            'node_id': node.get('id', ''),
            'structural_kind': node.get('structural_kind', ''),
            'semantic_facets': '|'.join(sorted(set(node.get('semantic_facets') or []))),
            'field_mask': '|'.join(sorted(set(node.get('field_mask') or []))),
            'observed_field_keys': '|'.join(sorted(fields)),
            'relation_count': sum(1 for r in (doc.get('relations') or []) if str(r.get('from', r.get('source', ''))) == str(node.get('id', ''))),
        })

def write(name, fields, data):
    with open(os.path.join(out_dir, name), 'w', newline='', encoding='utf-8') as f:
        w = csv.DictWriter(f, fieldnames=fields); w.writeheader(); w.writerows(data)

write('frontend_fact_uast_matrix.csv', list(rows[0]) if rows else ['capture','language','node_id','structural_kind','semantic_facets','field_mask','observed_field_keys','relation_count'], rows)

summary = []
by = collections.defaultdict(list)
for r in rows: by[(r['language'], r['structural_kind'])].append(r)
for (lang, kind), items in sorted(by.items()):
    summary.append({'language': lang, 'structural_kind': kind, 'captures': len(set(x['capture'] for x in items)), 'node_count': len(items), 'facets': '|'.join(sorted(set('|'.join(x['semantic_facets'].split('|')) for x in items if x['semantic_facets']))), 'fields': '|'.join(sorted(set('|'.join(x['observed_field_keys'].split('|')) for x in items if x['observed_field_keys']))), 'relations': sum(int(x['relation_count']) for x in items)})
write('frontend_fact_uast_summary.csv', ['language','structural_kind','captures','node_count','facets','fields','relations'], summary)

langs = sorted(set(r['language'] for r in rows))
kinds = sorted(set(r['structural_kind'] for r in rows))
coverage = []
for lang in langs:
    have = {r['structural_kind'] for r in rows if r['language'] == lang}
    for kind in kinds:
        coverage.append({'language': lang, 'structural_kind': kind, 'frontend_fact_present': int(kind in have), 'uast_observed': int(kind in have), 'backend_gap': 'UNMEASURED'})
write('frontend_uast_capability_matrix.csv', ['language','structural_kind','frontend_fact_present','uast_observed','backend_gap'], coverage)

with open(os.path.join(out_dir, 'README.txt'), 'w', encoding='utf-8') as f:
    f.write('Derived only from local semantic-export JSON captures. backend_gap=UNMEASURED means no target projection claim is inferred.\n')
print(f'CAPTURES={len(set(r["capture"] for r in rows))} NODES={len(rows)} LANGUAGES={len(langs)} STRUCTURES={len(kinds)}')
