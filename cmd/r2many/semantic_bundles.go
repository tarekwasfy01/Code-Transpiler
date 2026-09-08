// Copyright (c) 2026 Tarek Wasfy
package main

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed semantic-bundles/semantic_frontend.se
var embeddedSemanticFrontend []byte

//go:embed semantic-bundles/semantic_uast.se
var embeddedSemanticUAST []byte

//go:embed semantic-bundles/semantic_backend.se
var embeddedSemanticBackend []byte

type semanticBundleInfo struct {
	Name    string `json:"name"`
	Bytes   int    `json:"bytes"`
	SHA256  string `json:"sha256"`
	Members int    `json:"members"`
}

var semanticBundleContract = []semanticBundleInfo{
	{Name: "semantic_frontend.se", Bytes: 17270998, SHA256: "8e84c9f867b4f63655d21d3164e1fda6658342091048fb1abb591b2709247bb3", Members: 23},
	{Name: "semantic_uast.se", Bytes: 23428874, SHA256: "d43ce090e5f1801df8759ac0cec92efb85ee449784fbb34cf23e1ab89c9a5c13", Members: 34},
	{Name: "semantic_backend.se", Bytes: 46096668, SHA256: "db6af41f22ccf06849be47d51f290aa1781e17a12c631b86e6c54e9310c357bf", Members: 119},
}

func embeddedSemanticBundles() [][]byte {
	return [][]byte{embeddedSemanticFrontend, embeddedSemanticUAST, embeddedSemanticBackend}
}

func verifyEmbeddedSemanticBundles() ([]semanticBundleInfo, error) {
	bundles := embeddedSemanticBundles()
	actual := make([]semanticBundleInfo, len(bundles))
	for i, data := range bundles {
		contract := semanticBundleContract[i]
		digest := sha256.Sum256(data)
		actual[i] = semanticBundleInfo{Name: contract.Name, Bytes: len(data), SHA256: hex.EncodeToString(digest[:]), Members: contract.Members}
		marker := []byte(fmt.Sprintf("count=%d ", contract.Members))
		if len(data) != contract.Bytes || actual[i].SHA256 != contract.SHA256 {
			return actual, fmt.Errorf("embedded bundle %s failed size/hash verification", contract.Name)
		}
		if !bytes.Contains(data, []byte("# semantic_bundle_members_v1 ")) || !bytes.Contains(data, marker) {
			return actual, fmt.Errorf("embedded bundle %s has no valid membership marker", contract.Name)
		}
	}
	return actual, nil
}

func semanticBundleInfoCommand() error {
	info, err := verifyEmbeddedSemanticBundles()
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"status":              "VALID",
		"member_entries":      176,
		"unique_source_files": 174,
		"bundles":             info,
	})
}

func semanticBundleExtractCommand(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: bundle-extract <directory>")
	}
	if _, err := verifyEmbeddedSemanticBundles(); err != nil {
		return err
	}
	if err := os.MkdirAll(args[0], 0o755); err != nil {
		return err
	}
	for i, data := range embeddedSemanticBundles() {
		path := filepath.Join(args[0], semanticBundleContract[i].Name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
