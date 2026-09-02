package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
)

type legacySite struct {
	Path           string `json:"path"`
	Line           int    `json:"line"`
	Caller         string `json:"caller"`
	Symbol         string `json:"symbol"`
	Classification string `json:"classification"`
	Boundary       bool   `json:"boundary_conversion_site"`
	Text           string `json:"text"`
}

func main() {
	out := flag.String("out", filepath.FromSlash("outputs/uast-direct-execution"), "output directory")
	flag.Parse()
	report, err := backend.UniversalDirectExecutionReport()
	must(err)
	sites, err := scanLegacySites(filepath.FromSlash("internal/backend"))
	must(err)
	boundarySites := 0
	for _, site := range sites {
		if site.Boundary {
			boundarySites++
		}
	}
	must(os.MkdirAll(*out, 0o755))
	must(writeJSON(filepath.Join(*out, "direct-execution-matrix.json"), map[string]any{"report": report, "legacy_conversion_sites": sites}))
	must(writeDirectCSV(filepath.Join(*out, "direct-execution-matrix.csv"), report))
	must(writeJSON(filepath.Join(*out, "target-capability-matrix.json"), report.TargetCapabilities))
	must(writeCapabilityCSV(filepath.Join(*out, "target-capability-matrix.csv"), report.TargetCapabilities))
	must(writeJSON(filepath.Join(*out, "end-to-end-capability.json"), report.EndToEnd))
	must(writeEndToEndCSV(filepath.Join(*out, "end-to-end-capability.csv"), report.EndToEnd))
	must(writeJSON(filepath.Join(*out, "backend-audit.json"), report.BackendAudit))
	must(writeBackendAuditCSV(filepath.Join(*out, "backend-audit.csv"), report.BackendAudit))
	must(writeJSON(filepath.Join(*out, "legacy-adapters.json"), map[string]any{"remaining_conversion_sites": boundarySites, "all_compatibility_call_observations": len(sites), "sites": sites, "backend_paths": report.BackendPaths}))
	must(writeLegacyCSV(filepath.Join(*out, "legacy-adapters.csv"), sites))
	coverage := map[string]any{"schema": report.Schema, "basis_sha256": report.BasisSHA256, "summary": report.Summary, "backend_paths": report.BackendPaths, "remaining_legacy_conversion_sites": boundarySites, "compatibility_call_observations": len(sites)}
	must(writeJSON(filepath.Join(*out, "coverage.json"), coverage))
	must(writeCoverageCSV(filepath.Join(*out, "coverage.csv"), report, boundarySites))
	fmt.Printf("UAST_DIRECT_REPORT=%s\nLEGACY_CONVERSION_SITES=%d\nCOMPATIBILITY_CALL_OBSERVATIONS=%d\n", *out, boundarySites, len(sites))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func scanLegacySites(root string) ([]legacySite, error) {
	legacy := map[string]bool{"SemanticDocumentFromUniversalAST": true, "legacyExecutableBodyFromUniversal": true, "documentStatementAST": true, "documentExpressionAST": true, "installLegacyProgramView": true}
	internal := map[string]bool{"SemanticDocumentFromUniversalAST": true, "legacyExecutableBodyFromUniversal": true, "semanticBodyFromUniversal": true, "documentStatementAST": true, "documentStatementPointerAST": true, "documentExpressionAST": true, "documentArgumentsAST": true, "installLegacyProgramView": true, "refreshLegacyExecutableBodyView": true}
	sites := []legacySite{}
	fset := token.NewFileSet()
	packages, err := parser.ParseDir(fset, root, func(info os.FileInfo) bool {
		return strings.HasSuffix(info.Name(), ".go") && !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		return nil, err
	}
	lineCache := map[string][]string{}
	for _, pkg := range packages {
		for _, file := range pkg.Files {
			for _, declaration := range file.Decls {
				fn, ok := declaration.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				ast.Inspect(fn.Body, func(node ast.Node) bool {
					call, ok := node.(*ast.CallExpr)
					if !ok {
						return true
					}
					name := ""
					switch value := call.Fun.(type) {
					case *ast.Ident:
						name = value.Name
					case *ast.SelectorExpr:
						name = value.Sel.Name
					}
					classification := ""
					if legacy[name] {
						classification = "uast_or_document_to_legacy"
					} else if name == "Document" {
						classification = "compatibility_document_request"
					} else {
						return true
					}
					position := fset.Position(call.Pos())
					path := filepath.Clean(position.Filename)
					lines, ok := lineCache[path]
					if !ok {
						if data, readErr := os.ReadFile(path); readErr == nil {
							lines = strings.Split(string(data), "\n")
							lineCache[path] = lines
						}
					}
					text := ""
					if position.Line > 0 && position.Line <= len(lines) {
						text = strings.TrimSpace(lines[position.Line-1])
					}
					boundary := legacy[name] && !internal[fn.Name.Name]
					// These two functions only materialize read-only compatibility/API
					// views; they are not semantic conversion boundaries.
					if name == "installLegacyProgramView" || name == "refreshLegacyExecutableBodyView" {
						boundary = false
					}
					if name == "SemanticDocumentFromUniversalAST" && (fn.Name.Name == "validateUniversalASTCompatibility" || fn.Name.Name == "validateExecutableUniversalProjection" || fn.Name.Name == "documentFromCanonicalUniversalAST") {
						boundary = false
					}
					if fn.Name.Name == "LowerNativeGo" && name == "documentStatementAST" {
						boundary = false
						classification = "legacy_ingress_adapter"
					}
					if fn.Name.Name == "ParseSemanticDocument" {
						boundary = false
						classification = "legacy_ingress_adapter"
					}
					if fn.Name.Name == "materializeLegacyNativeGo" {
						boundary = false
						classification = "test_reference_legacy_renderer"
					}
					sites = append(sites, legacySite{Path: filepath.ToSlash(path), Line: position.Line, Caller: fn.Name.Name, Symbol: name, Classification: classification, Boundary: boundary, Text: text})
					return true
				})
			}
		}
	}
	for i := range sites {
		if relative, relErr := filepath.Rel(".", sites[i].Path); relErr == nil {
			sites[i].Path = filepath.ToSlash(relative)
		}
	}
	sort.Slice(sites, func(i, j int) bool {
		if sites[i].Path == sites[j].Path {
			return sites[i].Line < sites[j].Line
		}
		return sites[i].Path < sites[j].Path
	})
	return sites, err
}

func csvFile(path string) (*os.File, *csv.Writer, error) {
	file, err := os.Create(path)
	if err != nil {
		return nil, nil, err
	}
	return file, csv.NewWriter(file), nil
}

func writeDirectCSV(path string, report backend.UASTDirectExecutionReport) error {
	file, writer, err := csvFile(path)
	if err != nil {
		return err
	}
	defer file.Close()
	defer writer.Flush()
	if err := writer.Write([]string{"category", "name", "projected", "currently_direct", "currently_via_legacy_adapter", "representable_not_executable", "stored_only", "direct_targets", "lowerable_targets", "runtime_required_targets", "unsupported_targets", "unknown_targets"}); err != nil {
		return err
	}
	for _, item := range report.Items {
		if err := writer.Write([]string{item.Category, item.Name, strconv.FormatBool(item.Projected), strconv.FormatBool(item.CurrentlyDirect), strconv.FormatBool(item.CurrentlyViaLegacyAdapter), strconv.FormatBool(item.RepresentableNotExecutable), strconv.FormatBool(item.StoredOnly), strings.Join(item.DirectTargets, ";"), strings.Join(item.LowerableTargets, ";"), strings.Join(item.RuntimeRequiredTargets, ";"), strings.Join(item.UnsupportedTargets, ";"), strings.Join(item.UnknownTargets, ";")}); err != nil {
			return err
		}
	}
	return writer.Error()
}

func statusRows(category string, plane backend.UASTCapabilityPlane) [][]string {
	rows := [][]string{}
	for row, item := range plane.Rows {
		for col, target := range plane.Targets {
			rows = append(rows, []string{category, item, target, string(plane.Status(row, col))})
		}
	}
	return rows
}

func writeCapabilityCSV(path string, matrix backend.UASTTargetCapabilityMatrix) error {
	file, writer, err := csvFile(path)
	if err != nil {
		return err
	}
	defer file.Close()
	defer writer.Flush()
	if err := writer.Write([]string{"category", "item", "target", "status"}); err != nil {
		return err
	}
	rows := append(statusRows("structure", matrix.Structures), statusRows("facet", matrix.Facets)...)
	rows = append(rows, statusRows("relation", matrix.Relations)...)
	rows = append(rows, statusRows("field", matrix.Fields)...)
	for _, row := range rows {
		if err := writer.Write(row); err != nil {
			return err
		}
	}
	return writer.Error()
}

func writeEndToEndCSV(path string, report backend.UASTEndToEndCapabilityReport) error {
	file, writer, err := csvFile(path)
	if err != nil {
		return err
	}
	defer file.Close()
	defer writer.Flush()
	if err := writer.Write([]string{"category", "item", "representable", "frontend", "enrichment", "evidence", "normalize", "backend", "target", "test", "full", "representation_only", "frontend_gap", "enrichment_gap", "evidence_gap", "backend_gap", "target_gap", "test_gap"}); err != nil {
		return err
	}
	planes := map[string]backend.UASTEndToEndCapabilityPlane{
		"structure": report.Structures, "relation": report.Relations,
		"facet": report.Facets, "field": report.Fields,
	}
	categories := []string{"structure", "relation", "facet", "field"}
	for _, category := range categories {
		plane := planes[category]
		for row, item := range plane.Rows {
			value := func(matrixValue float64) string { return strconv.FormatBool(matrixValue != 0) }
			values := []float64{plane.Representable.At(row, 0), plane.Frontend.At(row, 0), plane.Enrichment.At(row, 0), plane.Evidence.At(row, 0), plane.Normalize.At(row, 0), plane.Backend.At(row, 0), plane.Target.At(row, 0), plane.Test.At(row, 0), plane.Full.At(row, 0), plane.RepresentationOnly.At(row, 0), plane.FrontendGap.At(row, 0), plane.EnrichmentGap.At(row, 0), plane.EvidenceGap.At(row, 0), plane.BackendGap.At(row, 0), plane.TargetGap.At(row, 0), plane.TestGap.At(row, 0)}
			record := []string{category, item}
			for _, matrixValue := range values {
				record = append(record, value(matrixValue))
			}
			if err := writer.Write(record); err != nil {
				return err
			}
		}
	}
	return writer.Error()
}

func writeBackendAuditCSV(path string, report backend.UASTBackendAuditReport) error {
	file, writer, err := csvFile(path)
	if err != nil {
		return err
	}
	defer file.Close()
	defer writer.Flush()
	if err := writer.Write([]string{"target", "uast_direct", "legacy_dependency", "source_language_dependency", "structures_direct", "relations_direct", "facets_direct", "fields_direct"}); err != nil {
		return err
	}
	for _, target := range report.Targets {
		if err := writer.Write([]string{target.Language, strconv.FormatBool(target.UASTDirect), strconv.FormatBool(target.LegacyDependency), strconv.FormatBool(target.SourceLanguageDependency), strconv.Itoa(target.StructureCapabilities["direct"]), strconv.Itoa(target.RelationCapabilities["direct"]), strconv.Itoa(target.FacetCapabilities["direct"]), strconv.Itoa(target.FieldCapabilities["direct"])}); err != nil {
			return err
		}
	}
	return writer.Error()
}

func writeLegacyCSV(path string, sites []legacySite) error {
	file, writer, err := csvFile(path)
	if err != nil {
		return err
	}
	defer file.Close()
	defer writer.Flush()
	if err := writer.Write([]string{"path", "line", "caller", "symbol", "classification", "boundary_conversion_site", "text"}); err != nil {
		return err
	}
	for _, site := range sites {
		if err := writer.Write([]string{site.Path, strconv.Itoa(site.Line), site.Caller, site.Symbol, site.Classification, strconv.FormatBool(site.Boundary), site.Text}); err != nil {
			return err
		}
	}
	return writer.Error()
}

func writeCoverageCSV(path string, report backend.UASTDirectExecutionReport, legacy int) error {
	file, writer, err := csvFile(path)
	if err != nil {
		return err
	}
	defer file.Close()
	defer writer.Flush()
	if err := writer.Write([]string{"category", "metric", "count"}); err != nil {
		return err
	}
	categories := make([]string, 0, len(report.Summary))
	for category := range report.Summary {
		categories = append(categories, category)
	}
	sort.Strings(categories)
	for _, category := range categories {
		metrics := make([]string, 0, len(report.Summary[category]))
		for metric := range report.Summary[category] {
			metrics = append(metrics, metric)
		}
		sort.Strings(metrics)
		for _, metric := range metrics {
			if err := writer.Write([]string{category, metric, strconv.Itoa(report.Summary[category][metric])}); err != nil {
				return err
			}
		}
	}
	if err := writer.Write([]string{"legacy", "remaining_conversion_sites", strconv.Itoa(legacy)}); err != nil {
		return err
	}
	return writer.Error()
}
