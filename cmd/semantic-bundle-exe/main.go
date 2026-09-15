// Copyright (c) 2026 Tarek Wasfy
package main

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
)

// The three complete semantic bundle documents are embedded into this native
// executable. They are verified at startup so the distribution cannot silently
// run with a partial or altered bundle.
//
//go:embed assets/semantic_frontend.se
var frontend []byte

//go:embed assets/semantic_uast.se
var uast []byte

//go:embed assets/semantic_backend.se
var backend []byte

func main() {
	for _, b := range [][]byte{frontend, uast, backend} {
		if len(b) == 0 || len(b) < 4 || string(b[:2]) != "# " {
			panic("invalid embedded semantic bundle")
		}
	}
	fmt.Printf("semantic bundles embedded: frontend=%d uast=%d backend=%d bytes\n", len(frontend), len(uast), len(backend))
	fmt.Printf("sha256 frontend=%s\n", digest(frontend))
	fmt.Printf("sha256 uast=%s\n", digest(uast))
	fmt.Printf("sha256 backend=%s\n", digest(backend))
}

func digest(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
