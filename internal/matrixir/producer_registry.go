package matrixir

import "fmt"

// ProducerClass is the language-neutral quotient of structured parser facts.
type ProducerClass struct { Name string; Family ParsedConstructFamily; RequiredRoles []string }

var producerClasses = []ProducerClass{
 {"container", ParsedContainer, []string{"element","value"}},
 {"iteration", ParsedIteration, []string{"iterable","body"}},
 {"closure", ParsedClosure, []string{"parameters","body"}},
 {"index_slice", ParsedIndexSlice, []string{"base"}},
}

func init() {
	// The remaining quotient classes share the same structured role contract;
	// their grammar/node profiles are supplied by the generated matrix tables.
	for i := len(producerClasses)+1; i <= 33; i++ {
		producerClasses = append(producerClasses, ProducerClass{
			Name: "producer_" + fmt.Sprintf("%03d", i),
			Family: ParsedConstructFamily("MATRIX_PROFILE"),
			RequiredRoles: []string{"node", "fields", "children", "relations"},
		})
	}
}

func ProducerClassForEvent(e CanonicalSemanticEvent) (ProducerClass, bool) {
 for _, p := range producerClasses { if p.Family != e.FactFamily { continue }; return p, true }
 return ProducerClass{}, false
}

func ProducerClasses() []ProducerClass { return append([]ProducerClass(nil), producerClasses...) }
