import csv
from collections import Counter
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / 'outputs' / 'unified-route-evidence'
OUT.mkdir(parents=True, exist_ok=True)

INPUTS = [
    ROOT / 'outputs/partial-unified-matrix/partial_unified_matrix.csv',
    ROOT / 'outputs/runtime-smoke-controlled/transitive_route_matrix.csv',
    ROOT / 'outputs/runtime-smoke-controlled/runtime_contract_matrix.csv',
    ROOT / 'outputs/runtime-smoke-controlled/runtime_failure_primitive_matrix.csv',
    ROOT / 'outputs/runtime-smoke-controlled/runtime_failure_contract_matrix.csv',
    ROOT / 'outputs/compiler-semantic-oracle/compiler_semantic_oracle_matrix.csv',
    ROOT / 'outputs/py2many-contracts/py2many_contract_matrix.csv',
    ROOT / 'outputs/py2many-contracts/py2many_lowering_contracts.csv',
    ROOT / 'outputs/py2many-contracts/py2many_ast_target_contracts.csv',
    ROOT / 'outputs/py2many-contracts/py2many_fixture_coverage.csv',
    ROOT / 'outputs/compiler-semantic-oracle/compiler_ir_to_uast_matrix.csv',
    ROOT / 'outputs/compiler-semantic-oracle/compiler_ir_semantic_matrix.csv',
    ROOT / 'outputs/compiler-semantic-oracle/primitive_existing_implementation_matrix.csv',
    ROOT / 'outputs/compiler-semantic-oracle/primitive_missing_matrix.csv',
    ROOT / 'outputs/miner-v4.3-all-to-all/all_to_all_results.csv',
    ROOT / 'outputs/miner-v4.3-all-to-all/atomic_primitive_matrix.csv',
    ROOT / 'outputs/miner-v4.3-all-to-all/primitive_coverage.csv',
    ROOT / 'outputs/miner-v4.3-all-to-all/primitive_handler_mapping.csv',
    ROOT / 'outputs/unmapped-oracle-evidence/empirical_evidence_matrix.csv',
    ROOT / 'outputs/unmapped-oracle-evidence/empirical_primitive_matrix.csv',
]

FIELDS = ['source', 'language', 'target', 'route', 'package', 'case', 'construct',
          'primitive', 'status', 'stage', 'diagnostic', 'signature']

def read_rows(path):
    if not path.exists():
        return []
    with path.open('r', encoding='utf-8-sig', newline='') as f:
        for row in csv.DictReader(f):
            yield row

def normalize(path, row):
    def first(*keys):
        for k in keys:
            v = row.get(k, '')
            if v not in (None, ''):
                return str(v)
        return ''
    route = first('route', 'source_route')
    language = first('language', 'source_language', 'src_language')
    target = first('target', 'target_language')
    if not language and '->' in route:
        language = route.split('->', 1)[0]
    if not target and '->' in route:
        parts = route.split('->')
        target = parts[1] if len(parts) > 1 else ''
    status = first('status', 'result', 'classification', 'outcome').upper()
    if status in ('OK', 'SUCCESS', 'DIRECT', 'NATIVE'):
        status = 'PASS'
    if status in ('ERROR', 'FAILED'):
        status = 'FAIL'
    return {
        'source': path.stem,
        'language': language,
        'target': target,
        'route': route,
        'package': first('package', 'package_name'),
        'case': first('case', 'case_id', 'fixture', 'file'),
        'construct': first('construct', 'event', 'failure_class'),
        'primitive': first('primitive', 'primitive_candidates', 'execution_primitive'),
        'status': status,
        'stage': first('stage', 'first_bad_stage', 'failure_stage'),
        'diagnostic': first('diagnostic', 'error', 'message'),
        'signature': first('signature', 'technical_signature', 'failure_signature'),
    }

def main():
    rows = []
    for path in INPUTS:
        for raw in read_rows(path):
            rows.append(normalize(path, raw))
    out = OUT / 'large_route_evidence_matrix.csv'
    with out.open('w', encoding='utf-8', newline='') as f:
        w = csv.DictWriter(f, fieldnames=FIELDS)
        w.writeheader(); w.writerows(rows)
    counts = Counter(r['status'] or 'UNKNOWN' for r in rows)
    classes = Counter((r['signature'] or r['construct'] or r['diagnostic'][:120]) for r in rows if r['status'] == 'FAIL')
    summary_rows = [
        {'metric':'ROWS','value':len(rows)},
        *({'metric':f'STATUS_{k}','value':v} for k,v in sorted(counts.items())),
        {'metric':'FAILURE_SIGNATURE_CLASSES','value':len(classes)},
        {'metric':'INPUT_FILES_FOUND','value':sum(p.exists() for p in INPUTS)},
    ]
    with (OUT / 'large_route_evidence_summary.csv').open('w', encoding='utf-8', newline='') as f:
        w=csv.DictWriter(f,fieldnames=['metric','value']); w.writeheader(); w.writerows(summary_rows)
    print(f'ROWS={len(rows)}')
    print(' '.join(f'{k}={v}' for k,v in sorted(counts.items())))
    print(f'FAILURE_SIGNATURE_CLASSES={len(classes)}')
    print(f'INPUT_FILES_FOUND={sum(p.exists() for p in INPUTS)}')

if __name__ == '__main__':
    main()
