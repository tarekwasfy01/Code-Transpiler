// Copyright (c) 2026 Tarek Wasfy
package thirdpartylicenses

import (
	_ "embed"
	"fmt"
	"strings"
)

type Entry struct {
	Name     string
	License  string
	Source   string
	Text     string
	Embedded bool
}

// TreeSitter contains the MIT license text for the bundled Tree-sitter
// reference/runtime-derived assets. It is embedded so one-file builds carry
// the notice without relying on files next to the executable.
//
//go:embed TreeSitterLicense.txt
var TreeSitter string

//go:embed GioLicense.txt
var Gio string

//go:embed GVCodeLicense.txt
var GVCode string

//go:embed ProjectLicense.txt
var Project string

//go:embed Py2ManyLicense.txt
var Py2Many string

func All() []Entry {
	return []Entry{
		{Name: "Code Transpiler", License: "MIT", Source: "https://github.com/tarekwasfy01/Code-Transpiler/v2/v2", Text: Project, Embedded: true},
		{Name: "Tree-sitter", License: "MIT", Source: "https://github.com/tree-sitter/tree-sitter", Text: TreeSitter, Embedded: true},
		{Name: "Gio", License: "MIT", Source: "https://gioui.org", Text: Gio, Embedded: true},
		{Name: "gvcode", License: "MIT", Source: "https://github.com/oligo/gvcode", Text: GVCode, Embedded: true},
		{Name: "Py2Many", License: "MIT", Source: "https://github.com/py2many/py2many", Text: Py2Many, Embedded: true},
		{Name: "go2cs", License: "MIT", Source: "https://github.com/ritchiecarroll/go2cs"},
		{Name: "MrKWatkins.Ast", License: "MIT", Source: "https://github.com/MrKWatkins/Ast"},
		{Name: "Babylon", License: "MIT", Source: "https://github.com/babel/babylon"},
		{Name: "Amplication AST Types", License: "Apache-2.0", Source: "https://github.com/amplication/ast-types"},
		{Name: "Sharplasu", License: "Apache-2.0", Source: "https://github.com/Strumenta/sharplasu"},
		{Name: "Harmony", License: "MIT", Source: "https://harmony.pardeike.net"},
		{Name: "AssetRipper LLVM IR", License: "MIT", Source: "https://github.com/AssetRipper/AssetRipper.Translation.LlvmIR"},
		{Name: "Roslyn", License: "MIT", Source: "https://github.com/dotnet/roslyn"},
		{Name: "Mono mcs", License: "MIT", Source: "https://github.com/mono/mono"},
		{Name: "CIL Project", License: "see repository terms", Source: "https://github.com/cil-project/cil"},
		{Name: "CIL reference", License: "see source terms", Source: "https://people.eecs.berkeley.edu/~necula/cil/"},
		{Name: "gioui.org/shader", License: "see repository terms", Source: "https://github.com/gioui/gio"},
		{Name: "andybalholm/stroke", License: "see repository terms", Source: "https://github.com/andybalholm/stroke"},
		{Name: "go-text/typesetting", License: "see repository terms", Source: "https://github.com/go-text/typesetting"},
		{Name: "go-text/typesetting-utils", License: "see repository terms", Source: "https://github.com/go-text/typesetting-utils"},
		{Name: "rdleal/intervalst", License: "see repository terms", Source: "https://github.com/rdleal/intervalst"},
		{Name: "golang.org/x/exp", License: "BSD-3-Clause", Source: "https://cs.opensource.google/go/x/exp/"},
		{Name: "golang.org/x/image", License: "BSD-3-Clause", Source: "https://cs.opensource.google/go/x/image/"},
		{Name: "golang.org/x/net", License: "BSD-3-Clause", Source: "https://cs.opensource.google/go/x/net/"},
		{Name: "golang.org/x/sys", License: "BSD-3-Clause", Source: "https://cs.opensource.google/go/x/sys/"},
		{Name: "golang.org/x/text", License: "BSD-3-Clause", Source: "https://cs.opensource.google/go/x/text/"},
	}
}

func Summary() string {
	var b strings.Builder
	b.WriteString("LICENSES / NOTICES\n\n")
	for _, e := range All() {
		state := "metadata only"
		if e.Embedded {
			state = "embedded"
		}
		fmt.Fprintf(&b, "%s | %s | %s | %s\n", e.Name, e.License, state, e.Source)
	}
	return b.String()
}

func FullText() string {
	var b strings.Builder
	for _, e := range All() {
		fmt.Fprintf(&b, "===== %s (%s) =====\nSource: %s\n", e.Name, e.License, e.Source)
		if e.Embedded {
			b.WriteString(e.Text)
		} else {
			b.WriteString("License text is not embedded in this build; consult the authoritative source above.\n")
		}
		b.WriteString("\n\n")
	}
	return b.String()
}
