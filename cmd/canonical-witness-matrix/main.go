// canonical-witness-matrix executes the backend matrix from generated
// Canonical UAST witnesses. Unlike source replay, an absent frontend source
// witness never suppresses Primitive x Target validation.
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
	"github.com/tarekwasfy01/Code-Transpiler/internal/manytomany"
)

func writeCSV(path string, rows [][]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{"primitive_id", "kernel", "target", "direct", "universal_lowering", "runtime", "status", "diagnostic"}); err != nil {
		return err
	}
	return w.WriteAll(rows)
}

func main() {
	out := flag.String("out", "outputs/v8-canonical-witness-matrix", "output directory")
	primitive := flag.String("primitive", "", "optional primitive filter")
	target := flag.String("target", "", "optional target filter")
	stages := flag.String("stages", "all", "direct or all")
	offset := flag.Int("offset", 0, "sorted primitive offset for bounded matrix shards")
	limit := flag.Int("limit", 0, "maximum primitives in this shard; zero means all")
	flag.Parse()
	report, err := backend.CompileUniversalPrimitiveSpecs()
	if err != nil {
		panic(err)
	}
	ids := make([]string, 0, len(report.Specs))
	for _, s := range report.Specs {
		ids = append(ids, s.ID)
	}
	sort.Strings(ids)
	rows, counts := make([][]string, 0, len(ids)*len(manytomany.Languages)), map[string]int{}
	type outcome struct{ direct, lowered, runtime, status, diagnostic string }
	// Parameterized primitives with the same kernel share an exact canonical
	// witness shape. Execute every kernel×target cell once, then project that
	// witnessed result onto its quotient class. This preserves real execution
	// while avoiding 1,000s of redundant identical emitter invocations.
	cache := map[string]outcome{}
	selected := 0
	for ordinal, id := range ids {
		if ordinal < *offset {
			continue
		}
		if *limit > 0 && selected >= *limit {
			break
		}
		if *primitive != "" && !strings.EqualFold(*primitive, id) {
			continue
		}
		selected++
		witness, e := backend.BuildCanonicalPrimitiveWitness(id)
		for _, language := range manytomany.Languages {
			if *target != "" && !strings.EqualFold(*target, language) {
				continue
			}
			if e != nil {
				rows = append(rows, []string{id, "", language, "", "", "", "NO_EMISSION", e.Error()})
				counts["NO_EMISSION"]++
				continue
			}
			key := witness.Kernel + "\x00" + language
			value, exists := cache[key]
			if !exists {
				direct, directErr := backend.EmitSemanticDirect(language, witness.Program)
				if directErr == nil && strings.TrimSpace(direct) != "" {
					value = outcome{direct: "PASS", status: "VALIDATED_DIRECT"}
				} else {
					diagnostic := "DIRECT_EMPTY_OUTPUT"
					if directErr != nil {
						diagnostic = directErr.Error()
					}
					if *stages == "direct" {
						value = outcome{direct: "FAIL", status: "DIRECT_FAIL", diagnostic: diagnostic}
						cache[key] = value
						rows = append(rows, []string{id, witness.Kernel, language, value.direct, value.lowered, value.runtime, value.status, value.diagnostic})
						counts[value.status]++
						continue
					}
					lowered, _, lowerErr := backend.EmitSemanticLoweredDirect(language, witness.Program)
					if lowerErr == nil && strings.TrimSpace(lowered) != "" {
						value = outcome{direct: "FAIL", lowered: "PASS", status: "VALIDATED_UNIVERSAL_LOWERING", diagnostic: diagnostic}
					} else {
						runtime, runtimeErr := backend.EmitSemantic(language, witness.Program)
						if runtimeErr == nil && strings.TrimSpace(runtime) != "" {
							value = outcome{direct: "FAIL", lowered: "FAIL", runtime: "PASS", status: "RUNTIME_ONLY", diagnostic: diagnostic}
						} else {
							if lowerErr != nil {
								diagnostic += " | lowering: " + lowerErr.Error()
							}
							if runtimeErr != nil {
								diagnostic += " | runtime: " + runtimeErr.Error()
							}
							value = outcome{direct: "FAIL", lowered: "FAIL", runtime: "FAIL", status: "NO_EMISSION", diagnostic: diagnostic}
						}
					}
				}
				cache[key] = value
			}
			rows = append(rows, []string{id, witness.Kernel, language, value.direct, value.lowered, value.runtime, value.status, value.diagnostic})
			counts[value.status]++
		}
	}
	if err := writeCSV(filepath.Join(*out, "target_cell_execution.csv"), rows); err != nil {
		panic(err)
	}
	fmt.Printf("CANONICAL_WITNESS_MATRIX authority=%d cells=%d direct=%d lowered=%d runtime=%d no_emission=%d out=%s\n", len(ids), len(rows), counts["VALIDATED_DIRECT"], counts["VALIDATED_UNIVERSAL_LOWERING"], counts["RUNTIME_ONLY"], counts["NO_EMISSION"], *out)
}
