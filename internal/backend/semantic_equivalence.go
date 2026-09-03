package backend

// SemanticObservation is the deterministic observation available from the
// embedded semantic runtime. External native tests may extend this with files,
// state snapshots and process exit information.
type SemanticObservation struct {
	Stdout  string                `json:"stdout"`
	Error   string                `json:"error,omitempty"`
	Effects SemanticEffectSummary `json:"effects"`
}

func ObserveSemantic(program *SemanticProgram) SemanticObservation {
	out, err := RunSemantic(program)
	observation := SemanticObservation{Stdout: out, Effects: SummarizeEffects(program)}
	if err != nil {
		observation.Error = err.Error()
	}
	return observation
}

func EquivalentSemanticObservations(a, b SemanticObservation) bool {
	if a.Stdout != b.Stdout || a.Error != b.Error || a.Effects.ConservativePure != b.Effects.ConservativePure || a.Effects.Unknown != b.Effects.Unknown || len(a.Effects.Counts) != len(b.Effects.Counts) {
		return false
	}
	for effect, count := range a.Effects.Counts {
		if b.Effects.Counts[effect] != count {
			return false
		}
	}
	return true
}
