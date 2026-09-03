#!/usr/bin/env python3
"""Repair execution-ready Tree-sitter runtime tables directly from generated parser.c.

This is a generic extractor/reconciler. It contains no language-specific rules.
It performs three source-derived repairs:
  1) restore source order of ts_lex / ts_lex_keywords transitions;
  2) add reserved_word_set_id and reserved-word symbol sets (ABI-15 data);
  3) add explicit large parse-table states missing from parse_dispatch.csv.
"""
from __future__ import annotations
import argparse, csv, hashlib, re, shutil
from pathlib import Path


def read_rows(path: Path):
    with path.open(encoding="utf-8-sig", newline="") as f:
        return list(csv.DictReader(f))

def split_c_args(s):
    out=[]; cur=[]; q=None; esc=False; depth=0
    for ch in s:
        if q:
            cur.append(ch)
            if esc: esc=False
            elif ch=='\\': esc=True
            elif ch==q: q=None
            continue
        if ch in "'\"": q=ch; cur.append(ch); continue
        if ch=='(': depth+=1
        elif ch==')' and depth: depth-=1
        if ch==',' and depth==0: out.append(''.join(cur).strip()); cur=[]
        else: cur.append(ch)
    if ''.join(cur).strip(): out.append(''.join(cur).strip())
    return out

def c_char(tok):
    tok=tok.strip()
    if re.fullmatch(r'0[xX][0-9A-Fa-f]+',tok): return int(tok,16)
    if re.fullmatch(r'-?\d+',tok): return int(tok)
    if tok.startswith("'") and tok.endswith("'"):
        body=tok[1:-1]
        try:
            val=bytes(body,'utf-8').decode('unicode_escape')
            return ord(val[0]) if val else 0
        except Exception: return None
    return None

def match_paren(s, openpos):
    depth=0; q=None; esc=False
    for i in range(openpos,len(s)):
        ch=s[i]
        if q:
            if esc: esc=False
            elif ch=='\\': esc=True
            elif ch==q: q=None
            continue
        if ch in "'\"": q=ch; continue
        if ch=='(': depth+=1
        elif ch==')':
            depth-=1
            if depth==0:return i
    return -1

def preceding_guard_hash(block, macro_pos):
    search=macro_pos
    while True:
        ip=block.rfind('if',0,search)
        if ip<0:return None
        op=block.find('(',ip,macro_pos)
        if op<0:search=ip;continue
        cp=match_paren(block,op)
        if cp>=0 and cp<macro_pos and block[cp+1:macro_pos].strip()=='':
            norm=' '.join(block[op+1:cp].split())
            return hashlib.sha256(norm.encode()).hexdigest()[:16]
        search=ip

def function_ranges(src):
    out={}
    a=src.find('static bool ts_lex(')
    if a>=0:
        b=src.find('static bool ts_lex_keywords',a)
        if b<0:b=src.find('static const TSLex',a)
        out['ts_lex']=src[a:b]
    a=src.find('static bool ts_lex_keywords')
    if a>=0:
        b=src.find('static const TSLex',a)
        out['ts_lex_keywords']=src[a:b]
    return out

def case_events(body):
    cases={}; ms=list(re.finditer(r'\bcase\s+(\d+)\s*:',body))
    for idx,m in enumerate(ms):
        st=int(m.group(1)); end=body.find('END_STATE();',m.end())
        if end<0:end=ms[idx+1].start() if idx+1<len(ms) else len(body)
        block=body[m.end():end]; events=[]; pos=0
        while pos<len(block):
            cand=[]
            for name in ('ADVANCE_MAP','ADVANCE','SKIP'):
                q=block.find(name+'(',pos)
                if q>=0:cand.append((q,name))
            if not cand:break
            q,name=min(cand); op=block.find('(',q); cp=match_paren(block,op)
            if cp<0:break
            args=block[op+1:cp]
            if name=='ADVANCE_MAP':
                toks=split_c_args(args)
                for i in range(0,len(toks)-1,2):
                    code=c_char(toks[i]); target=int(toks[i+1]) if re.fullmatch(r'\d+',toks[i+1].strip()) else None
                    if code is not None and target is not None:
                        events.append({'kind':'ADVANCE','target':target,'map':code,'hash':None})
            else:
                target=int(args.strip()) if re.fullmatch(r'\d+',args.strip()) else None
                if target is not None:
                    events.append({'kind':name,'target':target,'map':None,'hash':preceding_guard_hash(block,q)})
            pos=cp+1
        cases[st]=events
    return cases

def balanced_brace_body(src, start_pattern):
    m=re.search(start_pattern,src)
    if not m:return None
    i=m.end(); depth=1; j=i
    while j<len(src) and depth:
        if src[j]=='{':depth+=1
        elif src[j]=='}':depth-=1
        j+=1
    return src[i:j-1]

def parser_sources(root: Path):
    return [(p.parent.name,p,p.read_text(errors='replace')) for p in sorted(root.glob('*/parser.c'))]

def restore_lex_order(execdir: Path, sources):
    path=execdir/'lex_dispatch.csv'; rows=read_rows(path); fields=list(rows[0])
    by={}
    for r in rows:by.setdefault((r['language'],r['lexer_function'],int(r['lex_state'])),[]).append(r)
    new=[]; warnings=[]
    for lang,_,src in sources:
        for fn,body in function_ranges(src).items():
            cases=case_events(body)
            for st in sorted(x[2] for x in by if x[0]==lang and x[1]==fn):
                old=by[(lang,fn,st)]; used=[False]*len(old); ordered=[]
                for ev in cases.get(st,[]):
                    cand=[]
                    for i,r in enumerate(old):
                        if used[i] or r['transition_kind']!=ev['kind'] or int(r['target_lex_state'] or 0)!=ev['target']:continue
                        rm=int(r['map_codepoint']) if r['map_codepoint'] else None
                        if ev['map'] is not None and rm!=ev['map']:continue
                        if ev['map'] is None and rm is not None:continue
                        cand.append(i)
                    if ev['hash']:
                        exact=[i for i in cand if old[i]['source_guard_hash']==ev['hash']]
                        if exact:cand=exact
                    if not cand:
                        warnings.append((lang,fn,st,'unmatched'));continue
                    i=cand[0];used[i]=True;ordered.append(old[i])
                leftovers=[r for i,r in enumerate(old) if not used[i]]
                if leftovers:warnings.append((lang,fn,st,f'leftover:{len(leftovers)}'));ordered.extend(leftovers)
                for ordinal,r in enumerate(ordered):
                    x=dict(r);x['transition_ordinal']=str(ordinal);new.append(x)
    seen={(r['language'],r['lexer_function'],r['lex_state'],r['predicate_id'],r['target_lex_state'],r['map_codepoint']) for r in new}
    for r in rows:
        key=(r['language'],r['lexer_function'],r['lex_state'],r['predicate_id'],r['target_lex_state'],r['map_codepoint'])
        if key not in seen:new.append(r)
    changed=sum(1 for a,b in zip(sorted(rows,key=lambda r:(r['language'],r['lexer_function'],int(r['lex_state']),r['predicate_id'],r['target_lex_state'],r['map_codepoint'])), sorted(new,key=lambda r:(r['language'],r['lexer_function'],int(r['lex_state']),r['predicate_id'],r['target_lex_state'],r['map_codepoint']))) if a.get('transition_ordinal')!=b.get('transition_ordinal'))
    out=execdir/'lex_dispatch_source_order.csv'
    with out.open('w',encoding='utf-8',newline='') as f:
        w=csv.DictWriter(f,fieldnames=fields);w.writeheader();w.writerows(new)
    if warnings: raise RuntimeError(f'lex reconciliation not lossless: {warnings[:5]}')
    return len(rows),changed,out

def symbol_maps(execdir):
    byname={}
    for r in read_rows(execdir/'symbols.csv'):
        byname[(r['language'],r['symbol_name'])]=(int(r['symbol_id']),r['symbol_kind'])
    return byname

def reserved_word_supplements(execdir:Path,sources,byname):
    mode_rows=[]; word_rows=[]
    for lang,_,src in sources:
        body=balanced_brace_body(src,r'static const TSLex(?:er)?Mode\s+ts_lex_modes\s*\[[^\]]+\]\s*=\s*\{')
        if body:
            for m in re.finditer(r'\[(\d+)\]\s*=\s*\{([^{}]*)\}',body):
                st=int(m.group(1)); mm=re.search(r'\.reserved_word_set_id\s*=\s*(\d+)',m.group(2))
                if mm: mode_rows.append([lang,st,int(mm.group(1))])
        body=balanced_brace_body(src,r'static const TSSymbol\s+ts_reserved_words\s*\[[^\]]+\]\s*\[[^\]]+\]\s*=\s*\{')
        if body:
            for sm in re.finditer(r'\[(\d+)\]\s*=\s*\{',body):
                sid=int(sm.group(1)); i=sm.end(); dep=1
                while i<len(body) and dep:
                    if body[i]=='{':dep+=1
                    elif body[i]=='}':dep-=1
                    i+=1
                blk=body[sm.end():i-1]
                for name in re.findall(r'(?m)^\s*(?:\[\d+\]\s*=\s*)?([A-Za-z_][A-Za-z0-9_]*)\s*,',blk):
                    info=byname.get((lang,name))
                    if info:word_rows.append([lang,sid,info[0],name])
    with (execdir/'lex_reserved_word_sets.csv').open('w',encoding='utf-8',newline='') as f:
        w=csv.writer(f);w.writerow(['language','parse_state','reserved_word_set_id']);w.writerows(mode_rows)
    with (execdir/'reserved_words.csv').open('w',encoding='utf-8',newline='') as f:
        w=csv.writer(f);w.writerow(['language','reserved_word_set_id','symbol_id','symbol_name']);w.writerows(word_rows)
    return len(mode_rows),len(word_rows)

def lex_mode_supplement(execdir: Path, sources):
    """Reconcile the per-parser-state lexer mode table from parser.c.

    Some older exports contain lex transitions/accepts for every language but
    omit the corresponding ``ts_lex_modes`` partition.  The generated parser
    is the authoritative source for this small table; copying its designated
    initializers is lossless and keeps the runtime language-neutral.
    """
    path = execdir / 'lex_modes.csv'
    existing = read_rows(path) if path.exists() else []
    fields = list(existing[0]) if existing else ['language','parse_state','lex_state','external_lex_state']
    seen = {(r.get('language',''), int(r.get('parse_state','0') or 0)) for r in existing}
    additions = []
    for lang, _, src in sources:
        body = balanced_brace_body(src, r'static const TSLex(?:er)?Mode\s+ts_lex_modes\s*\[[^\]]+\]\s*=\s*\{')
        if body is None:
            continue
        for m in re.finditer(r'(?m)^\s*\[(\d+)\]\s*=\s*\{([^{}]*)\}', body):
            state = int(m.group(1))
            if (lang, state) in seen:
                continue
            item = m.group(2)
            lm = re.search(r'\.lex_state\s*=\s*(-?\d+)', item)
            em = re.search(r'\.external_lex_state\s*=\s*(-?\d+)', item)
            lex_state = int(lm.group(1)) if lm else 0
            external = int(em.group(1)) if em else 0
            additions.append({'language': lang, 'parse_state': str(state), 'lex_state': str(lex_state), 'external_lex_state': str(external)})
            seen.add((lang, state))
    merged = existing + additions
    merged.sort(key=lambda r: (r.get('language',''), int(r.get('parse_state','0') or 0)))
    with path.open('w', encoding='utf-8', newline='') as f:
        w = csv.DictWriter(f, fieldnames=fields)
        w.writeheader()
        for r in merged:
            w.writerow({k: r.get(k, '') for k in fields})
    return len(additions), sorted({r['language'] for r in additions})

def parse_dispatch_supplement(execdir:Path,sources,byname):
    present={}
    with (execdir/'parse_dispatch.csv').open(encoding='utf-8-sig',newline='') as f:
        for r in csv.DictReader(f):present.setdefault(r['language'],set()).add(int(r['parse_state']))
    rows=[]; per={}
    for lang,_,src in sources:
        body=balanced_brace_body(src,r'static const uint16_t\s+ts_parse_table\s*\[[^\]]+\]\s*\[[^\]]+\]\s*=\s*\{')
        if not body:continue
        for sm in re.finditer(r'(?m)^\s*\[(\d+)\]\s*=\s*\{',body):
            st=int(sm.group(1))
            if st in present.get(lang,set()):continue
            i=sm.end();dep=1
            while i<len(body) and dep:
                if body[i]=='{':dep+=1
                elif body[i]=='}':dep-=1
                i+=1
            blk=body[sm.end():i-1]
            for em in re.finditer(r'\[([A-Za-z_][A-Za-z0-9_]*)\]\s*=\s*(STATE|ACTIONS)\((\d+)\)',blk):
                name,kind,val=em.group(1),em.group(2),int(em.group(3)); info=byname.get((lang,name))
                if not info:continue
                sid,sk=info
                rows.append([lang,st,sid,name,sk,'STATE' if kind=='STATE' else 'ACTION',val if kind=='STATE' else '',val if kind=='ACTIONS' else '','large'])
                per[lang]=per.get(lang,0)+1
    with (execdir/'parse_dispatch_supplement.csv').open('w',encoding='utf-8',newline='') as f:
        w=csv.writer(f);w.writerow(['language','parse_state','symbol_id','symbol_name','symbol_kind','dispatch_kind','next_state','action_list_id','table_kind']);w.writerows(rows)
    return len(rows),per

def main():
    ap=argparse.ArgumentParser();ap.add_argument('--raw-parser-root',required=True,type=Path);ap.add_argument('--execution-ready-dir',required=True,type=Path);ap.add_argument('--replace-lex-dispatch',action='store_true')
    a=ap.parse_args();sources=parser_sources(a.raw_parser_root);byname=symbol_maps(a.execution_ready_dir)
    n,changed,out=restore_lex_order(a.execution_ready_dir,sources)
    mode_additions,mode_languages=lex_mode_supplement(a.execution_ready_dir,sources)
    modes,words=reserved_word_supplements(a.execution_ready_dir,sources,byname)
    supplement,per=parse_dispatch_supplement(a.execution_ready_dir,sources,byname)
    if a.replace_lex_dispatch:
        backup=a.execution_ready_dir/'lex_dispatch.pre_source_order.csv'
        if not backup.exists():shutil.copy2(a.execution_ready_dir/'lex_dispatch.csv',backup)
        shutil.copy2(out,a.execution_ready_dir/'lex_dispatch.csv')
    print(f'parser_sources={len(sources)}')
    print(f'lex_rows={n} lex_ordinals_changed={changed}')
    print(f'lex_mode_additions={mode_additions} languages={mode_languages}')
    print(f'reserved_mode_rows={modes} reserved_word_rows={words}')
    print(f'parse_dispatch_supplement_rows={supplement} by_language={per}')

if __name__=='__main__':main()
