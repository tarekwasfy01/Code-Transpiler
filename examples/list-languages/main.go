// Copyright (c) 2026 Tarek Wasfy
package main

import (
	"fmt"

	codetranspiler "github.com/tarekwasfy01/Code-Transpiler"
)

func main() {
	for _, lang := range codetranspiler.Languages() {
		fmt.Println(lang.ID)
	}
}
