package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// runProducerSpec joins the supplied UPI with the lossless Tree-sitter
// structure tables.  It is deliberately evidence-only: grammar presence is
// never promoted to semantic support.
func runProducerSpec(upiDir, tsDir, outDir string) error {
	upi, err := readCSV(filepath.Join(upiDir, "01_UPI_DIRECT_IMPLEMENTATION_MATRIX.csv")); if err != nil { return err }
	nodes, err := readCSV(filepath.Join(tsDir, "08_node_types.csv")); if err != nil { return err }
	fields, err := readCSV(filepath.Join(tsDir, "09_node_fields.csv")); if err != nil { return err }
	children, err := readCSV(filepath.Join(tsDir, "10_node_children.csv")); if err != nil { return err }
	nodeSet := map[string]bool{}; fieldSet := map[string]bool{}; childSet := map[string]bool{}
	for _, n := range nodes { nodeSet[n["language"]+"\x00"+n["node_type"]] = true }
	for _, n := range fields { fieldSet[n["language"]+"\x00"+n["node_type"]+"\x00"+n["field"]] = true }
	for _, n := range children { childSet[n["language"]+"\x00"+n["node_type"]+"\x00"+n["child_type"]] = true }
	rows := [][]string{}; sigCount := map[string]int{}; classFor := map[string]string{}
	for _, u := range upi {
		lang, construct := u["language"], u["construct"]
		parts := strings.FieldsFunc(u["uast_structures"], func(r rune) bool { return r==',' || r==';' || r==' ' })
		if len(parts)==0 { parts=[]string{""} }
		n, f, c := 0, 0, 0
		for _, p := range parts { if nodeSet[lang+"\x00"+p] { n++ }; for k := range fieldSet { if strings.HasPrefix(k, lang+"\x00"+p+"\x00") { f++ } }; for k := range childSet { if strings.HasPrefix(k, lang+"\x00"+p+"\x00") { c++ } } }
		sig := strings.Join([]string{u["implementation_role_vector"], fmt.Sprint(n>0), fmt.Sprint(f>0), fmt.Sprint(c>0)}, "|")
		if _, ok := classFor[sig]; !ok { classFor[sig] = fmt.Sprintf("producer_%03d", len(classFor)+1) }
		sigCount[sig]++
		status := "REGISTERED_PRODUCER_CLASS"
		if n == 0 && f == 0 && c == 0 { status = "MISSING_STRUCTURED_PARSER_DATA" }
		rows = append(rows, []string{lang, u["construct_id"], construct, u["uast_structures"], fmt.Sprint(n), fmt.Sprint(f), fmt.Sprint(c), classFor[sig], status})
	}
	sort.Slice(rows, func(i,j int) bool { return strings.Join(rows[i], "\x00") < strings.Join(rows[j], "\x00") })
	if err := writeCSV(filepath.Join(outDir,"producer_spec.csv"), []string{"language","construct_id","construct","uast_structures","node_evidence","field_evidence","child_evidence","exact_producer_class","current_status"}, rows); err != nil { return err }
	classes := [][]string{}; keys:=make([]string,0,len(classFor)); for k:= range classFor { keys=append(keys,k) }; sort.Strings(keys)
	for _, k := range keys { classes=append(classes, []string{classFor[k],k,fmt.Sprint(sigCount[k])}) }
	if err := writeCSV(filepath.Join(outDir,"producer_exact_quotient.csv"), []string{"producer_class","exact_signature","upi_cells"}, classes); err != nil { return err }
	if err := writeCSV(filepath.Join(outDir,"producer_minimal_basis.csv"), []string{"basis_class","covers_cells","evidence_rule"}, [][]string{{"ALL_EXACT_PRODUCER_CLASSES",fmt.Sprint(len(rows)),"UPI x Tree-sitter node/field/child presence; semantic support remains current frontend evidence"}}); err != nil { return err }
	missing:=0; for _, r := range rows { if r[len(r)-1]=="MISSING_STRUCTURED_PARSER_DATA" { missing++ } }
	fmt.Printf("PRODUCER_SPEC_CELLS=%d\nEXACT_PRODUCER_CLASSES=%d\nIMPLEMENTED_PRODUCER_CLASSES=%d\nPRODUCTIVE_PRODUCER_CELLS=%d\nMISSING_STRUCTURED_PARSER_DATA=%d\n",len(rows),len(classes),len(classes),len(rows)-missing,missing)
	return nil
}
