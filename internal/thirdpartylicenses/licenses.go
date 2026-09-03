package thirdpartylicenses

import _ "embed"

// TreeSitter contains the MIT license text for the bundled Tree-sitter
// reference/runtime-derived assets. It is embedded so one-file builds carry
// the notice without relying on files next to the executable.
//
//go:embed TreeSitterLicense.txt
var TreeSitter string
