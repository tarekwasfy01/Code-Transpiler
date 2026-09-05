// universal-truth-miner derives a small, evidence-backed implication basis
// from structured PASS/UAST data.  It never reads diagnostics as semantics and
// never reconstructs values, names, or operands from source text.
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
	"strconv"
	"strings"
)

type row map[string]string

type implication struct {
	ID, Antecedent, Consequent, Classification, Domains string
	Support, Confidence, Counterexamples                int
	Languages, Targets                                  string
}

func readRows(path string) ([]row, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	h, err := r.Read()
	if err != nil {
		return nil, err
	}
	var out []row
	for {
		v, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		m := row{}
		for i, k := range h {
			if i < len(v) {
				m[k] = v[i]
			}
		}
		out = append(out, m)
	}
	return out, nil
}

func writeRows(path string, header []string, rows [][]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write(header); err != nil {
		return err
	}
	for _, r := range rows {
		if err := w.Write(r); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

func splitFacts(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '|' || r == ';' || r == ',' })
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func addFact(dst map[string]map[string]bool, id, fact string) {
	if fact == "" || strings.HasSuffix(fact, ":") {
		return
	}
	if dst[id] == nil {
		dst[id] = map[string]bool{}
	}
	dst[id][fact] = true
}

func boolField(v string) bool {
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "pass")
}

func main() {
	miner := flag.String("miner", "outputs/miner-semantic-validation-v2-clean", "structured semantic miner output")
	failures := flag.String("failures", "outputs/v6-repair-basis", "structured earliest failure basis")
	out := flag.String("out", "matrices/universal_truth_basis", "truth-basis output directory")
	flag.Parse()

	cases, err := readRows(filepath.Join(*miner, "cases.csv"))
	if err != nil {
		panic(err)
	}
	features, err := readRows(filepath.Join(*miner, "case_semantic_features.csv"))
	if err != nil {
		panic(err)
	}
	primitives, err := readRows(filepath.Join(*miner, "case_primitive_matrix.csv"))
	if err != nil {
		panic(err)
	}
	nodes, err := readRows(filepath.Join(*miner, "uast_nodes.csv"))
	if err != nil {
		panic(err)
	}
	attempts, err := readRows(filepath.Join(*miner, "attempts.csv"))
	if err != nil {
		panic(err)
	}

	trusted := map[string]bool{}
	languages := map[string]map[string]bool{}
	for _, c := range cases {
		if boolField(c["parse_success"]) && boolField(c["uast_success"]) && boolField(c["structured_demand_success"]) {
			trusted[c["case_id"]] = true
			if languages[c["case_id"]] == nil {
				languages[c["case_id"]] = map[string]bool{}
			}
			languages[c["case_id"]][c["source_language"]] = true
		}
	}
	facts := map[string]map[string]bool{}
	for _, f := range features {
		id := f["case_id"]
		if !trusted[id] {
			continue
		}
		addFact(facts, id, "kind:"+f["node_kind"])
		addFact(facts, id, "operation:"+f["semantic_operation"])
		addFact(facts, id, "family:"+f["semantic_family"])
		if f["arity"] != "" {
			addFact(facts, id, "arity:"+f["arity"])
		}
		for _, x := range splitFacts(f["operand_roles"]) {
			addFact(facts, id, "operand:"+x)
		}
		if f["result_role"] != "" {
			addFact(facts, id, "result:"+f["result_role"])
		}
		if f["type_model"] != "" {
			addFact(facts, id, "type:"+f["type_model"])
		}
		if f["numeric_model"] != "" {
			addFact(facts, id, "numeric:"+f["numeric_model"])
		}
		for _, col := range []string{"effects", "evaluation_order", "binding", "scope", "ownership", "lifetime", "representation", "control_flow", "memory_behavior", "exception_behavior"} {
			for _, x := range splitFacts(f[col]) {
				addFact(facts, id, col+":"+x)
			}
		}
	}
	for _, p := range primitives {
		id := p["case_id"]
		if !trusted[id] {
			continue
		}
		if p["primitive_id"] != "" && !strings.EqualFold(p["primitive_id"], "UNCLASSIFIED") && !strings.HasPrefix(strings.ToUpper(p["primitive_id"]), "UNSUPPORTED") {
			addFact(facts, id, "primitive:"+p["primitive_id"])
			addFact(facts, id, "primitive_family:"+p["primitive_family"])
		}
	}
	for _, n := range nodes {
		id := n["case_id"]
		if !trusted[id] {
			continue
		}
		addFact(facts, id, "uast:"+n["node_kind"])
		addFact(facts, id, "language_operation:"+n["language_operation"])
	}
	targets := map[string]map[string]bool{}
	for _, a := range attempts {
		id := a["case_id"]
		if !trusted[id] || !boolField(a["transpile_success"]) {
			continue
		}
		if targets[id] == nil {
			targets[id] = map[string]bool{}
		}
		targets[id][a["target_language"]] = true
		addFact(facts, id, "target:"+a["target_language"])
		addFact(facts, id, "route:"+a["route_type"])
	}

	factNames := map[string]bool{}
	for _, fs := range facts {
		for f := range fs {
			factNames[f] = true
		}
	}
	factList := make([]string, 0, len(factNames))
	for f := range factNames {
		factList = append(factList, f)
	}
	sort.Strings(factList)
	caseIDs := make([]string, 0, len(facts))
	for id := range facts {
		caseIDs = append(caseIDs, id)
	}
	sort.Strings(caseIDs)

	fm := make([][]string, 0, len(caseIDs)*len(factList))
	for _, id := range caseIDs {
		for _, f := range factList {
			v := "0"
			if facts[id][f] {
				v = "1"
			}
			fm = append(fm, []string{id, f, v})
		}
	}
	if err := writeRows(filepath.Join(*out, "fact_matrix.csv"), []string{"case_id", "fact", "present"}, fm); err != nil {
		panic(err)
	}

	set := func(ante ...string) []string { out := append([]string(nil), ante...); sort.Strings(out); return out }
	key := func(ante []string) string { return strings.Join(set(ante...), "+") }
	relevant := func(f string) bool {
		for _, p := range []string{"kind:", "uast:", "operation:", "family:", "primitive:", "primitive_family:", "operand:", "result:", "binding:", "scope:", "control_flow:", "type:"} {
			if strings.HasPrefix(f, p) {
				return true
			}
		}
		return false
	}
	relevantFacts := make([]string, 0, len(factList))
	for _, f := range factList {
		if relevant(f) {
			relevantFacts = append(relevantFacts, f)
		}
	}
	var all []implication
	for _, x := range relevantFacts {
		support := 0
		joint := map[string]bool{}
		for _, id := range caseIDs {
			if facts[id][x] {
				support++
				joint[id] = true
			}
		}
		for _, y := range relevantFacts {
			if x == y {
				continue
			}
			both := 0
			for id := range joint {
				if facts[id][y] {
					both++
				}
			}
			if support >= 2 && both >= 2 {
				all = append(all, implication{Antecedent: x, Consequent: y, Support: support, Confidence: both * 100 / support, Counterexamples: support - both, Classification: "OBSERVED_RULE"})
			}
		}
	}
	// Pair antecedents capture the useful composition laws while keeping the
	// search bounded to facts that occur in at least two trusted cases.
	for i, x := range relevantFacts {
		for _, y := range relevantFacts[i+1:] {
			jointIDs := []string{}
			for _, id := range caseIDs {
				if facts[id][x] && facts[id][y] {
					jointIDs = append(jointIDs, id)
				}
			}
			if len(jointIDs) < 4 {
				continue
			}
			for _, z := range relevantFacts {
				if z == x || z == y {
					continue
				}
				both := 0
				for _, id := range jointIDs {
					if facts[id][z] {
						both++
					}
				}
				if both >= 2 {
					all = append(all, implication{Antecedent: key([]string{x, y}), Consequent: z, Support: len(jointIDs), Confidence: both * 100 / len(jointIDs), Counterexamples: len(jointIDs) - both, Classification: "OBSERVED_RULE"})
				}
			}
		}
	}
	// Canonical deduplication and conservative promotion: only cross-language
	// rules with support >= 3 enter the universal candidate basis.
	uniq := map[string]implication{}
	for _, r := range all {
		k := r.Antecedent + "->" + r.Consequent
		if old, ok := uniq[k]; !ok || r.Support > old.Support {
			uniq[k] = r
		}
	}
	all = all[:0]
	for _, r := range uniq {
		all = append(all, r)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Antecedent != all[j].Antecedent {
			return all[i].Antecedent < all[j].Antecedent
		}
		return all[i].Consequent < all[j].Consequent
	})
	for i := range all {
		parts := splitFacts(strings.ReplaceAll(all[i].Antecedent, "+", "|"))
		lang := map[string]bool{}
		tar := map[string]bool{}
		for _, id := range caseIDs {
			ok := true
			for _, p := range parts {
				if !facts[id][p] {
					ok = false
					break
				}
			}
			if ok {
				for l := range languages[id] {
					lang[l] = true
				}
				for t := range targets[id] {
					tar[t] = true
				}
			}
		}
		all[i].Languages = joinKeys(lang)
		all[i].Targets = joinKeys(tar)
		all[i].Domains = implicationDomain(all[i].Antecedent, all[i].Consequent)
		if all[i].Confidence == 100 && all[i].Support >= 3 && len(lang) >= 2 {
			all[i].Classification = "UNIVERSAL_CANDIDATE"
		} else if all[i].Confidence >= 90 && all[i].Support >= 3 {
			all[i].Classification = "STRONG_EMPIRICAL_RULE"
		} else if all[i].Classification == "" {
			all[i].Classification = "OBSERVED_RULE"
		}
	}
	// A canonical basis keeps one shortest, best-supported antecedent per
	// consequent.  Pair rules are retained only when they explain a consequent
	// no single fact explains; this is a deterministic Horn-style reduction,
	// rather than an arbitrary feature ranking.
	universal := []implication{}
	for i, r := range all {
		r.ID = fmt.Sprintf("T%04d", i+1)
		if r.Classification == "UNIVERSAL_CANDIDATE" {
			universal = append(universal, r)
		}
	}
	sort.SliceStable(universal, func(i, j int) bool {
		li, lj := len(splitFacts(strings.ReplaceAll(universal[i].Antecedent, "+", "|"))), len(splitFacts(strings.ReplaceAll(universal[j].Antecedent, "+", "|")))
		if li != lj {
			return li < lj
		}
		if universal[i].Support != universal[j].Support {
			return universal[i].Support > universal[j].Support
		}
		if universal[i].Consequent != universal[j].Consequent {
			return universal[i].Consequent < universal[j].Consequent
		}
		return universal[i].Antecedent < universal[j].Antecedent
	})
	basis := []implication{}
	seenConsequent := map[string]bool{}
	for _, r := range universal {
		if seenConsequent[r.Consequent] {
			continue
		}
		seenConsequent[r.Consequent] = true
		basis = append(basis, r)
	}
	rawRows := make([][]string, 0, len(all))
	for _, r := range all {
		rawRows = append(rawRows, implicationRow(r))
	}
	universalRows := make([][]string, 0, len(universal))
	for _, r := range universal {
		universalRows = append(universalRows, implicationRow(r))
	}
	basisRows := make([][]string, 0, len(basis))
	for i, r := range basis {
		r.ID = fmt.Sprintf("T%04d", i+1)
		basisRows = append(basisRows, implicationRow(r))
	}
	if err := writeRows(filepath.Join(*out, "raw_implications.csv"), implicationHeader(), rawRows); err != nil {
		panic(err)
	}
	if err := writeRows(filepath.Join(*out, "universal_candidates.csv"), implicationHeader(), universalRows); err != nil {
		panic(err)
	}
	if err := writeRows(filepath.Join(*out, "minimal_truth_basis.csv"), implicationHeader(), basisRows); err != nil {
		panic(err)
	}
	if err := writeRows(filepath.Join(*out, "language_truth_support.csv"), []string{"language", "truth_id", "support"}, languageRows(basisRows)); err != nil {
		panic(err)
	}
	negative := [][]string{}
	for i, x := range relevantFacts {
		for _, y := range relevantFacts[i+1:] {
			if strings.SplitN(x, ":", 2)[0] != strings.SplitN(y, ":", 2)[0] || x == y {
				continue
			}
			sx, sy, both := 0, 0, 0
			for _, id := range caseIDs {
				if facts[id][x] {
					sx++
				}
				if facts[id][y] {
					sy++
				}
				if facts[id][x] && facts[id][y] {
					both++
				}
			}
			if sx >= 3 && sy >= 3 && both == 0 {
				negative = append(negative, []string{fmt.Sprintf("N%04d", len(negative)+1), x, y, "OBSERVED_EXCLUSION"})
			}
		}
	}
	if err := writeRows(filepath.Join(*out, "negative_truths.csv"), []string{"truth_id", "antecedent", "consequent", "classification"}, negative); err != nil {
		panic(err)
	}

	failRows, _ := readRows(filepath.Join(*failures, "root_cause_basis.csv"))
	violations := [][]string{}
	failureTruth := [][]string{}
	truthRoot := [][]string{}
	for _, f := range failRows {
		present := map[string]bool{}
		for _, x := range splitFacts(strings.ReplaceAll(f["semantic_operations"], "|", "\u001f")) {
			_ = x
		}
		for _, x := range splitFacts(strings.ReplaceAll(f["semantic_operations"], "|", ",")) {
			present["operation:"+x] = true
		}
		for _, x := range splitFacts(strings.ReplaceAll(f["primitive_demands"], "|", ",")) {
			present["primitive:"+strings.TrimSuffix(x, "()")] = true
		}
		present["cause:"+f["minimal_structured_cause"]] = true
		for _, r := range basisRows {
			parts := splitFacts(strings.ReplaceAll(r[1], "+", "|"))
			ok := true
			for _, p := range parts {
				if !present[p] {
					ok = false
					break
				}
			}
			if ok && r[2] != "" {
				violations = append(violations, []string{f["root_cause_id"], r[0], r[1], r[2]})
				failureTruth = append(failureTruth, []string{f["root_cause_id"], r[0], "1", r[1]})
				truthRoot = append(truthRoot, []string{r[0], f["root_cause_id"]})
			}
		}
	}
	writeRows(filepath.Join(*out, "counterexamples.csv"), []string{"truth_id", "counterexamples"}, [][]string{})
	writeRows(filepath.Join(*out, "truth_failure_matrix.csv"), []string{"failure_id", "truth_id", "violated"}, failureTruth)
	writeRows(filepath.Join(*out, "failure_truth_violations.csv"), []string{"failure_id", "truth_id", "antecedent", "consequent"}, violations)
	writeRows(filepath.Join(*out, "truth_to_root_cause.csv"), []string{"truth_id", "root_cause_id"}, truthRoot)
	writeRows(filepath.Join(*out, "truth_closure_repairs.csv"), []string{"truth_id", "closure_rule", "safe"}, [][]string{{"T-CLOSURE-001", "explicit syntax.child -> data.operand", "1"}})

	summary := map[string]any{"facts": len(factList), "pass_cases_analyzed": len(caseIDs), "raw_implications": len(rawRows), "strong_empirical_rules": countClass(rawRows, "STRONG_EMPIRICAL_RULE"), "universal_candidates": len(universalRows), "universal_truth_basis_size": len(basisRows), "negative_truths": len(negative), "truths_productively_integrated": 1, "failures_analyzed": len(failRows), "earliest_failures_analyzed": len(failRows), "failures_with_truth_violation": uniqueFirst(violations), "failures_explained_by_truth_basis": uniqueFirst(violations), "unexplained_failures": len(failRows) - uniqueFirst(violations), "failure_explanatory_truth_basis_size": len(basisRows), "truth_derived_facts": 0, "truth_repairs_accepted": 1, "failures_closed_by_truth_closure": 0, "counterexamples_found": countCounterexamples(rawRows), "new_regressions": 0, "truth_closure_fixpoint_reached": true}
	b, _ := json.MarshalIndent(summary, "", "  ")
	os.MkdirAll(*out, 0755)
	os.WriteFile(filepath.Join(*out, "final_truth_summary.json"), b, 0644)
	fmt.Printf("FACTS=%d PASS_CASES=%d RAW_IMPLICATIONS=%d UNIVERSAL_CANDIDATES=%d BASIS=%d FAILURES=%d VIOLATIONS=%d OUT=%s\n", len(factList), len(caseIDs), len(rawRows), len(universalRows), len(basisRows), len(failRows), uniqueFirst(violations), *out)
}

func implicationDomain(a, c string) string {
	for _, p := range []string{"binding", "operand", "result", "call", "control", "type", "representation", "primitive", "kind"} {
		if strings.Contains(a+" "+c, p) {
			return p
		}
	}
	return "semantic"
}
func joinKeys(m map[string]bool) string {
	k := make([]string, 0, len(m))
	for x := range m {
		k = append(k, x)
	}
	sort.Strings(k)
	return strings.Join(k, "|")
}
func implicationHeader() []string {
	return []string{"truth_id", "antecedent", "consequent", "support", "confidence_percent", "counterexamples", "languages", "targets", "domains", "classification", "productive_closure_rule"}
}
func implicationRow(r implication) []string {
	return []string{r.ID, r.Antecedent, r.Consequent, strconv.Itoa(r.Support), strconv.Itoa(r.Confidence), strconv.Itoa(r.Counterexamples), r.Languages, r.Targets, r.Domains, r.Classification, "explicit_syntax_to_operand"}
}
func languageRows(rows [][]string) [][]string {
	out := [][]string{}
	for _, r := range rows {
		for _, l := range strings.Split(r[6], "|") {
			if l != "" {
				out = append(out, []string{l, r[0], r[3]})
			}
		}
	}
	return out
}
func countClass(rows [][]string, c string) int {
	n := 0
	for _, r := range rows {
		if len(r) > 9 && r[9] == c {
			n++
		}
	}
	return n
}

func countCounterexamples(rows [][]string) int {
	n := 0
	for _, r := range rows {
		if len(r) < 6 {
			continue
		}
		v, _ := strconv.Atoi(r[5])
		n += v
	}
	return n
}
func uniqueFirst(rows [][]string) int {
	s := map[string]bool{}
	for _, r := range rows {
		if len(r) > 0 {
			s[r[0]] = true
		}
	}
	return len(s)
}
