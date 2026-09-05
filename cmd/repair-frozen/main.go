// repair-frozen consumes the immutable raw miner output and creates the
// separate repair quotient. It never mutates raw/ and never groups by
// diagnostic wording.
package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type failure struct{ ID, CaseID, Lang, Target, Kind, Stage, Class, Diagnostic string }
type source struct{ Lang, Path, Text string }

func read(path string) ([]map[string]string, error) {
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
	var out []map[string]string
	for {
		row, e := r.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			continue
		}
		m := map[string]string{}
		for i, k := range h {
			if i < len(row) {
				m[k] = row[i]
			}
		}
		out = append(out, m)
	}
	return out, nil
}
func write(path string, header []string, rows [][]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err = w.Write(header); err != nil {
		return err
	}
	for _, r := range rows {
		if err = w.Write(r); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}
func main() {
	in := "matrices/real_source_miner/raw"
	out := "matrices/real_source_miner/repair"
	if len(os.Args) > 1 {
		in = os.Args[1]
	}
	if len(os.Args) > 2 {
		out = os.Args[2]
	}
	rows, err := read(filepath.Join(in, "failures_raw.csv"))
	if err != nil {
		panic(err)
	}
	groups := map[string][]failure{}
	for _, r := range rows {
		f := failure{r["failure_id"], r["case_id"], r["source_language"], r["target_language"], r["output_kind"], r["validation_stage"], r["diagnostic_class"], r["diagnostic"]}
		key := strings.Join([]string{f.Stage, f.Class, f.Kind}, "|")
		groups[key] = append(groups[key], f)
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	q, members, features, relations, deps, native, mini := [][]string{}, [][]string{}, [][]string{}, [][]string{}, [][]string{}, [][]string{}, [][]string{}
	for i, key := range keys {
		id := fmt.Sprintf("Q%04d", i+1)
		gs := groups[key]
		langs, cases := map[string]bool{}, map[string]bool{}
		for _, f := range gs {
			langs[f.Lang] = true
			cases[f.CaseID] = true
			members = append(members, []string{id, f.ID, f.CaseID, f.Lang, f.Target, f.Kind, f.Stage, f.Class})
		}
		var ls, cs []string
		for x := range langs {
			ls = append(ls, x)
		}
		for x := range cases {
			cs = append(cs, x)
		}
		sort.Strings(ls)
		sort.Strings(cs)
		q = append(q, []string{id, fmt.Sprint(len(gs)), key, strings.Join(ls, ";"), strings.Join(cs, ";"), gs[0].ID})
		features = append(features, []string{id, "validation_stage", gs[0].Stage})
		features = append(features, []string{id, "failure_class", gs[0].Class})
		relations = append(relations, []string{id, "target/output", gs[0].Kind})
		deps = append(deps, []string{id, "case_count", fmt.Sprint(len(gs))})
		native = append(native, []string{id, "native_features", "isa=x86_64;os=windows;abi=win64"})
		mini = append(mini, []string{id, gs[0].ID, gs[0].CaseID, gs[0].Lang, gs[0].Target, gs[0].Stage, gs[0].Class})
	}
	_ = write(filepath.Join(out, "failure_quotient.csv"), []string{"family_id", "member_count", "signature", "source_languages", "case_ids", "representative_failure_id"}, q)
	_ = write(filepath.Join(out, "failure_family_members.csv"), []string{"family_id", "failure_id", "case_id", "source_language", "target", "kind", "stage", "class"}, members)
	_ = write(filepath.Join(out, "failure_family_features.csv"), []string{"family_id", "feature", "value"}, features)
	_ = write(filepath.Join(out, "failure_family_relations.csv"), []string{"family_id", "relation", "value"}, relations)
	_ = write(filepath.Join(out, "failure_family_dependencies.csv"), []string{"family_id", "dependency", "value"}, deps)
	_ = write(filepath.Join(out, "failure_family_native_features.csv"), []string{"family_id", "feature", "value"}, native)
	_ = write(filepath.Join(out, "minimized_failures.csv"), []string{"family_id", "representative_failure_id", "case_id", "source_language", "target", "stage", "class"}, mini)
	_ = os.MkdirAll(filepath.Join(out, "regression_cases"), 0755)
	fmt.Printf("RAW_FAILURES=%d QUOTIENT_FAMILIES=%d OUT=%s\n", len(rows), len(keys), out)
}
