// Copyright (c) 2026 Tarek Wasfy
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tarekwasfy01/Code-Transpiler/v2/internal/backend"
)

func semanticMergeCommand(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: semantic-merge <inputs...> -o output.se|output.sp|output.spz|output.json")
	}
	out := ""
	inputs := []string{}
	for i := 0; i < len(args); i++ {
		if args[i] == "-o" && i+1 < len(args) {
			out = args[i+1]
			i++
			continue
		}
		inputs = append(inputs, args[i])
	}
	if out == "" || len(inputs) == 0 {
		return fmt.Errorf("usage: semantic-merge <inputs...> -o output.se|output.sp|output.spz|output.json")
	}
	p, err := backend.MergeSemanticFiles(inputs)
	if err != nil {
		return err
	}
	ext := strings.ToLower(filepath.Ext(out))
	var data []byte
	switch ext {
	case ".se":
		data, err = p.MarshalSemanticSE()
	case ".sp":
		data, err = p.MarshalSemanticSP()
	case ".spz":
		data, err = p.MarshalSemanticSPZ()
	case ".json":
		data, err = p.MarshalSemanticJSON()
	default:
		return fmt.Errorf("unsupported merge output %q", out)
	}
	if err != nil {
		return err
	}
	return os.WriteFile(out, data, 0644)
}
