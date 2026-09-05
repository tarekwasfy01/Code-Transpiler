package backend

import (
	"strings"
	"testing"
)

func TestSourceParserInventoryIsUpstreamAndLanguageSpecific(t *testing.T) {
	i := ActualFrontendParserInventory()
	if len(i.Languages) != 13 || len(i.Forms) == 0 || i.Accept.NonZeros() == 0 {
		t.Fatalf("empty source parser inventory: languages=%d forms=%d cells=%d", len(i.Languages), len(i.Forms), i.Accept.NonZeros())
	}
	sets := make(map[string]string, len(i.Languages))
	for _, language := range i.Languages {
		if len(i.PerLanguage[language]) == 0 {
			t.Fatalf("no parser forms for %s", language)
		}
		sets[language] = strings.Join(i.PerLanguage[language], "\x00")
		for _, form := range i.PerLanguage[language] {
			e, ok := i.ByLanguage[language][form]
			if !ok || e.ParserSource == "" || strings.Contains(e.ParserSource, "internal\\matrixir") || strings.Contains(e.ParserSource, "internal/matrixir") {
				t.Fatalf("invalid upstream evidence for %s/%s: %+v", language, form, e)
			}
			if e.Coverage == "" || e.Coverage == "UNCLASSIFIED" || e.Coverage == "SILENT_DROP" {
				t.Fatalf("invalid coverage for %s/%s: %q", language, form, e.Coverage)
			}
		}
	}
	first := ""
	identical := true
	for _, s := range sets {
		if first == "" {
			first = s
		} else if s != first {
			identical = false
			break
		}
	}
	if identical {
		t.Fatal("all language source-parser axes are identical without parser evidence")
	}
}
