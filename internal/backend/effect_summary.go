package backend

// SemanticEffectSummary is computed from the effect matrix. ConservativePure
// becomes true only when every observed effect is proven non-observable.
type SemanticEffectSummary struct {
	Counts           map[string]int `json:"counts"`
	ConservativePure bool           `json:"conservative_pure"`
	Unknown          bool           `json:"unknown"`
}

func SummarizeEffects(program *SemanticProgram) SemanticEffectSummary {
	out := SemanticEffectSummary{Counts: map[string]int{}, ConservativePure: true}
	if program == nil {
		out.ConservativePure, out.Unknown = false, true
		return out
	}
	evidence := program.Evidence
	if program.UniversalAST != nil {
		// This analysis reads the canonical matrix directly and therefore does
		// not require the temporary statement/expression compatibility view.
		evidence = program.UniversalAST.Evidence
	}
	for column, axis := range evidence.EffectAxes {
		count := 0
		for row := 0; row < evidence.Effects.Rows; row++ {
			if evidence.Effects.At(row, column) != 0 {
				count++
			}
		}
		if count == 0 {
			continue
		}
		out.Counts[axis] = count
		switch axis {
		case "local.read", "control":
		case "call.unknown":
			out.Unknown, out.ConservativePure = true, false
		default:
			out.ConservativePure = false
		}
	}
	return out
}
