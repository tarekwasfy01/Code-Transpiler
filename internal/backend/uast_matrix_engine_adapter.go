package backend

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

// WriteUASTMatrixEngineInputs adapts existing, checked-in UAST matrices to the
// offline uast-matrix-engine contract.  It performs no closure, matrix product,
// readiness selection, or preservation selection.  Those operations belong to
// the external engine.  In particular, language facet evidence is already an
// exact quotient class in the source matrix, so it is emitted as a stable
// evidence feature rather than being expanded back into guessed source rows.
func WriteUASTMatrixEngineInputs(dir string) error {
	if err := loadUniversalASTBasis(); err != nil {
		return err
	}
	if dir == "" {
		return fmt.Errorf("missing UAST matrix engine input directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b := uastEmbedded.Basis
	features := make([]string, len(b.Facets))
	for i, facet := range b.Facets {
		features[i] = "uast.evidence." + facet
	}

	if err := writeUASTEngineCSV(filepath.Join(dir, "language_features.csv"), []string{"language_id", "source_feature_id"}, func(w *csv.Writer) error {
		for _, frontend := range Frontends() {
			profile := universalASTProfileLanguage(frontend.ID)
			row := indexOf(b.Languages, profile)
			if row < 0 {
				continue // no checked-in language evidence row; never synthesize one
			}
			for col := range b.Facets {
				if b.LanguageFacet.At(row, col) != 0 {
					if err := w.Write([]string{frontend.ID, features[col]}); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if err := writeUASTEngineCSV(filepath.Join(dir, "feature_capabilities.csv"), []string{"source_feature_id", "canonical_semantic_id"}, func(w *csv.Writer) error {
		for col, facet := range b.Facets {
			if err := w.Write([]string{features[col], facet}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if err := writeUASTEngineCSV(filepath.Join(dir, "capability_schema.csv"), []string{"canonical_semantic_id", "element_type", "element_id"}, func(w *csv.Writer) error {
		for _, facet := range b.Facets {
			if err := w.Write([]string{facet, "facet", facet}); err != nil {
				return err
			}
		}
		if err := writeUASTEngineMatrixEdges(w, b.Facets, b.StructuralKinds, b.StructuralFacetSeed.Transpose(), "structure"); err != nil {
			return err
		}
		if err := writeUASTEngineMatrixEdges(w, b.Facets, b.ConcreteRelations, b.FacetConcreteRelation, "relation"); err != nil {
			return err
		}
		return writeUASTEngineMatrixEdges(w, b.Facets, b.Fields, b.FacetField, "field")
	}); err != nil {
		return err
	}
	// The schema inputs contain no capability-to-capability prerequisite matrix.
	// Keep the adapter file empty rather than turning overlap/coupling into a
	// fabricated dependency.
	if err := writeUASTEngineCSV(filepath.Join(dir, "capability_dependencies.csv"), []string{"canonical_semantic_id", "depends_on_semantic_id"}, func(*csv.Writer) error { return nil }); err != nil {
		return err
	}
	already := uastEngineDirectFacetSet(b)
	if err := writeUASTEngineCSV(filepath.Join(dir, "capability_status.csv"), []string{"canonical_semantic_id", "canonical", "conflict", "already_uast"}, func(w *csv.Writer) error {
		for _, facet := range b.Facets {
			if err := w.Write([]string{facet, "true", "false", strconv.FormatBool(already[facet])}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if err := writeUASTEngineCSV(filepath.Join(dir, "current_uast_elements.csv"), []string{"element_type", "element_id", "implemented"}, func(w *csv.Writer) error {
		structures, relations, fields := uastEngineCurrentElements(b)
		for _, axis := range []struct {
			kind string
			ids  []string
			set  map[string]bool
		}{{"structure", b.StructuralKinds, structures}, {"relation", b.ConcreteRelations, relations}, {"facet", b.Facets, already}, {"field", b.Fields, fields}} {
			for _, id := range axis.ids {
				if err := w.Write([]string{axis.kind, id, strconv.FormatBool(axis.set[id])}); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return err
	}
	preservation, err := UniversalTargetPreservationMatrix()
	if err != nil {
		return err
	}
	return writeUASTEngineCSV(filepath.Join(dir, "target_preservation.csv"), []string{"target_id", "canonical_semantic_id", "direct", "rewrite", "helper", "emulate", "runtime", "error", "tested"}, func(w *csv.Writer) error {
		for col, target := range preservation.Targets {
			for row, facet := range preservation.Capabilities {
				mode := preservation.Status(row, col)
				if err := w.Write([]string{target, facet, strconv.FormatBool(mode == PreservationDirect), strconv.FormatBool(mode == PreservationRewrite), strconv.FormatBool(mode == PreservationHelper), strconv.FormatBool(mode == PreservationEmulate), strconv.FormatBool(mode == PreservationRuntime), strconv.FormatBool(mode == PreservationError), "false"}); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func writeUASTEngineCSV(path string, header []string, write func(*csv.Writer) error) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	if err := w.Write(header); err != nil {
		f.Close()
		return err
	}
	if err := write(w); err != nil {
		f.Close()
		return err
	}
	w.Flush()
	if err := w.Error(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func writeUASTEngineMatrixEdges(w *csv.Writer, rows, cols []string, matrix matrixir.SparseMatrix, kind string) error {
	if matrix.Rows != len(rows) || matrix.Cols != len(cols) {
		return fmt.Errorf("UAST engine adapter matrix dimensions do not match axes")
	}
	var writeErr error
	matrix.Each(func(row, col int, value float64) {
		if writeErr == nil && value != 0 {
			writeErr = w.Write([]string{rows[row], kind, cols[col]})
		}
	})
	return writeErr
}

func uastEngineDirectFacetSet(b UniversalASTBasis) map[string]bool {
	structures := map[string]bool{"NilLiteral": true}
	for _, structure := range directSemanticStructure {
		structures[structure] = true
	}
	out := map[string]bool{}
	for structure := range structures {
		row := indexOf(b.StructuralKinds, structure)
		if row < 0 {
			continue
		}
		for col, facet := range b.Facets {
			if b.StructuralFacetSeed.At(row, col) != 0 {
				out[facet] = true
			}
		}
	}
	return out
}

func uastEngineCurrentElements(b UniversalASTBasis) (map[string]bool, map[string]bool, map[string]bool) {
	structures := map[string]bool{"NilLiteral": true}
	for _, structure := range directSemanticStructure {
		structures[structure] = true
	}
	relations := map[string]bool{}
	for relation := range directlyConsumedUASTRelations {
		relations[relation] = true
	}
	fields := map[string]bool{"source_span": true, "semantic_facets": true}
	for field := range directUASTFields {
		fields[field] = true
	}
	return structures, relations, fields
}
