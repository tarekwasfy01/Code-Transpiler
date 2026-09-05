package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
	"github.com/tarekwasfy01/Code-Transpiler/internal/manytomany"
)

type output struct {
	Route               string `json:"route,omitempty"`
	ProjectionMode      string `json:"projection_mode,omitempty"`
	ErrorClass          string `json:"error_class,omitempty"`
	Diagnostic          string `json:"diagnostic,omitempty"`
	RuntimeFallback     bool   `json:"runtime_fallback"`
	DirectSuccess       bool   `json:"direct_success"`
	LoweringSuccess     bool   `json:"primitive_lowering_success"`
	IntermediateSuccess bool   `json:"intermediate_success"`
	FinalSourceHash     string `json:"final_source_hash,omitempty"`
	UASTHash            string `json:"uast_hash,omitempty"`
}

func main() {
	sourceLang := flag.String("source-language", "", "source language")
	targetLang := flag.String("target-language", "", "target language")
	sourceFile := flag.String("source-file", "", "source file")
	flag.Parse()
	data, err := os.ReadFile(*sourceFile)
	if err != nil {
		fmt.Printf(`{"error_class":"SOURCE_READ","diagnostic":%q}`, err.Error())
		return
	}
	res, err := manytomany.TranspileCore(manytomany.TranspileRequest{Source: string(data), SourceLanguage: *sourceLang, TargetLanguage: *targetLang, EntryPoint: "target-closure-child"})
	tr := res.Trace
	out := output{ProjectionMode: tr.ProjectionMode, RuntimeFallback: tr.RuntimeFallback, DirectSuccess: err == nil && !tr.UniversalLoweringSuccess && tr.IntermediateRoute == "" && !tr.RuntimeFallback, LoweringSuccess: tr.UniversalLoweringSuccess, IntermediateSuccess: tr.IntermediateRoute != "", FinalSourceHash: tr.FinalSourceSHA256, UASTHash: tr.UASTSHA256}
	if out.DirectSuccess {
		out.Route = "DIRECT"
	} else if out.LoweringSuccess {
		out.Route = "PRIMITIVE_LOWERING"
	} else if out.IntermediateSuccess {
		out.Route = "INTERMEDIATE"
	} else if out.RuntimeFallback {
		out.Route = "RUNTIME"
	} else if err != nil {
		out.Route = "STILL_FAILED"
	} else {
		out.Route = "DIRECT"
	}
	if err != nil {
		out.ErrorClass = string(backend.FailureClassOf(err))
		out.Diagnostic = err.Error()
	}
	b, _ := json.Marshal(out)
	fmt.Print(string(b))
}
