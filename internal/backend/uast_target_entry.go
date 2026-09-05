package backend

import (
	"fmt"
	"strings"
)

// generateTargetFromUniversalExisting is the native UAST renderer retained as
// the implementation source for the universal projector. It consumes only the
// read-only UAST graph; TargetSpecs own selection and preflight.
func generateTargetFromUniversalExisting(evaluation, target string, graph *uastExecutionGraph) (string, error) {
	return generateTargetFromUniversalMode(evaluation, target, graph, true)
}

// generateTargetFromUniversalHybrid keeps native emission as the default but
// permits an individual unsupported UAST statement to cross the existing
// compatibility boundary.  It is selected only after a strict direct attempt
// has identified a concrete DIRECT_NATIVE_UNAVAILABLE contract gap.
func generateTargetFromUniversalHybrid(evaluation, target string, graph *uastExecutionGraph) (string, error) {
	return generateTargetFromUniversalMode(evaluation, target, graph, true, true)
}

// generateTargetFromUniversalCompatibility is intentionally private. It is
// the one explicit UAST -> target compatibility entrance. It does not receive
// a route mode from CLI or manytomany.
func generateTargetFromUniversalCompatibility(evaluation, target string, graph *uastExecutionGraph) (string, error) {
	return generateTargetFromUniversalMode(evaluation, target, graph, false)
}

func generateTargetFromUniversalMode(evaluation, target string, graph *uastExecutionGraph, nativeDirect bool, hybrid ...bool) (string, error) {
	generator := &targetGen{evaluation: evaluation, target: target, declared: []map[string]bool{{}}, funcs: map[string]bool{}, inline: map[string]*FunctionExpr{}, activeInline: map[*FunctionExpr]bool{}, uastFunctions: map[string]int{}, uastInline: map[string]bool{}, uastActiveInline: map[int]bool{}, helperSources: map[string]string{}, directVectors: map[string]bool{}}
	generator.nativeDirect = nativeDirect
	generator.hybridFallback = len(hybrid) > 0 && hybrid[0]
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
		if generator.hybridFallback && generator.runtimeUsed {
			return targetPreludeExisting(target) + "\n" + renderTargetHelpers(generator.requiredHelperSources()) + "\n" + body, nil
		}
		if nativeSourceWithoutRuntime(target, body, generator.requiredHelperSources()) {
			return nativeTargetPrefix(target) + renderTargetHelpers(generator.requiredHelperSources()) + "\n" + body, nil
		}
		if generator.nativeDirect {
			return "", fmt.Errorf("DIRECT_NATIVE_UNAVAILABLE: target %s emitted %s", target, nativeRuntimeMarker(body, generator.requiredHelperSources()))
		}
		// Compatibility is the explicit runtime last resort. TargetSpec keeps
		// syntax-only imports for the native path, so use the established
		// complete runtime prelude here rather than returning unresolved r_* calls.
		return targetPreludeExisting(target) + "\n" + renderTargetHelpers(generator.requiredHelperSources()) + "\n" + generator.b.String(), nil
	default:
		generator.line(nativeMainOpen(target))
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
		if generator.hybridFallback && generator.runtimeUsed {
			return targetPreludeExisting(target) + "\n" + renderTargetHelpers(generator.requiredHelperSources()) + "\n" + body, nil
		}
		if nativeSourceWithoutRuntime(target, body, generator.requiredHelperSources()) {
			return nativeTargetPrefix(target) + renderTargetHelpers(generator.requiredHelperSources()) + "\n" + body, nil
		}
		if generator.nativeDirect {
			return "", fmt.Errorf("DIRECT_NATIVE_UNAVAILABLE: target %s emitted %s", target, nativeRuntimeMarker(body, generator.requiredHelperSources()))
		}
		return targetPreludeExisting(target) + "\n" + renderTargetHelpers(generator.requiredHelperSources()) + "\n" + body, nil
	}
}

func nativeSourceWithoutRuntime(_ string, body string, helpers []string) bool {
	return !AnalyzeRuntimeTaint(body, helpers).Tainted()
}

func nativeRuntimeMarker(body string, helpers []string) string {
	taint := AnalyzeRuntimeTaint(body, helpers)
	if taint.Tainted() {
		return "runtime artifact " + strings.Join(taint.Artifacts, ",")
	}
	return ""
}

// The canonical entry must not inherit compatibility initialization from
// mainOpen.  Only the enclosing target syntax belongs here.
func nativeMainOpen(target string) string {
	switch target {
	case "cpp":
		return "int main() {"
	case "c":
		return "int main(void) {"
	default:
		return mainOpen(target)
	}
}

func nativeTargetPrefix(target string) string {
	switch target {
	case "go":
		return "package main\n\nimport (\n    \"fmt\"\n    \"math\"\n)\n\n"
	case "rust":
		return ""
	case "cpp":
		return `#include <iostream>
#include <vector>
#include <cmath>

template <typename T>
static void uast_print_one(const T& value) { std::cout << value; }

template <typename T>
static void uast_print_one(const std::vector<T>& values) {
    std::cout << "[";
    for (size_t i = 0; i < values.size(); ++i) {
        if (i) std::cout << ", ";
        uast_print_one(values[i]);
    }
    std::cout << "]";
}

template <typename... Values>
static void uast_print(const Values&... values) {
    bool first = true;
    ((std::cout << (first ? "" : " "), first = false, uast_print_one(values)), ...);
    std::cout << std::endl;
}

`
	case "c":
		return "#include <stdio.h>\n#include <stdbool.h>\n#include <math.h>\n\n"
	case "zig":
		return "const std = @import(\"std\");\n\n"
	case "csharp":
		return "using System;\nusing System.Linq;\n\n"
	case "java":
		return "import java.util.Arrays;\n\n"
	default:
		return ""
	}
}
