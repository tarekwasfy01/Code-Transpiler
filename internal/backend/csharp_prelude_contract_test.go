// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"strings"
	"testing"
)

func TestCSharpPreludeSupportsFrameworkCSharp5(t *testing.T) {
	for _, forbidden := range []string{"=>", "double.IsFinite", " is Value x", " is IConvertible c"} {
		if strings.Contains(csharpPrelude, forbidden) {
			t.Fatalf("C# prelude still requires a newer language/runtime feature: %q", forbidden)
		}
	}
}
