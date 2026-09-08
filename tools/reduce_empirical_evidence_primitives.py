import csv,re
from pathlib import Path
from collections import Counter
ROOT=Path(__file__).resolve().parents[1]
SRC=ROOT/'outputs/unmapped-oracle-evidence/empirical_evidence_matrix.csv'
OUT=ROOT/'outputs/unmapped-oracle-evidence'
RULES=[('CALL',r'call|invoke|apply|message'),('CONTROL_TRANSFER',r'return|break|continue|yield|goto|unreachable|resume'),('BINARY_UNARY_OPERATOR',r'add|sub|mul|div|rem|mod|and|or|xor|shift|cmp|compare|binary|unary'),('BINDING_REFERENCE',r'load|store|declref|name|argument|parameter'),('AGGREGATE',r'array|tuple|list|map|set|record|struct|aggregate|initializer'),('INDEX_SLICE',r'index|subscript|slice|element'),('CONVERSION',r'cast|convert|coerce|bitcast|extend|trunc'),('ALLOCATION',r'alloc|new|create|box|memory'),('EXCEPTION',r'throw|raise|panic|abort|trap|catch'),('CONDITIONAL',r'branch|if|switch|case|select|conditional'),('ITERATION',r'loop|for|while|range|iterate'),('TYPE',r'type|class|interface|enum|pointer|functiontype')]
def main():
 rows=list(csv.DictReader(SRC.open(encoding='utf-8-sig'))); out=[]; counts=Counter(); primitive_names=[p for p,_ in RULES]
 for r in rows:
  text=(r.get('compiler_node','')+' '+r.get('evidence','')).lower(); hits=[p for p,pat in RULES if re.search(pat,text)]
  primitive=hits[0] if len(hits)==1 else ('AMBIGUOUS' if hits else 'UNMAPPED')
  confidence='HIGH' if len(hits)==1 and r['status']=='EMPIRICALLY_PROVEN' else ('MEDIUM' if len(hits)==1 else 'NONE')
  counts[(primitive,confidence)]+=1; row={**r,'primitive_candidate':primitive,'confidence':confidence}
  for p in primitive_names: row['requires_'+p.lower()]=1 if p in hits else 0
  out.append(row)
 fields=list(out[0]) if out else []
 with (OUT/'empirical_primitive_matrix.csv').open('w',encoding='utf-8',newline='') as f:
  w=csv.DictWriter(f,fieldnames=fields); w.writeheader(); w.writerows(out)
 with (OUT/'empirical_primitive_summary.csv').open('w',encoding='utf-8',newline='') as f:
  w=csv.DictWriter(f,fieldnames=['primitive_candidate','confidence','rows']); w.writeheader(); w.writerows({'primitive_candidate':p,'confidence':c,'rows':n} for (p,c),n in sorted(counts.items()))
 print(f'PRIMITIVE_MATRIX_ROWS={len(out)}'); print(f'UNIQUE_PRIMITIVE_CANDIDATES={len({r["primitive_candidate"] for r in out})}'); print(f'HIGH_CONFIDENCE={sum(v for (p,c),v in counts.items() if c=="HIGH")}'); print(f'MEDIUM_CONFIDENCE={sum(v for (p,c),v in counts.items() if c=="MEDIUM")}'); print(f'AMBIGUOUS_OR_UNMAPPED={sum(v for (p,c),v in counts.items() if p in ("AMBIGUOUS","UNMAPPED"))}')
if __name__=='__main__': main()
