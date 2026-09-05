// frontend-closure-report prints the computed frontend closure axes from the
// productive parser/UAST implementation. It is intentionally read-only: the
// report is derived from the same matrices used by the canonical path.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

func main() {
	debugSource := flag.String("debug-source", "", "lower a source string and print its canonical UAST")
	debugLanguage := flag.String("debug-language", "python", "source language for -debug-source")
	flag.Parse()
	if *debugSource != "" {
		program, err := backend.LowerMatrixLanguage(*debugLanguage, *debugSource)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		data, _ := json.MarshalIndent(program.UniversalAST, "", "  ")
		fmt.Println(string(data))
		return
	}
	m, err := backend.FrontendClosureMatrices()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	result := map[string]any{
		"languages":                              len(m.Languages),
		"old_generic_matrixir_form_axis":         m.OldGenericMatrixIRAxis,
		"true_source_parser_form_union":          m.SourceParserFormUnion,
		"true_source_parser_language_form_cells": m.SourceParserLanguageFormCells,
		"source_to_matrixir_cells":               m.SourceToMatrixIR.NonZeros(),
		"matrixir_form_axis":                     len(m.MatrixIRForms),
		"source_parser_gap_initial":              m.SourceParserGapInitial,
		"source_parser_gap_final":                m.SourceParserGapFinal,
		"silent_source_parser_drop":              m.SilentSourceParserDrop,
		"invalid_parser_evidence_rows":           m.InvalidParserEvidenceRows,
		"identical_language_axis_warning":        m.IdenticalLanguageAxisWarning,
		"old_accepted_form_axis":                 len(m.OldAxisForms),
		"true_parser_form_axis":                  len(m.Forms),
		"accepted_cells":                         m.Accept.NonZeros(),
		"parser_accepted_cells":                  m.ParserAccept.NonZeros(),
		"parser_accepted_language_form_cells":    m.ParserAccept.NonZeros(),
		"accounted_cells":                        m.Accounted.NonZeros(),
		"accounted_language_form_cells":          m.Accounted.NonZeros(),
		"accepted_forms":                         len(m.Forms),
		"new_parser_forms_discovered":            len(newParserForms(m.OldAxisForms, m.Forms)),
		"node_gap_initial":                       m.NodeGapInitial,
		"node_gap_final":                         m.NodeGapFinal,
		"semantic_features":                      len(m.Features),
		"semantic_required_cells":                m.Required.NonZeros(),
		"semantic_have_cells":                    m.Have.NonZeros(),
		"relation_patterns":                      len(m.Patterns),
		"relation_required_cells":                m.RelationRequired.NonZeros(),
		"relation_have_cells":                    m.RelationHave.NonZeros(),
		"node_gap":                               m.NodeGap,
		"semantic_gap":                           m.SemanticGap,
		"relation_gap":                           m.RelationGap,
		"phase_gap":                              m.PhaseGap,
		"dependency_gap":                         m.DependencyGap,
		"unowned_source_surface_final":           m.UnownedSurface,
		"language_specific_preserved":            m.LanguageSpecificPreserved,
		"unknown_semantic_forms":                 m.UnknownSemanticForms,
		"structural_frontend_complete":           m.StructuralComplete,
		"semantic_frontend_complete":             m.SemanticComplete,
		"frontend100":                            m.SourceParserGapFinal == 0 && m.NodeGap == 0 && m.SemanticGap == 0 && m.RelationGap == 0 && m.PhaseGap == 0 && m.DependencyGap == 0 && m.UnownedSurface == 0,
		"cross_language_emit_safe":               m.CrossLanguageEmitSafe,
		"old_axis_complete":                      m.OldAxisComplete,
		"per_language":                           m.PerLanguage,
		"parser_evidence":                        m.ParserEvidence,
		"semantic_families":                      m.Families,
		"semantic_gaps":                          gaps(m.Forms, m.Features, m.Required, m.Have),
		"relation_gaps":                          gaps(m.Forms, m.Patterns, m.RelationRequired, m.RelationHave),
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(data))
}

func newParserForms(oldForms, parserForms []string) []string {
	old := map[string]bool{}
	for _, f := range oldForms {
		old[f] = true
	}
	out := make([]string, 0)
	for _, f := range parserForms {
		if !old[f] {
			out = append(out, f)
		}
	}
	return out
}

func gaps(rows, cols []string, required, have matrixir.SparseMatrix) []string {
	var out []string
	for r := range rows {
		for c := range cols {
			if required.At(r, c) != 0 && have.At(r, c) == 0 {
				out = append(out, rows[r]+"/"+cols[c])
			}
		}
	}
	return out
}
