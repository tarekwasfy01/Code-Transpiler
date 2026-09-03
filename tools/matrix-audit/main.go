// matrix-audit exposes the existing transpilers without the GUI/runtimeassets dependency.
// It is an audit adapter, not an alternate implementation of translation.
package main

import (
	"encoding/json"
	"fmt"
	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
	"github.com/tarekwasfy01/Code-Transpiler/internal/manytomany"
	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
	"os"
)

type Request struct {
	ID              string   `json:"id"`
	Source          string   `json:"source"`
	Target          string   `json:"target"`
	Code            string   `json:"code"`
	Mode            string   `json:"mode"`
	MaxTokens       int      `json:"max_tokens,omitempty"`
	Targets         []string `json:"targets,omitempty"`
	IncludeSemantic bool     `json:"include_semantic,omitempty"`
}
type Response struct {
	ID       string                         `json:"id"`
	Code     string                         `json:"code,omitempty"`
	Error    string                         `json:"error,omitempty"`
	Analysis *GraphAnalysis                 `json:"analysis,omitempty"`
	Flows    []backend.FunctionFlowEvidence `json:"flows,omitempty"`
	Semantic *backend.SemanticProgram       `json:"semantic,omitempty"`
	Results  []Response                     `json:"results,omitempty"`
}
type GraphAnalysis struct {
	Semantic              matrixir.Vector `json:"semantic"`
	Features              matrixir.Vector `json:"features"`
	RelationCounts        matrixir.Vector `json:"relation_counts"`
	RelationKnown         matrixir.Vector `json:"relation_known"`
	NodeCount             int             `json:"node_count"`
	GraphError            string          `json:"graph_error,omitempty"`
	LexicalNodeMatrix     matrixir.Matrix `json:"lexical_node_matrix"`
	LexicalAxisMatrix     matrixir.Matrix `json:"lexical_axis_matrix"`
	LexicalControlMatrix  matrixir.Matrix `json:"lexical_control_matrix"`
	LexicalRelationCounts matrixir.Vector `json:"lexical_relation_counts"`
	TokenClasses          []int           `json:"token_classes"`
}

func apply(r Request) (out Response) {
	out.ID = r.ID
	defer func() {
		if p := recover(); p != nil {
			out.Error = fmt.Sprintf("panic: %v", p)
		}
	}()
	if r.MaxTokens > 0 {
		n := len(matrixir.Tokenize(r.Source, r.Code))
		if n > r.MaxTokens {
			out.Error = fmt.Sprintf("RESOURCE_LIMIT: lexical matrix has %d tokens, audit limit %d", n, r.MaxTokens)
			return
		}
	}
	if r.Mode == "analyze" {
		semantic := matrixir.AnalyzeExpression(r.Source, r.Code)
		features, err := matrixir.FeatureRequirements(semantic)
		if err != nil {
			out.Error = err.Error()
			return
		}
		analysis := &GraphAnalysis{
			Semantic: semantic, Features: features,
			RelationCounts: make(matrixir.Vector, matrixir.RelationCount),
			RelationKnown:  make(matrixir.Vector, matrixir.RelationCount),
		}
		lexicalGraph, lexemes, lexicalErr := matrixir.BuildLexicalGraph(r.Source, r.Code)
		if lexicalErr != nil {
			out.Error = lexicalErr.Error()
			return
		}
		analysis.LexicalNodeMatrix = lexicalGraph.NodeMatrix()
		analysis.LexicalAxisMatrix = matrixir.NewMatrix(len(lexemes), matrixir.LexicalAxes)
		analysis.LexicalControlMatrix = lexicalGraph.Edges[matrixir.Control].Dense()
		analysis.LexicalRelationCounts = lexicalGraph.RelationCounts()
		analysis.TokenClasses = make([]int, len(lexemes))
		for i, lexeme := range lexemes {
			analysis.TokenClasses[i] = int(lexeme.Class)
			copy(analysis.LexicalAxisMatrix.Data[i*analysis.LexicalAxisMatrix.Cols:(i+1)*analysis.LexicalAxisMatrix.Cols], lexeme.Axes)
		}
		program, graphErr := manytomany.Parse(r.Source, r.Code)
		if graphErr != nil {
			analysis.GraphError = graphErr.Error()
		} else {
			analysis.RelationCounts = program.Graph.RelationCounts()
			for i := range analysis.RelationKnown {
				analysis.RelationKnown[i] = 1
			}
			analysis.NodeCount = len(program.Graph.Nodes)
		}
		out.Analysis = analysis
		return
	}
	if r.Mode == "fanout" {
		program, err := manytomany.Parse(r.Source, r.Code)
		if err != nil {
			out.Error = err.Error()
			return
		}
		if r.IncludeSemantic {
			out.Semantic = program.Semantic
		}
		for _, target := range r.Targets {
			result := func() (v Response) {
				v.ID = target
				defer func() {
					if p := recover(); p != nil {
						v.Error = fmt.Sprintf("panic: %v", p)
					}
				}()
				code, err := manytomany.Emit(target, program)
				if err != nil {
					v.Error = err.Error()
				} else {
					v.Code = code
				}
				return
			}()
			out.Results = append(out.Results, result)
		}
		return
	}
	if r.Mode == "canonical" {
		program, err := manytomany.Parse(r.Source, r.Code)
		if err != nil {
			out.Error = err.Error()
			return
		}
		// Compatibility view only. The translation path itself does not retain
		// Canonical R after Parse has built SemanticProgram.
		out.Code, err = program.Semantic.RSource(false)
		if err != nil {
			out.Error = err.Error()
		}
		return
	}
	if r.Mode == "semantic" {
		program, err := manytomany.Parse(r.Source, r.Code)
		if err != nil {
			out.Error = err.Error()
			return
		}
		out.Semantic = program.Semantic
		return
	}
	if r.Mode == "semantic-document" {
		program, err := manytomany.Parse(r.Source, r.Code)
		if err != nil {
			out.Error = err.Error()
			return
		}
		data, err := program.Semantic.MarshalSemanticJSON()
		if err != nil {
			out.Error = err.Error()
			return
		}
		out.Code = string(data)
		return
	}
	if r.Mode == "from-semantic-document" {
		program, err := manytomany.ParseDocument([]byte(r.Code))
		if err != nil {
			out.Error = err.Error()
			return
		}
		out.Code, err = manytomany.Emit(r.Target, program)
		if err != nil {
			out.Error = err.Error()
		}
		return
	}
	if r.Mode == "function-flow" {
		program, err := manytomany.Parse(r.Source, r.Code)
		if err != nil {
			out.Error = err.Error()
			return
		}
		out.Flows, err = backend.AnalyzeSemanticFunctionFlows(program.Semantic)
		if err != nil {
			out.Error = err.Error()
		}
		return
	}
	var err error
	if r.Mode == "embedded" {
		out.Code, err = backend.Run(r.Code)
	} else {
		out.Code, err = manytomany.Transpile(r.Source, r.Target, r.Code)
	}
	if err != nil {
		out.Error = err.Error()
	}
	return
}
func main() {
	var requests []Request
	if err := json.NewDecoder(os.Stdin).Decode(&requests); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	results := make([]Response, len(requests))
	for i, r := range requests {
		results[i] = apply(r)
	}
	if err := json.NewEncoder(os.Stdout).Encode(results); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
