package backend

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RuntimePromotionAnalysis is a report/registry view over the existing
// target×UASF preservation matrix. It is deliberately not an IR: it records
// which runtime cells have a complete direct projection contract and which
// obligations still prevent promotion.
type RuntimePromotionAnalysis struct {
	Schema                 string                         `json:"schema"`
	Targets                []string                       `json:"targets"`
	ProjectionClasses      []string                       `json:"projection_classes"`
	RuntimeUASF            [][]string                     `json:"runtime_uasf"`
	RuntimeProjectionClass map[string]map[string]bool     `json:"runtime_projection_classes"`
	DirectProjectionClass  map[string]map[string]bool     `json:"direct_projection_classes"`
	Residual               map[string]map[string][]string `json:"direct_residual"`
	Promotable             map[string]map[string]bool     `json:"promotable"`
}

// UniversalRuntimePromotionAnalysis derives M_RU and M_RP directly from the
// current preservation and projection-class matrices. A cell is promotable
// only when the class has a direct syntax contract and no residual obligation.
func UniversalRuntimePromotionAnalysis() (RuntimePromotionAnalysis, error) {
	preservation, err := UniversalTargetPreservationMatrix()
	if err != nil {
		return RuntimePromotionAnalysis{}, err
	}
	projection, err := UASTTargetProjectionCapabilities()
	if err != nil {
		return RuntimePromotionAnalysis{}, err
	}
	registry, err := UniversalStructureProjectionRegistry()
	if err != nil {
		return RuntimePromotionAnalysis{}, err
	}
	classes := make([]string, 0, len(registry.Classes))
	for class := range registry.Classes {
		classes = append(classes, class)
	}
	sort.Strings(classes)
	analysis := RuntimePromotionAnalysis{
		Schema: "code-transpiler.uast-runtime-promotion.v1", Targets: append([]string(nil), preservation.Targets...), ProjectionClasses: classes,
		RuntimeUASF: make([][]string, len(preservation.Targets)), RuntimeProjectionClass: map[string]map[string]bool{}, DirectProjectionClass: map[string]map[string]bool{}, Residual: map[string]map[string][]string{}, Promotable: map[string]map[string]bool{},
	}
	for col, target := range preservation.Targets {
		analysis.RuntimeProjectionClass[target] = map[string]bool{}
		analysis.DirectProjectionClass[target] = map[string]bool{}
		analysis.Residual[target] = map[string][]string{}
		analysis.Promotable[target] = map[string]bool{}
		for row, facet := range preservation.Capabilities {
			if preservation.Status(row, col) == PreservationRuntime {
				analysis.RuntimeUASF[col] = append(analysis.RuntimeUASF[col], facet)
			}
		}
		for _, class := range classes {
			mode := projection[target][class]
			if mode == PreservationRuntime {
				analysis.RuntimeProjectionClass[target][class] = true
			}
			if mode == PreservationDirect {
				analysis.DirectProjectionClass[target][class] = true
			}
			residual := []string{}
			switch mode {
			case PreservationDirect:
				// complete
			case PreservationRuntime:
				residual = append(residual, "target_runtime_contract")
			default:
				residual = append(residual, strings.ToLower(string(mode)))
			}
			if len(residual) != 0 {
				analysis.Residual[target][class] = residual
			}
			analysis.Promotable[target][class] = mode == PreservationDirect && len(residual) == 0
		}
	}
	return analysis, nil
}

// WriteRuntimePromotionAnalysis persists the matrix views used by the next
// evidence-mining round. Existing runtime support remains untouched.
func WriteRuntimePromotionAnalysis(dir string) (RuntimePromotionAnalysis, error) {
	a, err := UniversalRuntimePromotionAnalysis()
	if err != nil {
		return RuntimePromotionAnalysis{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return RuntimePromotionAnalysis{}, err
	}
	encoded, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return RuntimePromotionAnalysis{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "runtime_promotion_analysis.json"), encoded, 0o644); err != nil {
		return RuntimePromotionAnalysis{}, err
	}
	write := func(name string, header []string, rows [][]string) error {
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		defer f.Close()
		w := csv.NewWriter(f)
		if err := w.Write(header); err != nil {
			return err
		}
		for _, row := range rows {
			if err := w.Write(row); err != nil {
				return err
			}
		}
		w.Flush()
		return w.Error()
	}
	runtimeRows := [][]string{}
	for col, target := range a.Targets {
		for _, facet := range a.RuntimeUASF[col] {
			runtimeRows = append(runtimeRows, []string{target, facet, "RUNTIME"})
		}
	}
	if err := write("runtime_promotion_matrix.csv", []string{"target", "canonical_semantic_id", "runtime_before"}, runtimeRows); err != nil {
		return a, err
	}
	classRows := [][]string{}
	for _, target := range a.Targets {
		for _, class := range a.ProjectionClasses {
			before := a.RuntimeProjectionClass[target][class]
			direct := a.DirectProjectionClass[target][class]
			classRows = append(classRows, []string{target, class, fmt.Sprintf("%t", before), fmt.Sprintf("%t", direct), strings.Join(a.Residual[target][class], ";"), fmt.Sprintf("%t", a.Promotable[target][class])})
		}
	}
	if err := write("runtime_promotion_projection_classes.csv", []string{"target", "projection_class", "runtime_before", "direct_contract", "direct_residual", "promotion_allowed"}, classRows); err != nil {
		return a, err
	}
	contractRows := [][]string{}
	for _, target := range a.Targets {
		for _, class := range a.ProjectionClasses {
			direct := a.DirectProjectionClass[target][class]
			templateID := ""
			if direct {
				templateID = class
			}
			contractRows = append(contractRows, []string{target, class, templateID, "", "", "", "0", "0", "0", "false", "false", "false", "false", "false", fmt.Sprintf("%t", a.Promotable[target][class])})
		}
	}
	if err := write("runtime_promotion_contracts.csv", []string{"target", "projection_class", "direct_template_id", "template_signature", "slot_roles", "target_parameters", "native_occurrences", "independent_packages", "independent_hashes", "roundtrip_pass", "compiler_pass", "behavior_match_runtime", "empirical_proven", "conflict", "promotion_allowed"}, contractRows); err != nil {
		return a, err
	}
	return a, nil
}
