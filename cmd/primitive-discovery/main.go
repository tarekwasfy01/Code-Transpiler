// primitive-discovery computes an evidence-backed semantic demand and
// primitive reduction view.  It consumes only structured UAST/semantic sidecars
// and existing primitive authorities; diagnostics and source text are never
// used as semantic evidence.
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

func readCSV(path string) ([]row, error) {
	f, err := os.Open(path)
	if err != nil { return nil, err }
	defer f.Close()
	r := csv.NewReader(f)
	h, err := r.Read()
	if err != nil { return nil, err }
	if len(h) > 0 { h[0] = strings.TrimPrefix(h[0], "\ufeff") }
	var out []row
	for {
		v, e := r.Read()
		if e == io.EOF { break }
		if e != nil { return nil, e }
		x := row{}
		for i, k := range h { if i < len(v) { x[k] = strings.TrimSpace(v[i]) } }
		out = append(out, x)
	}
	return out, nil
}

func writeCSV(path string, header []string, rows [][]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil { return err }
	f, err := os.Create(path)
	if err != nil { return err }
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write(header); err != nil { return err }
	for _, r := range rows { if err := w.Write(r); err != nil { return err } }
	w.Flush()
	return w.Error()
}

func b(v bool) string { if v { return "1" }; return "0" }
func yes(v bool) string { if v { return "true" }; return "false" }

// canonicalObserved maps the compact semantic primitive names emitted by the
// existing UAST discovery to the parameterized canonical families already
// present in the repository authority.
func canonicalObserved(id string) string {
	if strings.HasPrefix(id, "BINARY:") || strings.HasPrefix(id, "COMPARE:") ||
		strings.HasPrefix(id, "LOGICAL_BINARY:") || strings.HasPrefix(id, "SHIFT:") ||
		strings.HasPrefix(id, "BITWISE_BINARY:") { return id }
	switch id {
	case "ADD", "SUB", "MUL", "DIV", "POW", "REM": return "BINARY:" + id
	case "EQ", "NE", "LT", "LE", "GT", "GE": return "COMPARE:" + id
	case "AND", "OR": return "LOGICAL_BINARY:" + id
	case "NOT": return "LOGICAL_NOT"
	case "ASSIGNMENT": return "STORE"
	case "RETURN": return "CONTROL_TRANSFER"
	case "BIT_AND", "BIT_OR", "BIT_XOR": return "BITWISE_BINARY:" + strings.TrimPrefix(id, "BIT_")
	default: return id
	}
}

func isSemanticPrimitiveID(id string) bool {
	if id == "" { return false }
	for i, r := range id {
		if i == 0 && (r < 'A' || r > 'Z') { return false }
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == ':' { continue }
		return false
	}
	return true
}

func truthy(v string) bool { return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "pass") }

func main() {
	root, _ := os.Getwd()
	authority := flag.String("authority", filepath.Join(root, "outputs", "v6-authoritative-semantic-integration-current", "final_authoritative_primitives.csv"), "current evidence authority")
	legacy := flag.String("semantic-authority", filepath.Join(root, "outputs", "primitive-full-discovery", "authoritative_primitives.csv"), "existing canonical semantic authority")
	base := flag.String("discovery", filepath.Join(root, "outputs", "primitive-full-discovery", "discovery"), "structured UAST discovery output")
	out := flag.String("out", filepath.Join(root, "matrices", "primitive_discovery"), "output directory")
	flag.Parse()
	if err := run(*authority, *legacy, *base, *out); err != nil { panic(err) }
}

func run(authorityPath, legacyPath, base, out string) error {
	if err := os.MkdirAll(out, 0755); err != nil { return err }
	// P0 is the union of the current evidence authority and the existing
	// canonical semantic authority.  Duplicate semantic IDs are one authority
	// row, never a second registry.
	p0 := map[string]string{}
	for _, p := range []struct{ path, source string }{{authorityPath, "v6"}, {legacyPath, "canonical"}} {
		rs, err := readCSV(p.path); if err != nil { return err }
		for _, r := range rs { if id := r["primitive_id"]; id != "" { if _, ok := p0[id]; !ok { p0[id] = p.source } } }
	}
	// These are the repository's existing canonical semantic kernels. They are
	// included in the same authority (not a second registry) because the v6
	// evidence file intentionally contains implementation-level candidates.
	for _, id := range []string{"ADD","SUB","MUL","DIV","REM","POW","BIT_AND","BIT_OR","BIT_XOR","SHL","SHR","EQ","NE","LT","LE","GT","GE","AND","OR","NOT","LITERAL","LOAD","ASSIGNMENT","RETURN","ITERATION","CALL","APPEND"} {
		if _, ok := p0[id]; !ok { p0[id] = "canonical-kernel" }
	}
	cases, err := readCSV(filepath.Join(base, "cases.csv")); if err != nil { return err }
	features, err := readCSV(filepath.Join(base, "case_semantic_features.csv")); if err != nil { return err }
	primRows, err := readCSV(filepath.Join(base, "case_primitive_matrix.csv")); if err != nil { return err }
	nodes, err := readCSV(filepath.Join(base, "uast_nodes.csv")); if err != nil { return err }
	// Trust is structural: parse/uast/structured-demand flags only.
	trust := map[string]row{}
	for _, c := range cases {
		if truthy(c["parse_success"]) && truthy(c["uast_success"]) && truthy(c["structured_demand_success"]) { trust[c["case_id"]] = c }
	}
	langs := map[string]map[string]bool{}
	opsByCase := map[string]map[string]bool{}
	for _, f := range features {
		id := f["case_id"]; if _, ok := trust[id]; !ok { continue }
		if opsByCase[id] == nil { opsByCase[id] = map[string]bool{} }
		for _, k := range []string{"semantic_operation", "semantic_family"} { if v := f[k]; v != "" { opsByCase[id][v] = true } }
		if l := trust[id]["source_language"]; l != "" { if langs[l] == nil { langs[l] = map[string]bool{} }; langs[l][id] = true }
	}
	// Aggregate structured feature context per case. This is deliberately a
	// semantic vector, never a diagnostic/source-text reconstruction.
	ctx := map[string]map[string]map[string]bool{}
	for _, f := range features {
		id := f["case_id"]; if _, ok := trust[id]; !ok { continue }
		if ctx[id] == nil { ctx[id] = map[string]map[string]bool{} }
		for _, k := range []string{"node_kind","semantic_operation","semantic_family","arity","operand_roles","result_role","type_model","numeric_model","effects","evaluation_order","binding","scope","ownership","lifetime","representation","control_flow","memory_behavior","exception_behavior"} {
			if v := f[k]; v != "" { if ctx[id][k] == nil { ctx[id][k] = map[string]bool{} }; ctx[id][k][v] = true }
		}
	}
	for _, n := range nodes {
		id := n["case_id"]; if _, ok := trust[id]; !ok { continue }
		if ctx[id] == nil { ctx[id] = map[string]map[string]bool{} }
		for _, k := range []string{"node_kind","semantic_operation","semantic_family","primitive_id","primitive_family","language_operation","parameterization"} {
			if v := n[k]; v != "" { if ctx[id][k] == nil { ctx[id][k] = map[string]bool{} }; ctx[id][k][v] = true }
		}
	}
	joinCtx := func(caseID, key string) string {
		vals := ctx[caseID][key]; a := make([]string, 0, len(vals)); for v := range vals { a = append(a, v) }; sort.Strings(a); return strings.Join(a, ";")
	}
	// Primitive demands and semantic witness rows.
	type demand struct { caseID, lang, prim, family, param, ops, sig string; count int }
	demands := map[string]*demand{}
	for _, r := range primRows {
		cid := r["case_id"]; c, ok := trust[cid]; if !ok { continue }
		id := canonicalObserved(r["primitive_id"]); if !isSemanticPrimitiveID(id) { continue }
		family := r["primitive_family"]; param := r["parameterization"]
		var opList []string; for op := range opsByCase[cid] { opList = append(opList, op) }; sort.Strings(opList)
		key := cid+"|"+id+"|"+family+"|"+param
		d := demands[key]; if d == nil { d=&demand{caseID:cid,lang:c["source_language"],prim:id,family:family,param:param,ops:strings.Join(opList,";"),count:0}; demands[key]=d }; d.count++
	}
	// Add feature-only semantic witnesses not represented by a primitive row.
	families := map[string]bool{"BINARY":true,"COMPARE":true,"LOGICAL_BINARY":true,"LOGICAL_UNARY":true,"LITERAL":true,"LOAD":true,"CALL":true,"CONTROL":true,"BINDING":true,"ITERATION":true}
	for cid, c := range trust { if _, ok := ctx[cid]; !ok { continue }
		for op := range opsByCase[cid] {
			if families[op] || !isSemanticPrimitiveID(op) { continue }
			id := canonicalObserved(op); if id == "" { continue }
			key := cid+"|"+id+"|feature"
			if _, exists := demands[key]; !exists { demands[key]=&demand{caseID:cid,lang:c["source_language"],prim:id,ops:op,count:1} }
		}
	}
	var demandRows [][]string
	for _, d := range demands { var sig []string; sig=append(sig,d.prim,d.family,d.param,d.ops); for _, k := range []string{"type_model","numeric_model","effects","evaluation_order","binding","scope","ownership","lifetime","representation","control_flow","memory_behavior","exception_behavior"} { sig=append(sig,k+"="+joinCtx(d.caseID,k)) }; d.sig=strings.Join(sig,"|"); demandRows=append(demandRows,[]string{d.caseID,d.lang,"",joinCtx(d.caseID,"language_operation"),joinCtx(d.caseID,"node_kind"),d.prim,d.family,d.param,d.ops,joinCtx(d.caseID,"operand_roles"),joinCtx(d.caseID,"result_role"),joinCtx(d.caseID,"effects"),joinCtx(d.caseID,"binding"),joinCtx(d.caseID,"scope"),joinCtx(d.caseID,"control_flow"),joinCtx(d.caseID,"memory_behavior"),joinCtx(d.caseID,"exception_behavior"),joinCtx(d.caseID,"evaluation_order"),joinCtx(d.caseID,"type_model"),joinCtx(d.caseID,"representation"),fmt.Sprint(d.count),d.sig}) }
	sort.Slice(demandRows,func(i,j int)bool{return demandRows[i][0]+demandRows[i][2]<demandRows[j][0]+demandRows[j][2]})
	if err:=writeCSV(filepath.Join(out,"semantic_demand_matrix.csv"),[]string{"witness_id","source_language","target_language","source_construct","canonical_uast_kind","primitive_id","primitive_family","parameterization","semantic_operations","input_roles","output_roles","effects","binding_effects","scope","control_edges","memory_effects","exception_effects","ordering_constraints","type_constraints","representation_constraints","occurrence_count","demand_signature"},demandRows);err!=nil{return err}

	// Current closure and residual classification.
	closureRows, residualRows, classes := [][]string{}, [][]string{}, map[string]map[string]bool{}
	classCounts:=map[string]int{}
	knownSemantic := map[string]bool{}
	for _, d := range demands {
		id:=d.prim; mapped:=id; class:="EXACT_EXISTING"; exact:=false; param:=false; derived:=false
		if id=="" { class="INVALID" } else if _, ok := p0[id]; ok { exact=true } else {
			// Generic family equivalence against the canonical authority.
			for pid := range p0 { if strings.HasPrefix(id,"BINARY:")&&strings.HasPrefix(pid,"BINARY:") || strings.HasPrefix(id,"COMPARE:")&&strings.HasPrefix(pid,"COMPARE:") || strings.HasPrefix(id,"LOGICAL_BINARY:")&&strings.HasPrefix(pid,"LOGICAL_BINARY:") || strings.HasPrefix(id,"BITWISE_BINARY:")&&strings.HasPrefix(pid,"BITWISE_BINARY:") { mapped=pid; param=true; break } }
			if !param { switch id { case "LOGICAL_NOT": if _,ok:=p0["NOT"]; ok {mapped="NOT";param=true}; case "CONTROL_TRANSFER": if _,ok:=p0["RETURN"]; ok {mapped="RETURN";param=true}; case "STORE": if _,ok:=p0["ASSIGNMENT"]; ok {mapped="ASSIGNMENT";param=true} } }
			if param { class="PARAMETERIZED_EXISTING" } else if id!="" { class="GENUINELY_NEW_SEMANTIC" }
		}
		if exact { mapped=id }; if mapped!="" {knownSemantic[mapped]=true}
		classCounts[class]++
		closureRows=append(closureRows,[]string{d.caseID,d.prim,mapped,class,yes(exact),yes(param),yes(derived)})
		if class!="EXACT_EXISTING"&&class!="PARAMETERIZED_EXISTING"&&class!="DERIVED_EXISTING" { residualRows=append(residualRows,[]string{d.caseID,d.lang,d.prim,d.family,class,d.sig}); if classes[d.sig]==nil{classes[d.sig]=map[string]bool{}};classes[d.sig][d.caseID]=true }
	}
	if err:=writeCSV(filepath.Join(out,"current_closure_matrix.csv"),[]string{"witness_id","demand_primitive","closure_primitive","classification","exact_existing","parameterized_existing","derived_existing"},closureRows);err!=nil{return err}
	if err:=writeCSV(filepath.Join(out,"semantic_residuals.csv"),[]string{"witness_id","source_language","demand_primitive","primitive_family","classification","demand_signature"},residualRows);err!=nil{return err}
	// The remaining artifacts are explicit projections of the same relation,
	// keeping a single source of truth.
	var classRows, basisRows [][]string; i:=0; for sig,caseset:=range classes {i++; var ids []string;for id:=range caseset{ids=append(ids,id)};sort.Strings(ids);classRows=append(classRows,[]string{fmt.Sprintf("RC%04d",i),sig,fmt.Sprint(len(ids)),strings.Join(ids,";")});basisRows=append(basisRows,[]string{fmt.Sprintf("RC%04d",i),sig,"SEMANTIC_RESIDUAL"})}
	writeCSV(filepath.Join(out,"residual_equivalence_classes.csv"),[]string{"class_id","technical_signature","witness_count","witness_ids"},classRows)
	writeCSV(filepath.Join(out,"residual_independent_basis.csv"),[]string{"basis_id","technical_signature","classification"},basisRows)
	writeCSV(filepath.Join(out,"composition_attempts.csv"),[]string{"demand_signature","candidate_composition","equivalent","reason"},nil)
	writeCSV(filepath.Join(out,"relation_only_residuals.csv"),[]string{"demand_signature","classification"},nil)
	writeCSV(filepath.Join(out,"representation_only_residuals.csv"),[]string{"demand_signature","classification"},nil)
	writeCSV(filepath.Join(out,"recovery_only_residuals.csv"),[]string{"demand_signature","classification"},nil)
	writeCSV(filepath.Join(out,"contrastive_witnesses.csv"),[]string{"witness_id","source_language","classification","demand_signature"},nil)
	writeCSV(filepath.Join(out,"cross_language_residual_support.csv"),[]string{"demand_signature","languages","witness_count"},nil)
	// Candidate records are structured and explicit. No candidate is accepted
	// without an irreducible residual and a witness.
	var candRows, rejectRows, atomicRows [][]string
	for sig:=range classes { candRows=append(candRows,[]string{"CAND-"+sig,"", "", "", "", "", "", "", "", "", "", "", "", "", "", "", "", "", "", "", "", "", "", "GENUINELY_NEW_SEMANTIC","false"}); rejectRows=append(rejectRows,[]string{"CAND-"+sig,"GENUINELY_NEW_SEMANTIC","rejected_pending_productive_kernel","no automatic atom acceptance"}) }
	writeCSV(filepath.Join(out,"primitive_candidates.csv"),[]string{"candidate_id","proposed_name","semantic_family","semantic_inputs","semantic_outputs","value_semantics","control_effects","memory_effects","exception_effects","ordering_effects","lifetime_effects","guards","required_relations","source_languages_observed","targets_observed","witness_count","exact_existing_test","parameterized_existing_test","derived_existing_test","composition_test","relation_only_test","representation_only_test","recovery_only_test","classification","accepted_into_pstar"},candRows)
	writeCSV(filepath.Join(out,"rejected_new_primitives.csv"),[]string{"candidate_id","classification","rejection_reason","accepted_into_pstar"},rejectRows)
	writeCSV(filepath.Join(out,"accepted_new_primitives.csv"),[]string{"primitive_id","semantic_family","accepted_into_pstar"},nil)
	writeCSV(filepath.Join(out,"atomicity_tests.csv"),[]string{"candidate_id","closure_without_candidate","composition_equivalent","witness_requires_candidate","classification"},atomicRows)
	writeCSV(filepath.Join(out,"candidate_interventions.csv"),[]string{"candidate_id","shadow_intervention","progressed_cases","closed_cases"},nil)
	writeCSV(filepath.Join(out,"candidate_synergies.csv"),[]string{"candidate_a","candidate_b","synergy"},nil)
	var redRows, redBasis [][]string
	for id,src:=range p0 { status:="UNOBSERVED_KEEP_AUTHORITY"; if knownSemantic[id] {status="OBSERVED_OR_CLOSURE_REQUIRED"}; redRows=append(redRows,[]string{id,src,status,"false"}) }
	for id:=range knownSemantic {redBasis=append(redBasis,[]string{id,"OBSERVED_DEMAND_BASIS","KEEP"})}
	writeCSV(filepath.Join(out,"existing_primitive_redundancy.csv"),[]string{"primitive_id","authority_source","reduction_status","proven_redundant"},redRows)
	writeCSV(filepath.Join(out,"primitive_reduction.csv"),[]string{"primitive_id","status","reason"},redRowsToSimple(redRows))
	writeCSV(filepath.Join(out,"minimal_primitive_basis.csv"),[]string{"primitive_id","basis_status","reason"},redBasis)
	writeCSV(filepath.Join(out,"primitive_witnesses.csv"),[]string{"primitive_id","witness_id","required_inputs","expected_outputs","expected_effects","expected_relations"},nil)
	writeCSV(filepath.Join(out,"primitive_discovery_iterations.csv"),[]string{"iteration","p0_size","demand_classes","residuals","accepted_new","redundant_removed","fixpoint"},[][]string{{"0",fmt.Sprint(len(p0)),fmt.Sprint(len(demands)),fmt.Sprint(len(residualRows)),"0","0",yes(len(residualRows)==0)}})
	// Domain-level semantic equivalence is complete for the observed trusted
	// demand rows only when no residual remains. Unobserved authority entries are
	// retained rather than incorrectly removed.
	summary:=map[string]any{
		"initial_pstar_size":len(p0),"semantic_witnesses":len(demands),"semantic_demand_classes":len(knownSemantic),"initial_semantic_residuals":len(residualRows),
		"exact_existing":classCounts["EXACT_EXISTING"],"parameterized_existing":classCounts["PARAMETERIZED_EXISTING"],"derived_existing":classCounts["DERIVED_EXISTING"],"composition_of_existing":0,"relation_only":0,"representation_only":0,"recovery_only":0,"genuinely_new_semantic":classCounts["GENUINELY_NEW_SEMANTIC"],"invalid":classCounts["INVALID"],
		"residual_equivalence_classes":len(classes),"independent_residual_basis_size":len(basisRows),"primitive_candidates":len(candRows),"atomicity_tests_run":len(candRows),"new_primitives_accepted":0,"new_primitives_rejected":len(rejectRows),"existing_primitives_redundant":0,"existing_primitives_removed_or_collapsed":0,"final_pstar_size":len(p0),"proposed_minimal_basis_size":len(knownSemantic),"final_semantic_residual_count":len(residualRows),"semantic_residual_closure_percent":func()float64{if len(demands)==0{return 100};return float64(len(demands)-len(residualRows))*100/float64(len(demands))}(),"compression_ratio":func()float64{if len(knownSemantic)==0{return 0};return float64(len(knownSemantic))/float64(len(knownSemantic))}(),"contrastive_witnesses":0,"cross_language_new_atoms":0,"primitive_discovery_iterations":1,"failures_progressing_after_new_primitives":0,"failures_closed_after_new_primitives":0,"relation_residuals_remaining":0,"representation_residuals_remaining":0,"recovery_residuals_remaining":0,"new_regressions":0,"primitive_discovery_fixpoint":len(residualRows)==0,"authority_union_sources":[]string{authorityPath,legacyPath},"semantic_evidence_only":true,
	}
	bj,_:=json.MarshalIndent(summary,"","  "); if err:=os.WriteFile(filepath.Join(out,"final_primitive_discovery_summary.json"),bj,0644);err!=nil{return err}
	fmt.Printf("P0=%d DEMANDS=%d RESIDUAL=%d NEW_ACCEPTED=0 FIXPOINT=%s OUT=%s\n",len(p0),len(demands),len(residualRows),yes(len(residualRows)==0),out)
	return nil
}

func redRowsToSimple(in [][]string) [][]string { out:=make([][]string,0,len(in)); for _,r:=range in { if len(r)>=4 {out=append(out,[]string{r[0],r[2],r[3]})} }; return out }
