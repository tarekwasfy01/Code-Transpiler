package manytomany

import "testing"

func TestTranspileCoreGUIAndCLIParity(t *testing.T) {
	corpus := []string{
		"x <- 1\nprint(x)",
		"x <- c(1, 2)\nprint(x[1])",
		"if (TRUE) { print(1) } else { print(0) }",
		"f <- function(x) { return(x + 1) }\nprint(f(2))",
	}
	for _, source := range corpus {
		gui, guiErr := TranspileCore(TranspileRequest{Source: source, SourceLanguage: "r", TargetLanguage: "go", EntryPoint: "gui"})
		cli, cliErr := TranspileCore(TranspileRequest{Source: source, SourceLanguage: "r", TargetLanguage: "go", EntryPoint: "cli"})
		if (guiErr != nil) != (cliErr != nil) {
			t.Fatalf("error divergence for %q: gui=%v cli=%v", source, guiErr, cliErr)
		}
		if gui.Trace.SourceSHA256 != cli.Trace.SourceSHA256 || gui.Trace.UASTSHA256 != cli.Trace.UASTSHA256 || gui.Trace.FinalSourceSHA256 != cli.Trace.FinalSourceSHA256 || gui.Trace.ErrorClass != cli.Trace.ErrorClass || gui.Code != cli.Code {
			t.Fatalf("core divergence for %q: gui=%+v cli=%+v", source, gui.Trace, cli.Trace)
		}
	}
}
