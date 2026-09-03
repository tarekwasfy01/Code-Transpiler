from pathlib import Path
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
