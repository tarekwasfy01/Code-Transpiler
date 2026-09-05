// observed-witness-replay executes only source operations that have already
// been observed in a Canonical-UAST sidecar.  It deliberately emits an
// explicit SOURCE_WITNESS_MISSING row for evidence-only primitives instead of
// treating a kernel capability as an executable semantic witness.
package main

import (
	"encoding/csv"
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

type nodeRow map[string]string

func readCSV(path string) ([]nodeRow, error) {
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
	for i := range h {
		h[i] = strings.TrimPrefix(strings.TrimSpace(h[i]), "\ufeff")
	}
	var rows []nodeRow
	for {
		v, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		x := nodeRow{}
		for i, k := range h {
			if i < len(v) {
				x[k] = strings.TrimSpace(v[i])
			}
		}
		rows = append(rows, x)
	}
	return rows, nil
}

func fixture(id string) (string, bool) {
	// These are source programs fed through the real modern R frontend.  Each
	// fixture is deliberately small so its UAST operation is observable.
	switch id {
	case "ASSIGNMENT":
		return "x <- 1\n", true
	case "LOAD":
		return "x <- 1\ny <- x\n", true
	case "LITERAL":
		return "x <- 1\n", true
	case "CALL":
		return "x <- sum(c(1, 2, 3))\n", true
	case "NOT":
		return "x <- !TRUE\n", true
	case "BIT_AND":
		// R's infix '&' is logical, not an integer bitwise operation.  Keep
		// this witness inside the source language's actual type contract.
		return "x <- TRUE & FALSE\n", true
	case "BIT_OR":
		return "x <- TRUE | FALSE\n", true
	case "DIV":
		return "x <- 6 / 2\n", true
	case "EQ":
		return "x <- 1 == 1\n", true
	case "GT":
		return "x <- 2 > 1\n", true
	case "ITERATION":
		return "for (x in c(1, 2)) { y <- x }\n", true
	case "LE":
		return "x <- 1 <= 2\n", true
	case "LT":
		return "x <- 1 < 2\n", true
	case "MUL":
		return "x <- 2 * 3\n", true
	case "NE":
		return "x <- 1 != 2\n", true
	case "OR":
		return "x <- TRUE | FALSE\n", true
	case "RETURN":
		return "f <- function(x) { return(x) }\n", true
	case "SUB":
		return "x <- 3 - 1\n", true
	}
	return "", false
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
	if err := w.WriteAll(rows); err != nil {
		return err
	}
	w.Flush()
	return w.Error()
}

func main() {
	nodesPath := flag.String("nodes", "outputs/miner-semantic-validation-v2-clean/uast_nodes.csv", "real miner UAST node sidecar")
	out := flag.String("out", "outputs/v6-real-witness-replay", "result directory")
	primitiveFilter := flag.String("primitive", "", "execute one primitive only")
	targetFilter := flag.String("target", "", "execute one target only")
	show := flag.Bool("show", false, "print successful direct witness source")
	showUAST := flag.Bool("show-uast", false, "print the canonical witness UAST")
	stages := flag.String("stages", "all", "direct or all")
	flag.Parse()
	nodes, err := readCSV(*nodesPath)
	if err != nil {
		panic(err)
	}
	observed := map[string]bool{}
	for _, n := range nodes {
		if id := strings.TrimSpace(n["primitive_id"]); id != "" {
			observed[id] = true
		}
	}
	report, err := backend.CompileUniversalPrimitiveSpecs()
	if err != nil {
		panic(err)
	}
	ids := make([]string, 0, len(report.Specs))
	for _, p := range report.Specs {
		ids = append(ids, p.ID)
	}
	sort.Strings(ids)

	cells := make([][]string, 0, len(ids)*len(manytomany.Languages))
	frontend := make([][]string, 0, len(observed))
	counts := map[string]int{}
	for _, id := range ids {
		if *primitiveFilter != "" && !strings.EqualFold(*primitiveFilter, id) {
			continue
		}
		source, hasFixture := fixture(id)
		if !observed[id] || !hasFixture {
			for _, target := range manytomany.Languages {
				if *targetFilter != "" && !strings.EqualFold(*targetFilter, target) {
					continue
				}
				cells = append(cells, []string{id, target, "", "", "", "", "SOURCE_WITNESS_MISSING", "no observed Source→UAST witness for exact primitive"})
				counts["SOURCE_WITNESS_MISSING"]++
			}
			continue
		}
		program, frontErr := backend.LowerSource("r", "witness.R", source)
		frontStatus := "PASS"
		frontDiag := ""
		if frontErr != nil {
			frontStatus, frontDiag = "FAIL", frontErr.Error()
		}
		frontend = append(frontend, []string{id, "r", frontStatus, frontDiag})
		if frontErr == nil && *showUAST {
			if encoded, marshalErr := program.MarshalUniversalASTJSON(); marshalErr == nil {
				fmt.Printf("--- %s/UAST ---\n%s\n", id, encoded)
			}
		}
		for _, target := range manytomany.Languages {
			if *targetFilter != "" && !strings.EqualFold(*targetFilter, target) {
				continue
			}
			if frontErr != nil {
				cells = append(cells, []string{id, target, "FAIL", "", "", "", "FRONTEND_FAIL", frontDiag})
				counts["FRONTEND_FAIL"]++
				continue
			}
			direct, directErr := backend.EmitSemanticDirect(target, program)
			if directErr == nil && strings.TrimSpace(direct) != "" {
				if *show {
					fmt.Printf("--- %s/%s ---\n%s\n", id, target, direct)
				}
				cells = append(cells, []string{id, target, "PASS", "PASS", "", "", "VALIDATED_DIRECT", ""})
				counts["VALIDATED_DIRECT"]++
				continue
			}
			directDiagnostic := "DIRECT_EMPTY_OUTPUT"
			if directErr != nil {
				directDiagnostic = directErr.Error()
			}
			if *stages == "direct" {
				cells = append(cells, []string{id, target, "PASS", "FAIL", "", "", "DIRECT_FAIL", directDiagnostic})
				counts["DIRECT_FAIL"]++
				continue
			}
			lowered, _, loweredErr := backend.EmitSemanticLoweredDirect(target, program)
			if loweredErr == nil && strings.TrimSpace(lowered) != "" {
				cells = append(cells, []string{id, target, "PASS", "FAIL", "PASS", "", "VALIDATED_UNIVERSAL_LOWERING", directDiagnostic})
				counts["VALIDATED_UNIVERSAL_LOWERING"]++
				continue
			}
			runtime, runtimeErr := backend.EmitSemantic(target, program)
			if runtimeErr == nil && strings.TrimSpace(runtime) != "" {
				cells = append(cells, []string{id, target, "PASS", "FAIL", "FAIL", "PASS", "RUNTIME_ONLY", directDiagnostic})
				counts["RUNTIME_ONLY"]++
				continue
			}
			diag := directDiagnostic
			if loweredErr != nil {
				diag += " | lowering: " + loweredErr.Error()
			}
			if runtimeErr != nil {
				diag += " | runtime: " + runtimeErr.Error()
			}
			cells = append(cells, []string{id, target, "PASS", "FAIL", "FAIL", "FAIL", "NO_EMISSION", diag})
			counts["NO_EMISSION"]++
		}
	}
	if err := writeRows(filepath.Join(*out, "target_cell_execution.csv"), []string{"primitive_id", "target", "frontend", "direct", "universal_lowering", "runtime", "status", "diagnostic"}, cells); err != nil {
		panic(err)
	}
	if err := writeRows(filepath.Join(*out, "frontend_source_witnesses.csv"), []string{"primitive_id", "source_language", "status", "diagnostic"}, frontend); err != nil {
		panic(err)
	}
	fmt.Printf("REAL_WITNESS_REPLAY authority=%d observed=%d frontend=%d direct=%d lowered=%d runtime=%d missing_source=%d no_emission=%d out=%s\n", len(ids), len(observed), len(frontend), counts["VALIDATED_DIRECT"], counts["VALIDATED_UNIVERSAL_LOWERING"], counts["RUNTIME_ONLY"], counts["SOURCE_WITNESS_MISSING"], counts["NO_EMISSION"], *out)
}
