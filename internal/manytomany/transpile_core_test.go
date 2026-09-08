// Copyright (c) 2026 Tarek Wasfy
package manytomany

import "testing"

func TestTranspileCoreGUIAndCLIParity(t *testing.T) {
	corpus := []struct {
		name, sourceLanguage, targetLanguage, source string
		disableRuntime                               bool
	}{
		{"r-scalar", "r", "go", "x <- 1\nprint(x)", false},
		{"r-index", "r", "go", "x <- c(1, 2)\nprint(x[1])", false},
		{"r-control", "r", "go", "if (TRUE) { print(1) } else { print(0) }", false},
		{"r-function", "r", "go", "f <- function(x) { return(x + 1) }\nprint(f(2))", false},
		// This is intentionally the GUI-reported Go -> C++ aggregate witness.
		// Whether it is emitted natively or through the compatibility runtime is
		// decided in TranspileCore, never by either presentation frontend.
		{"go-aggregate-runtime", "go", "cpp", "package main\nimport \"fmt\"\nfunc main(){ var x []float64 = []float64{1,2,3}; fmt.Println(x * 2) }", false},
		{"go-aggregate-no-runtime", "go", "cpp", "package main\nimport \"fmt\"\nfunc main(){ var x []float64 = []float64{1,2,3}; fmt.Println(x * 2) }", true},
	}
	for _, tc := range corpus {
		t.Run(tc.name, func(t *testing.T) {
			gui, guiErr := TranspileCore(TranspileRequest{Source: tc.source, SourceLanguage: tc.sourceLanguage, TargetLanguage: tc.targetLanguage, EntryPoint: "gui", DisableRuntimeFallback: tc.disableRuntime})
			cli, cliErr := TranspileCore(TranspileRequest{Source: tc.source, SourceLanguage: tc.sourceLanguage, TargetLanguage: tc.targetLanguage, EntryPoint: "cli", DisableRuntimeFallback: tc.disableRuntime})
			if (guiErr != nil) != (cliErr != nil) {
				t.Fatalf("error divergence: gui=%v cli=%v", guiErr, cliErr)
			}
			if gui.Trace.SourceSHA256 != cli.Trace.SourceSHA256 || gui.Trace.UASTSHA256 != cli.Trace.UASTSHA256 || gui.Trace.FinalSourceSHA256 != cli.Trace.FinalSourceSHA256 || gui.Trace.ErrorClass != cli.Trace.ErrorClass || gui.Trace.ProjectionMode != cli.Trace.ProjectionMode || gui.Trace.RuntimeFallback != cli.Trace.RuntimeFallback || gui.Trace.IntermediateRoute != cli.Trace.IntermediateRoute || gui.Code != cli.Code {
				t.Fatalf("core divergence: gui=%+v cli=%+v", gui.Trace, cli.Trace)
			}
		})
	}
}
