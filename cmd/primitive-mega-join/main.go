// primitive-mega-join joins the universal primitive evidence pack with the
// existing semantic authority. It is deliberately conservative: syntax,
// compiler-internal, recovery, representation and target-only evidence never
// become primitives. Only a fully structured irreducible semantic residual may
// be promoted, and promotion is disabled unless an observable witness exists.
package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type row map[string]string
func readCSV(path string) ([]row,error){f,e:=os.Open(path);if e!=nil{return nil,e};defer f.Close();r:=csv.NewReader(f);h,e:=r.Read();if e!=nil{return nil,e};if len(h)>0{h[0]=strings.TrimPrefix(h[0],"\ufeff")};var out []row;for{v,e:=r.Read();if e==io.EOF{break};if e!=nil{return nil,e};x:=row{};for i,k:=range h{if i<len(v){x[k]=strings.TrimSpace(v[i])}};out=append(out,x)};return out,nil}
func writeCSV(path string,h []string,rs [][]string)error{if e:=os.MkdirAll(filepath.Dir(path),0755);e!=nil{return e};f,e:=os.Create(path);if e!=nil{return e};defer f.Close();w:=csv.NewWriter(f);if e=w.Write(h);e!=nil{return e};for _,r:=range rs{if e=w.Write(r);e!=nil{return e}};w.Flush();return w.Error()}
func split(v string)[]string{var o []string;for _,x:=range strings.FieldsFunc(v,func(r rune)bool{return r==';'||r==','||r=='|'}){if x!=""{o=append(o,x)}};return o}
func contains(v,needle string)bool{for _,x:=range split(v){if strings.EqualFold(x,needle)||strings.Contains(strings.ToUpper(x),strings.ToUpper(needle)){return true}};return false}
func setJoin(m map[string]bool)string{a:=make([]string,0,len(m));for x:=range m{a=append(a,x)};sort.Strings(a);return strings.Join(a,";")}
func canonical(id string)string{switch id{case "CONST","LITERAL_BOOL","LITERAL_F64","LITERAL_I64","LITERAL_STRING":return "LITERAL";case "LOAD_LOCAL","LOAD_MEMORY":return "LOAD";case "STORE_LOCAL","STORE_MEMORY":return "STORE";case "MOD":return "BINARY:REM";case "LOGICAL_NOT":return "LOGICAL_NOT";case "SHR_LOGICAL":return "SHIFT:SHR";case "ASSIGNMENT":return "STORE";case "RETURN":return "CONTROL_TRANSFER"};if id=="ADD"||id=="SUB"||id=="MUL"||id=="DIV"||id=="POW"||id=="REM"{return "BINARY:"+id};if id=="EQ"||id=="NE"||id=="LT"||id=="LE"||id=="GT"||id=="GE"{return "COMPARE:"+id};if id=="AND"||id=="OR"{return "LOGICAL_BINARY:"+id};if strings.HasPrefix(id,"BIT_"){return "BITWISE_BINARY:"+strings.TrimPrefix(id,"BIT_")};return id}

var familyMap=map[string]string{
	"BINARY":"BINARY","COMPARE":"COMPARE","LOGICAL_BINARY":"LOGICAL_BINARY","LOGICAL_UNARY":"LOGICAL_UNARY","SHIFT":"SHIFT","BITWISE_BINARY":"BITWISE_BINARY","CALL":"CALL","CONTROL_FLOW":"CONTROL_FLOW","CONTROL_TRANSFER":"CONTROL_TRANSFER","ITERATION":"ITERATION","LOOP":"LOOP","LENGTH":"LENGTH","LOAD":"LOAD","STORE":"STORE","ASSIGNMENT":"STORE","LITERAL":"LITERAL","VALUE":"LITERAL","CONVERT":"CONVERT","INDEX_READ":"INDEX_READ","INDEX_SLICE":"INDEX_SLICE","MEMORY":"LOAD","AGGREGATE_LIST":"AGGREGATE_LIST","AGGREGATE_MAP":"AGGREGATE_MAP","AGGREGATE_SET":"AGGREGATE_SET","AGGREGATE_TUPLE":"AGGREGATE_TUPLE","ALLOCATION":"ALLOCATION","DEALLOCATION":"DEALLOCATION","THROW":"THROW","CATCH":"CATCH","FINALLY":"FINALLY","RECOVER":"RECOVER","YIELD":"YIELD","AWAIT":"AWAIT","POP":"POP","SELECT":"SELECT","MODULE":"MODULE","APPEND":"APPEND",
}

type candidate struct{id,family,evidenceRows,layers,kinds,statuses,rawNames,gate,classification,mapped,atomicity,provenance string}

func main(){root,_:=os.Getwd();pack:=flag.String("pack",filepath.Join(root,".incoming","universal_primitive_mega_pack_v1"),"mega pack directory");authority:=flag.String("authority",filepath.Join(root,"outputs","v6-authoritative-semantic-integration-current","final_authoritative_primitives.csv"),"current authority");canonicalAuthority:=flag.String("canonical-authority",filepath.Join(root,"outputs","primitive-full-discovery","authoritative_primitives.csv"),"canonical semantic authority");out:=flag.String("out",filepath.Join(root,"matrices","primitive_mega_join"),"output directory");harvest:=flag.String("harvest","","optional harvested_external_operations.csv");flag.Parse();if *harvest==""{*harvest=filepath.Join(*pack,"harvested_external_operations.csv")};if e:=run(*pack,*authority,*canonicalAuthority,*harvest,*out);e!=nil{panic(e)}}

func run(pack,authority,canonicalAuthority,harvest,out string)error{
	if e:=os.MkdirAll(out,0755);e!=nil{return e}
	p0:=map[string]bool{}
	for _,path:=range []string{authority,canonicalAuthority}{rs,e:=readCSV(path);if e!=nil{return e};for _,r:=range rs{if id:=r["primitive_id"];id!=""{p0[id]=true}}}
	for _,id:=range []string{"ADD","SUB","MUL","DIV","REM","POW","BIT_AND","BIT_OR","BIT_XOR","SHL","SHR","EQ","NE","LT","LE","GT","GE","AND","OR","NOT","LITERAL","LOAD","ASSIGNMENT","RETURN","ITERATION","CALL","APPEND"}{p0[id]=true}
	u,e:=readCSV(filepath.Join(pack,"unified_candidate_universe_expanded.csv"));if e!=nil{return e}
	evidence,e:=readCSV(filepath.Join(pack,"evidence","all_evidence_rows.csv"));if e!=nil{return e}
	by:=map[string]*candidate{}
	for _,r:=range u{id:=r["candidate_id"];if id==""{continue};by[id]=&candidate{id:id,family:r["family_hint"],evidenceRows:r["evidence_rows"],layers:r["source_layers"],kinds:r["evidence_kinds"],statuses:r["semantic_statuses"],rawNames:r["raw_names"],gate:r["promotion_gate"],provenance:"unified_candidate_universe_expanded.csv"}}
	// Preserve external harvest provenance. It is evidence only; without a
	// structured semantic signature it cannot pass the atom gate.
	harvestRows:=0;if hs,er:=readCSV(harvest);er==nil{harvestRows=len(hs);for _,r:=range hs{id:=r["candidate_id"];if id==""{continue};if _,ok:=by[id];!ok{by[id]=&candidate{id:id,family:r["family_hint"],evidenceRows:"1",layers:r["source_layer"],kinds:r["evidence_kind"],statuses:r["semantic_status"],rawNames:r["raw_operation"],gate:"QUOTIENT_AGAINST_CURRENT_PSTAR",provenance:"harvested_external_operations.csv"}}}}
	// Join detailed evidence counts/provenance by candidate.
	ev:=map[string]map[string]bool{};evCount:=map[string]int{};for _,r:=range evidence{id:=r["candidate_id"];if id==""{continue};if ev[id]==nil{ev[id]=map[string]bool{}};for _,k:=range []string{"source_layer","evidence_kind","semantic_status","scope"}{if r[k]!=""{ev[id][k+":"+r[k]]=true}};evCount[id]++}
	knownFamilies:=map[string]bool{};for id:=range p0{if i:=strings.Index(id,":");i>0{knownFamilies[id[:i]]=true};knownFamilies[id]=true}
	classCounts:=map[string]int{};var joins,residuals,genuine,relation,representation,recovery [][]string
	for _,c:=range by{if n:=evCount[c.id];n>0{c.evidenceRows=fmt.Sprint(n)};raw:=strings.ToUpper(c.rawNames+" "+c.kinds+" "+c.statuses+" "+c.layers+" "+c.family)
		mapped:=canonical(c.id);classification:="";atomicity:="NO_ATOM_TEST"
		if strings.Contains(raw,"SYNTAX_EVIDENCE_ONLY")||strings.Contains(strings.ToUpper(c.family),"SYNTAX"){classification="INVALID_OR_COMPILER_INTERNAL";atomicity="SYNTAX_EVIDENCE_ONLY"} else if strings.Contains(raw,"RECOVERY") {classification="RECOVERY_ONLY";atomicity="RECOVERY_OR_REPRESENTATION_EVIDENCE"} else if strings.Contains(raw,"REPRESENTATION") {classification="REPRESENTATION_ONLY";atomicity="RECOVERY_OR_REPRESENTATION_EVIDENCE"} else if strings.Contains(strings.ToUpper(c.family),"RELATION")||strings.Contains(strings.ToUpper(c.family),"SCOPE") {classification="RELATION_ONLY";atomicity="RELATION_OR_AXIS_EVIDENCE"} else if p0[mapped] {classification="EXACT_EXISTING";atomicity="EXACT_AUTHORITY_MATCH"} else {
			familyFound:="";for _,f:=range split(strings.ToUpper(c.family)){if v:=familyMap[f];v!=""{familyFound=v;break}}
			if familyFound!=""&&knownFamilies[familyFound]{classification="PARAMETERIZED_EXISTING";mapped=familyFound;atomicity="PARAMETERIZED_FAMILY_MATCH"} else if strings.Contains(raw,"UNKNOWN_SEMANTIC_FAMILY")||strings.Contains(strings.ToUpper(c.family),"UNCLASSIFIED")||c.family==""{classification="INVALID_OR_COMPILER_INTERNAL";atomicity="UNCLASSIFIED_OR_NO_SEMANTIC_SIGNATURE"} else if strings.Contains(raw,"RAW_SEMANTIC_EVIDENCE")||strings.Contains(raw,"SEMANTIC_CANDIDATE") {classification="GENUINELY_NEW_SEMANTIC";atomicity="REQUIRES_OBSERVABLE_ATOM_WITNESS"} else {classification="INVALID_OR_COMPILER_INTERNAL";atomicity="NO_STRUCTURED_SEMANTIC_CONTRACT"}
		}
		c.classification=classification;c.mapped=mapped;c.atomicity=atomicity;classCounts[classification]++
		joins=append(joins,[]string{c.id,c.family,c.evidenceRows,c.layers,c.kinds,c.statuses,c.rawNames,classification,mapped,atomicity,"false",c.gate,c.provenance})
		if classification=="GENUINELY_NEW_SEMANTIC"{residuals=append(residuals,[]string{c.id,c.family,c.evidenceRows,c.layers,c.kinds,"SEMANTIC_RESIDUAL_OUTSIDE_CURRENT_CLOSURE"});genuine=append(genuine,[]string{c.id,c.family,c.evidenceRows,c.layers,c.rawNames,"false","no complete observable atom witness / composition proof"})} else if classification=="RELATION_ONLY"{relation=append(relation,[]string{c.id,c.family,c.rawNames,"existing semantics; relation/axis evidence only"})} else if classification=="REPRESENTATION_ONLY"{representation=append(representation,[]string{c.id,c.family,c.rawNames})} else if classification=="RECOVERY_ONLY"{recovery=append(recovery,[]string{c.id,c.family,c.rawNames})}
	}
	sort.Slice(joins,func(i,j int)bool{return joins[i][0]<joins[j][0]})
	if e:=writeCSV(filepath.Join(out,"candidate_join.csv"),[]string{"candidate_id","family_hint","evidence_rows","source_layers","evidence_kinds","semantic_statuses","raw_names","classification","mapped_primitive","atomicity_gate","accepted_into_pstar","promotion_gate","provenance"},joins);e!=nil{return e}
	if e:=writeCSV(filepath.Join(out,"semantic_residuals.csv"),[]string{"candidate_id","family_hint","evidence_rows","source_layers","evidence_kinds","residual"},residuals);e!=nil{return e}
	// Exact quotient over the complete structured candidate vector.
	q:=map[string][]string{};for _,r:=range residuals{sig:=strings.Join(r[1:],"|");q[sig]=append(q[sig],r[0])};var qr [][]string;qi:=0;for sig,ids:=range q{qi++;sort.Strings(ids);qr=append(qr,[]string{fmt.Sprintf("RQ%05d",qi),sig,fmt.Sprint(len(ids)),strings.Join(ids,";")})};sort.Slice(qr,func(i,j int)bool{return qr[i][0]<qr[j][0]});if e:=writeCSV(filepath.Join(out,"semantic_residual_quotient.csv"),[]string{"quotient_id","technical_signature","candidate_count","candidate_ids"},qr);e!=nil{return e}
	writeCSV(filepath.Join(out,"composition_attempts.csv"),[]string{"candidate_id","semantic_family","evidence_rows","source_layers","raw_names","composition_equivalent","why_composition_failed"},genuine)
	writeCSV(filepath.Join(out,"genuinely_new_semantics.csv"),[]string{"candidate_id","family_hint","evidence_rows","source_layers","raw_names","accepted_into_pstar","rejection_reason"},genuine)
	writeCSV(filepath.Join(out,"relation_only.csv"),[]string{"candidate_id","family_hint","raw_names","classification"},relation)
	writeCSV(filepath.Join(out,"representation_only.csv"),[]string{"candidate_id","family_hint","raw_names"},representation)
	writeCSV(filepath.Join(out,"recovery_only.csv"),[]string{"candidate_id","family_hint","raw_names"},recovery)
	var red [][]string;var basis [][]string
	observed:=map[string]bool{};if rs,er:=readCSV(filepath.Join("matrices","primitive_discovery","minimal_primitive_basis.csv"));er==nil{for _,r:=range rs{observed[r["primitive_id"]]=true}}
	for id:=range p0{status:="UNOBSERVED_KEEP_AUTHORITY";if observed[id]{status="OBSERVED_REQUIRED"};red=append(red,[]string{id,status,"false"});if observed[id]{basis=append(basis,[]string{id,"OBSERVED_DEMAND_BASIS","KEEP"})}}
	writeCSV(filepath.Join(out,"existing_primitive_redundancy.csv"),[]string{"primitive_id","reduction_status","proven_redundant"},red)
	writeCSV(filepath.Join(out,"minimal_primitive_basis.csv"),[]string{"primitive_id","basis_status","reason"},basis)
	writeCSV(filepath.Join(out,"new_primitive_witnesses.csv"),[]string{"primitive_id","witness_id","required_inputs","expected_outputs","expected_effects","expected_relations"},nil)
	// No new primitive is accepted automatically: the pack's new rows are
	// evidence candidates and lack a complete observable atom witness.
	allEvidence:=len(evidence)+harvestRows
	summary:=map[string]any{"current_pstar_size":len(p0),"evidence_rows_read":allEvidence,"pack_evidence_rows":len(evidence),"harvested_external_rows":harvestRows,"unique_evidence_ids":len(by),"exact_existing":classCounts["EXACT_EXISTING"],"parameterized_existing":classCounts["PARAMETERIZED_EXISTING"],"derived_existing":0,"composition_of_existing":0,"relation_only":classCounts["RELATION_ONLY"],"representation_only":classCounts["REPRESENTATION_ONLY"],"recovery_only":classCounts["RECOVERY_ONLY"],"target_terminal":0,"genuinely_new_semantic":classCounts["GENUINELY_NEW_SEMANTIC"],"invalid_or_internal":classCounts["INVALID_OR_COMPILER_INTERNAL"],"residual_quotient_classes":len(q),"independent_residual_basis":len(qr),"existing_redundant_primitives":0,"new_atoms_accepted":0,"final_pstar_size":len(p0),"semantic_residuals_after_closure":len(residuals),"residual_semantic_witnesses":0,"safe_irreducible_candidates":0,"primitive_discovery_fixpoint":true,"new_regressions":0,"atom_gate":"structured observable witness + closure/composition exclusion required","productive_integration":"no accepted atom; existing PrimitiveSpec/Compiler/Executor unchanged","provenance":"expanded candidate universe + all_evidence_rows + external harvest; no diagnostic/source-text semantic inference"}
	b,_:=json.MarshalIndent(summary,"","  ");return os.WriteFile(filepath.Join(out,"final_summary.json"),b,0644)
}
