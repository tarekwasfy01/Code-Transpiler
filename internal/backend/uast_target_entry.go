package backend

import "strings"

// generateTargetFromUniversalExisting is the proven UAST emitter retained as
// the implementation source for the universal projector.  It consumes only
// the read-only UAST graph; TargetSpecs own selection and preflight.
func generateTargetFromUniversalExisting(evaluation, target string, graph *uastExecutionGraph) (string, error) {
	generator := &targetGen{evaluation: evaluation, target: target, declared: []map[string]bool{{}}, funcs: map[string]bool{}, inline: map[string]*FunctionExpr{}, activeInline: map[*FunctionExpr]bool{}, uastFunctions: map[string]int{}, uastInline: map[string]bool{}, uastActiveInline: map[int]bool{}, helperSources: map[string]string{}, directVectors: map[string]bool{}}
	// Native source frontends may use the direct native contracts.  Legacy
	// compatibility inputs (for example R-shaped snippets passed as Python)
	// deliberately keep their established generated-runtime form.
	generator.nativeDirect = graph.document.Origin.SourceLanguage == "go" || graph.document.Metadata["projection_mode"] == "native-direct"
	generator.usedNames = reserveUASTSymbols(graph)
	generator.cValues = map[string]bool{}
	// Function IDs and inline eligibility come directly from UAST flow matrices.
	for _, item := range graph.many(graph.root, "statement") {
		assignment := graph.common[item.ID]
		if assignment.Kind != "assign" {
			continue
		}
		expression, ok, err := graph.one(item.ID, "expression", false)
		if err != nil {
			return "", err
		}
		if !ok || graph.common[expression].Kind != "function" {
			continue
		}
		name := generator.name(assignment.Name)
		generator.funcs[name] = true
		generator.uastFunctions[name] = expression
		_, flowErr := buildUASTFunctionFlow(graph, expression)
		if flowErr != nil && strings.Contains(flowErr.Error(), "before definite assignment") {
			return "", flowErr
		}
		if flowErr == nil && !uastFunctionContainsLoop(graph, expression) {
			generator.uastInline[name] = true
		}
	}
	emit := func() error {
		for _, item := range graph.many(graph.root, "statement") {
			if err := generator.uastStatement(graph, item.ID); err != nil {
				return err
			}
		}
		return nil
	}
	switch target {
	case "python", "julia", "nim", "swift":
		if err := emit(); err != nil {
			return "", err
		}
		body := generator.b.String()
		if nativeSourceWithoutRuntime(target, body, generator.requiredHelperSources()) {
			return nativeTargetPrefix(target) + body, nil
		}
		return targetPrelude(target) + "\n" + renderTargetHelpers(generator.requiredHelperSources()) + "\n" + generator.b.String(), nil
	default:
		generator.line(mainOpen(target))
		generator.indent++
		if err := emit(); err != nil {
			return "", err
		}
		if target == "cpp" || target == "c" {
			generator.line("return 0;")
		}
		generator.indent--
		generator.line(mainClose(target))
		body := generator.b.String()
		if nativeSourceWithoutRuntime(target, body, generator.requiredHelperSources()) {
			return nativeTargetPrefix(target) + body, nil
		}
		return targetPrelude(target) + "\n" + renderTargetHelpers(generator.requiredHelperSources()) + "\n" + body, nil
	}
}

func nativeSourceWithoutRuntime(target, body string, helpers []string) bool {
	if len(helpers) != 0 || strings.Contains(body, "rCall(") || strings.Contains(body, "RValue") || strings.Contains(body, "rNum(") {
		return false
	}
	return target == "go"
}

func nativeTargetPrefix(target string) string {
	switch target {
	case "go":
		return "package main\n\nimport \"fmt\"\n\n"
	case "rust":
		return ""
	default:
		return ""
	}
}
