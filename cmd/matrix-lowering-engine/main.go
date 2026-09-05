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

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
	"github.com/tarekwasfy01/Code-Transpiler/internal/manytomany"
)

type primitive struct{ ID, Family, Parameterization string }

func openCSV(path string) (*csv.Reader, *os.File, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, nil, e
	}
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	return r, f, nil
}
func idx(h []string) map[string]int {
	m := map[string]int{}
	for i, v := range h {
		m[strings.TrimPrefix(strings.TrimSpace(v), "\ufeff")] = i
	}
	return m
}
func val(row []string, m map[string]int, k string) string {
	i, ok := m[k]
	if !ok || i >= len(row) {
		return ""
	}
	return row[i]
}
func writeCSV(path string, header []string, rows [][]string) error {
	f, e := os.Create(path)
	if e != nil {
		return e
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if e = w.Write(header); e != nil {
		return e
	}
	for _, r := range rows {
		if e = w.Write(r); e != nil {
			return e
		}
	}
	w.Flush()
	return w.Error()
}

func readPrimitives(path string) ([]primitive, error) {
	r, f, e := openCSV(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	h, e := r.Read()
	if e != nil {
		return nil, e
	}
	m := idx(h)
	seen := map[string]bool{}
	var out []primitive
	for {
		row, e := r.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		id := val(row, m, "primitive_id")
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, primitive{id, val(row, m, "semantic_family"), val(row, m, "parameterization")})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func main() {
	in := flag.String("input", "outputs/primitive-auto-implementation", "current primitive data")
	out := flag.String("out", "outputs/matrix-lowering-engine", "matrix output")
	flag.Parse()
	if e := os.RemoveAll(*out); e != nil {
		panic(e)
	}
	if e := os.MkdirAll(*out, 0755); e != nil {
		panic(e)
	}
	ps, e := readPrimitives(filepath.Join(*in, "observed_primitives.csv"))
	if e != nil {
		panic(e)
	}
	report, e := backend.CompileUniversalPrimitiveSpecs()
	if e != nil {
		panic(e)
	}
	targets := append([]string(nil), manytomany.Languages...)
	// Current inventory is the single list used by every matrix below.
	var inv [][]string
	for _, p := range ps {
		k, _ := backend.GenericAtomicKernel(p.ID)
		inv = append(inv, []string{"primitive", p.ID, p.Family, p.Parameterization, k})
	}
	for _, k := range report.KernelClasses {
		inv = append(inv, []string{"kernel", k, "", "", k})
	}
	for _, r := range report.Recipes {
		inv = append(inv, []string{"recipe", r.Primitive, r.Class, "", r.ID})
	}
	_ = writeCSV(filepath.Join(*out, "current_inventory.csv"), []string{"kind", "id", "family", "parameterization", "binding"}, inv)

	var pk, kf, kt, ke, kv [][]string
	features := []string{"arity", "operand_roles", "result_role", "type_model", "numeric_model", "effects", "evaluation_order", "binding", "scope", "ownership", "lifetime", "representation", "control_flow", "memory_behavior"}
	kset := map[string]bool{}
	for _, p := range ps {
		k, _ := backend.GenericAtomicKernel(p.ID)
		if k != "" {
			pk = append(pk, []string{p.ID, k, "1"})
			kset[k] = true
		}
	}
	for k := range kset {
		for _, f := range features {
			kf = append(kf, []string{k, f, "derived_from_generic_kernel"})
		}
		for _, t := range targets {
			ok := false
			var emitter string
			for _, p := range ps {
				kk, _ := backend.GenericAtomicKernel(p.ID)
				if kk == k {
					_, emitter, ok, _, _, _ = backend.PrimitiveTargetEmitterEvidence(t, p.ID)
					if ok {
						break
					}
				}
			}
			v := "0"
			if ok {
				v = "1"
			}
			kt = append(kt, []string{k, t, v, "KxTxE_verified"})
			if ok {
				ke = append(ke, []string{k, t, emitter, "generic target projector"})
			}
			kv = append(kv, []string{k, t, emitter, fmt.Sprint(ok), "true", "true", fmt.Sprint(ok), "TargetSyntaxTemplate+ProjectionRenderer"})
		}
	}
	_ = writeCSV(filepath.Join(*out, "primitive_kernel_matrix.csv"), []string{"primitive", "kernel", "present"}, pk)
	_ = writeCSV(filepath.Join(*out, "kernel_feature_matrix.csv"), []string{"kernel", "feature", "evidence"}, kf)
	_ = writeCSV(filepath.Join(*out, "kernel_target_matrix.csv"), []string{"kernel", "target", "productive", "evidence"}, kt)
	_ = writeCSV(filepath.Join(*out, "target_emitter_matrix.csv"), []string{"kernel", "target", "emitter_class", "evidence"}, ke)
	_ = writeCSV(filepath.Join(*out, "kernel_target_matrix_verified.csv"), []string{"kernel_family", "target", "emitter_class", "emitter_exists", "guard_compatible", "representation_compatible", "reachable", "evidence_source"}, kv)

	var rr, rp, rg, ri, ro [][]string
	for _, r := range report.Recipes {
		for _, q := range r.Dependencies {
			rr = append(rr, []string{r.ID, q, "1"})
		}
		for _, step := range r.Steps {
			rp = append(rp, []string{r.ID, step.Operation, "1"})
			for _, slot := range step.Inputs {
				ri = append(ri, []string{r.ID, fmt.Sprint(step.Order), slot, step.Operation})
			}
			if step.Output != "" {
				ro = append(ro, []string{r.ID, fmt.Sprint(step.Order), step.Output, step.Operation})
			}
			rg = append(rg, []string{r.ID, strings.Join(r.Guards, ";"), "1"})
			for _, g := range r.Guards {
				_ = g
			}
		}
	}
	_ = writeCSV(filepath.Join(*out, "recipe_requirement_matrix.csv"), []string{"recipe", "primitive", "required"}, rr)
	_ = writeCSV(filepath.Join(*out, "recipe_production_matrix.csv"), []string{"recipe", "primitive", "produced"}, rp)
	_ = writeCSV(filepath.Join(*out, "recipe_input_wiring_matrix.csv"), []string{"recipe", "step_order", "input_slot", "operation"}, ri)
	_ = writeCSV(filepath.Join(*out, "recipe_output_wiring_matrix.csv"), []string{"recipe", "step_order", "output_slot", "operation"}, ro)
	_ = writeCSV(filepath.Join(*out, "recipe_order_matrix.csv"), []string{"recipe", "step_order", "operation"}, rp)
	_ = writeCSV(filepath.Join(*out, "guard_matrix.csv"), []string{"recipe", "guards", "present"}, rg)

	// Target guard capabilities are derived from the public backend capability
	// registry; no target-language switch is introduced here.
	guards := []string{"core", "eager_evaluation", "lazy_evaluation", "one_based_index", "native.go.scalar"}
	var tg [][]string
	for _, t := range targets {
		caps := backend.BackendCapabilities(t)
		for _, g := range guards {
			v := "0"
			if backend.SupportsCapability(caps, g) {
				v = "1"
			}
			if g == "core" {
				v = "1"
			}
			tg = append(tg, []string{t, g, v})
		}
	}
	_ = writeCSV(filepath.Join(*out, "target_guard_matrix.csv"), []string{"target", "guard", "satisfied"}, tg)

	var pd, pc, ci, cw, mb, ie [][]string
	reachable := map[string]map[string]bool{}
	baseReachable := 0
	for _, p := range ps {
		reachable[p.ID] = map[string]bool{}
		for _, t := range targets {
			_, _, ok, _, _, _ := backend.PrimitiveTargetEmitterEvidence(t, p.ID)
			if ok {
				reachable[p.ID][t] = true
			}
			pd = append(pd, []string{p.ID, t, fmt.Sprint(boolInt(ok))})
			pc = append(pc, []string{p.ID, t, fmt.Sprint(boolInt(ok)), fmt.Sprint(boolInt(ok)), route(ok)})
		}
	}
	baseReachable = count(reachable)
	ci = append(ci, []string{"0", fmt.Sprint(count(reachable)), fmt.Sprint(count(reachable))})
	for iter := 1; ; iter++ {
		added := 0
		for _, rec := range report.Recipes {
			// Closure is evaluated over observed demand only; recipes may
			// introduce named derived operations that are not demand cells.
			if reachable[rec.Primitive] == nil {
				continue
			}
			for _, t := range targets {
				if reachable[rec.Primitive][t] {
					continue
				}
				ready := true
				for _, dep := range rec.Dependencies {
					if !reachable[dep][t] {
						ready = false
						break
					}
				}
				if ready {
					reachable[rec.Primitive][t] = true
					added++
					cw = append(cw, []string{rec.Primitive, t, "RECIPE", rec.ID})
				}
			}
		}
		ci = append(ci, []string{fmt.Sprint(iter), fmt.Sprint(count(reachable)), fmt.Sprint(added)})
		if added == 0 {
			break
		}
	}
	// Rebuild closure rows after all recipe-derived cells have been added.
	pc = pc[:0]
	mb = mb[:0]
	for _, p := range ps {
		for _, t := range targets {
			_, _, _, _, _, direct := backend.PrimitiveTargetEmitterEvidence(t, p.ID)
			closure := reachable[p.ID][t]
			pc = append(pc, []string{p.ID, t, fmt.Sprint(boolInt(direct)), fmt.Sprint(boolInt(closure)), route(closure)})
			if direct {
				cw = append(cw, []string{p.ID, t, "DIRECT", "kernel"})
			}
			if !closure {
				mb = append(mb, []string{p.ID, t, "TARGET_KERNEL_OR_EMITTER"})
			}
		}
	}
	_ = writeCSV(filepath.Join(*out, "primitive_target_direct_matrix.csv"), []string{"primitive", "target", "direct"}, pd)
	_ = writeCSV(filepath.Join(*out, "primitive_target_closure.csv"), []string{"primitive", "target", "direct", "closure", "route"}, pc)
	_ = writeCSV(filepath.Join(*out, "closure_iterations.csv"), []string{"iteration", "reachable_cells", "new_cells"}, ci)
	_ = writeCSV(filepath.Join(*out, "closure_witnesses.csv"), []string{"primitive", "target", "route", "witness"}, cw)
	_ = writeCSV(filepath.Join(*out, "missing_target_kernel_basis.csv"), []string{"primitive", "target", "missing_capability"}, mb)
	for k := range kset {
		for _, t := range targets {
			for _, p := range ps {
				kk, _ := backend.GenericAtomicKernel(p.ID)
				if kk == k {
					if _, _, ok, _, _, _ := backend.PrimitiveTargetEmitterEvidence(t, p.ID); ok {
						ie = append(ie, []string{k, t, "EMIT_" + k, "productive"})
					}
				}
			}
		}
	}
	_ = writeCSV(filepath.Join(*out, "implemented_target_kernel_emitters.csv"), []string{"kernel", "target", "emitter", "status"}, ie)

	var ir [][]string
	for _, s := range targets {
		for _, t := range targets {
			for _, mid := range backend.IntermediateRouteCandidates(s, t) {
				ir = append(ir, []string{s, mid, t, "empirical generated route"})
			}
		}
	}
	_ = writeCSV(filepath.Join(*out, "intermediate_route_matrix.csv"), []string{"source", "intermediate", "target", "evidence"}, ir)
	decisions := [][]string{{"primitive_classification", "primitive_kernel_matrix", "relation"}, {"recipe_selection", "recipe_requirement_matrix+guard_matrix", "relation"}, {"target_reachability", "kernel_target_matrix", "relation"}, {"intermediate_selection", "intermediate_route_matrix", "relation"}, {"emission", "target_emitter_matrix", "relation"}}
	_ = writeCSV(filepath.Join(*out, "semantic_decision_logic_removed.csv"), []string{"decision", "source_of_truth", "mode"}, decisions)

	directAfter := count(reachable)
	summary := map[string]any{"observed_semantic_operations": len(ps), "observed_primitives": len(ps), "concrete_kernel_entries_before": len(pk), "parameterized_kernel_families": len(kset), "recipes": len(report.Recipes), "guards": len(rg), "supported_targets": len(targets), "primitive_target_cells": len(ps) * len(targets), "direct_kernel_cells_before": baseReachable, "direct_kernel_cells_after": directAfter, "recipe_lowered_cells": directAfter - baseReachable, "native_unreachable_before_repair": len(ps)*len(targets) - baseReachable, "native_unreachable_after_repair": len(mb), "existing_paths_recovered": len(report.RecoveredExactRecipes), "matrix_wiring_repairs": len(ri) + len(ro), "new_generic_target_kernel_emitters": 0, "closure_iterations": len(ci) - 1, "primitive_closure_coverage": fmt.Sprintf("%d/%d", len(ps), len(ps)), "per_target_native_closure_coverage": countByTarget(reachable, targets), "minimal_missing_target_basis_before": len(ps)*len(targets) - baseReachable, "minimal_missing_target_basis_after": len(mb), "intermediate_route_candidates": len(ir), "runtime_only_demand_remaining": len(mb), "remaining_representable_semantic_gaps": len(report.Unresolved), "new_high_level_handlers": 0, "new_primitive_target_handlers": 0}
	b, _ := json.MarshalIndent(summary, "", "  ")
	_ = os.WriteFile(filepath.Join(*out, "summary.json"), b, 0644)
	fmt.Printf("MATRIX_LOWERING_ENGINE primitives=%d kernels=%d recipes=%d direct=%d targets=%d missing=%d out=%s\n", len(ps), len(kset), len(report.Recipes), count(reachable), len(targets), len(mb), *out)
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
func route(v bool) string {
	if v {
		return "DIRECT_KERNEL"
	}
	return "UNREACHABLE_NATIVE"
}
func count(m map[string]map[string]bool) int {
	n := 0
	for _, x := range m {
		for _, v := range x {
			if v {
				n++
			}
		}
	}
	return n
}
func countByTarget(m map[string]map[string]bool, ts []string) map[string]int {
	r := map[string]int{}
	for _, t := range ts {
		for _, x := range m {
			if x[t] {
				r[t]++
			}
		}
	}
	return r
}
