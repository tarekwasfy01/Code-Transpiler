package main

import (
 "fmt"
 "path/filepath"
 "sort"
 "strings"
)

// compileGrammarTables materializes a neutral, offline parse-table index. It
// contains no Tree-sitter code and is safe to ship as data with the frontend.
func compileGrammarTables(tsDir, outDir string) error {
 rules,err:=readCSV(filepath.Join(tsDir,"03_rules.csv")); if err!=nil{return err}
 edges,err:=readCSV(filepath.Join(tsDir,"04_rule_symbol_edges.csv")); if err!=nil{return err}
 atoms,err:=readCSV(filepath.Join(tsDir,"02_grammar_atoms.csv")); if err!=nil{return err}
 states:=map[string]string{}; for _,r:=range rules { k:=r["language"]+"\x00"+r["rule"]+"\x00"+r["operators"]; states[k]=fmt.Sprintf("S%06d",len(states)+1) }
 sr:=[][]string{}; keys:=make([]string,0,len(states)); for k:=range states{keys=append(keys,k)}; sort.Strings(keys)
 for _,k:=range keys { p:=strings.Split(k,"\x00"); sr=append(sr,[]string{states[k],p[0],p[1],p[2]}) }
 tr:=[][]string{}; for _,e:=range edges { from:=states[e["language"]+"\x00"+e["from_rule"]+"\x00"] ; if from=="" { for k,v:=range states {if strings.HasPrefix(k,e["language"]+"\x00"+e["from_rule"]+"\x00"){from=v;break}}}; tr=append(tr,[]string{from,e["language"],e["from_rule"],e["to_symbol"],e["path"]}) }
 pr:=[][]string{}; for _,a:=range atoms { pr=append(pr,[]string{a["language"],a["rule"],a["path"],a["op_type"],a["name"],a["value"]}) }
	inv:=[][]string{}; deps:=[][]string{}
	for _,r:=range rules { refs:=strings.Split(r["symbol_refs"],"|"); for _,s:=range refs { if s==""{continue}; kind:="NONTERMINAL"; if !strings.HasPrefix(s,"_") && (len(s)==1 || strings.ContainsAny(s,"()[]{}:;,.'\"+-*/=<>&|")){kind="TERMINAL"}; inv=append(inv,[]string{r["language"],s,kind,r["rule"]}); deps=append(deps,[]string{r["language"],r["rule"],s}) } }
 if err=writeCSV(filepath.Join(outDir,"grammar_production_specs.csv"),[]string{"language","rule","path","operation","name","value"},pr);err!=nil{return err}
 if err=writeCSV(filepath.Join(outDir,"parse_states.csv"),[]string{"state_id","language","rule","operators"},sr);err!=nil{return err}
 if err=writeCSV(filepath.Join(outDir,"parse_transitions.csv"),[]string{"state_id","language","rule","symbol","path"},tr);err!=nil{return err}
	if err=writeCSV(filepath.Join(outDir,"grammar_symbol_inventory.csv"),[]string{"language","symbol","kind","source_rule"},inv);err!=nil{return err}
	if err=writeCSV(filepath.Join(outDir,"rule_dependencies.csv"),[]string{"language","rule","symbol"},deps);err!=nil{return err}
 fmt.Printf("GRAMMAR_PRODUCTIONS=%d\nPARSE_STATES=%d\nPARSE_TRANSITIONS=%d\nLEXICAL_STATES=0\nEXACT_PARSE_STATE_CLASSES=%d\n",len(rules),len(states),len(tr),len(states))
 return nil
}
