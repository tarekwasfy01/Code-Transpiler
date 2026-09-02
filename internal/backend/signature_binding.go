package backend

import (
	"fmt"

	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

// SignatureParameter describes call binding, independently of evaluation and
// value/reference passing. Defaults must be evaluated by the source contract.
type SignatureParameter struct {
	Name       string `json:"name"`
	Passing    string `json:"passing"`
	HasDefault bool   `json:"has_default"`
}

// SignatureArgument columns preserve original evaluation order. Spread inputs
// must be expanded by a semantic runtime before binding, never guessed here.
type SignatureArgument struct {
	Name   string `json:"name,omitempty"`
	Spread bool   `json:"spread,omitempty"`
}

type SignatureBinding struct {
	ParameterArguments matrixir.SparseMatrix `json:"parameter_arguments"`
	UseDefaults        []int                 `json:"use_defaults"`
	ArgumentCounts     []int                 `json:"argument_counts"`
}

// BindSignature derives a parameter x argument incidence matrix. Matrix row
// sums then check required/default/variadic cardinalities for the whole call.
// It does not execute defaults, annotation expressions or function bodies.
func BindSignature(parameters []SignatureParameter, arguments []SignatureArgument) (*SignatureBinding, error) {
	rank := map[string]int{"positional_only": 0, "positional_or_keyword": 1, "variadic_positional": 2, "keyword_only": 3, "variadic_keyword": 4}
	names := map[string]int{}
	var positional []int
	varPos, varKw, lastRank := -1, -1, -1
	optionalPos := false
	for i, p := range parameters {
		order, ok := rank[p.Passing]
		if !ok || p.Name == "" {
			return nil, fmt.Errorf("invalid signature parameter %d", i)
		}
		if _, ok := names[p.Name]; ok {
			return nil, fmt.Errorf("duplicate parameter %q", p.Name)
		}
		if order < lastRank {
			return nil, fmt.Errorf("parameter %q has invalid mode order", p.Name)
		}
		lastRank = order
		names[p.Name] = i
		switch p.Passing {
		case "positional_only", "positional_or_keyword":
			if optionalPos && !p.HasDefault {
				return nil, fmt.Errorf("required positional parameter after default")
			}
			optionalPos = optionalPos || p.HasDefault
			positional = append(positional, i)
		case "variadic_positional":
			if varPos >= 0 || p.HasDefault {
				return nil, fmt.Errorf("invalid variadic positional parameter")
			}
			varPos = i
		case "variadic_keyword":
			if varKw >= 0 || p.HasDefault {
				return nil, fmt.Errorf("invalid variadic keyword parameter")
			}
			varKw = i
		}
	}
	b := &SignatureBinding{ParameterArguments: matrixir.NewSparseMatrix(len(parameters), len(arguments)), UseDefaults: make([]int, len(parameters)), ArgumentCounts: make([]int, len(parameters))}
	nextPos := 0
	keywords := map[string]bool{}
	for col, arg := range arguments {
		if arg.Spread {
			return nil, fmt.Errorf("argument %d requires runtime spread expansion", col)
		}
		row := -1
		if arg.Name == "" {
			if nextPos < len(positional) {
				row = positional[nextPos]
				nextPos++
			} else {
				row = varPos
			}
		} else {
			if keywords[arg.Name] {
				return nil, fmt.Errorf("duplicate keyword %q", arg.Name)
			}
			keywords[arg.Name] = true
			if i, ok := names[arg.Name]; ok && (parameters[i].Passing == "positional_or_keyword" || parameters[i].Passing == "keyword_only") {
				row = i
			} else {
				row = varKw
			}
		}
		if row < 0 {
			return nil, fmt.Errorf("argument %d has no accepting parameter", col)
		}
		b.ParameterArguments.Set(row, col, 1)
	}
	ones := matrixir.NewSparseMatrix(len(arguments), 1)
	for i := range arguments {
		ones.Set(i, 0, 1)
	}
	counts, err := b.ParameterArguments.Multiply(ones)
	if err != nil {
		return nil, err
	}
	for i, p := range parameters {
		count := int(counts.At(i, 0))
		b.ArgumentCounts[i] = count
		if p.Passing == "variadic_positional" || p.Passing == "variadic_keyword" {
			continue
		}
		if count > 1 {
			return nil, fmt.Errorf("parameter %q supplied more than once", p.Name)
		}
		if count == 0 {
			if !p.HasDefault {
				return nil, fmt.Errorf("missing required parameter %q", p.Name)
			}
			b.UseDefaults[i] = 1
		}
	}
	return b, nil
}
