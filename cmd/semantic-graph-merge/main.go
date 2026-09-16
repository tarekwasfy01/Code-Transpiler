// Copyright (c) 2026 Tarek Wasfy
package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/tarekwasfy01/Code-Transpiler/v2/internal/backend"
)

// This command performs the graph operation that a file concatenation cannot
// provide: it imports complete native-SE UAST documents, gives every source
// graph a disjoint node namespace, rewrites typed node references, and joins
// their syntax roots under one deterministic program root.
func main() {
	out := flag.String("o", "", "output .se file")
	flag.Parse()
	if *out == "" || flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: semantic-graph-merge -o merged.se input1.se input2.se ...")
		os.Exit(2)
	}

	programs := make([]*backend.SemanticProgram, 0, flag.NArg())
	seenMembers := map[string]string{}
	bundleCount := 0
	for _, name := range flag.Args() {
		data, err := os.ReadFile(name)
		if err != nil {
			fatalf("read %s: %v", name, err)
		}
		members, isBundle, err := decodeBundle(data)
		if err != nil {
			fatalf("decode %s: %v", name, err)
		}
		if isBundle {
			bundleCount++
			for _, member := range members {
				if old, exists := seenMembers[member.Path]; exists {
					if old != member.SHA256 {
						fatalf("bundle member path %q has conflicting hashes", member.Path)
					}
					continue
				}
				seenMembers[member.Path] = member.SHA256
				programGraph, parseErr := backend.ParseSemanticSEGraph(member.Data)
				if parseErr != nil {
					fatalf("parse bundle member %s: %v", member.Path, parseErr)
				}
				program := &backend.SemanticProgram{UniversalAST: programGraph}
				if program.UniversalAST == nil || len(program.UniversalAST.Nodes) == 0 {
					fatalf("bundle member %s has no canonical UAST graph", member.Path)
				}
				programs = append(programs, program)
			}
			continue
		}
		program, err := backend.ParseSemanticSE(data)
		if err != nil {
			fatalf("parse %s: %v", name, err)
		}
		if program.UniversalAST == nil || len(program.UniversalAST.Nodes) == 0 {
			fatalf("%s has no canonical UAST graph", name)
		}
		programs = append(programs, program)
	}

	merged, err := merge(programs)
	if err != nil {
		fatalf("merge: %v", err)
	}
	if err := backend.ApplySemanticClosure(merged); err != nil {
		fatalf("merged closure: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fatalf("create output directory: %v", err)
	}
	program := &backend.SemanticProgram{UniversalAST: merged}
	data, err := program.MarshalSemanticSEWithSource()
	if err != nil {
		fatalf("serialize merged graph: %v", err)
	}
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		fatalf("write %s: %v", *out, err)
	}
	fmt.Printf("GRAPH_MERGE=PASS\nBUNDLES=%d\nINPUTS=%d\nNODES=%d\nRELATIONS=%d\nBYTES=%d\nOUTPUT=%s\n", bundleCount, len(programs), len(merged.Nodes), len(merged.Relations), len(data), *out)
}

type bundleMember struct {
	Path   string
	SHA256 string
	SE     string `json:"se_gzip_base64"`
	Data   []byte `json:"-"`
}

// decodeBundle recognizes the membership payload appended by the distribution
// builder. The carrier's visible UAST is intentionally not used as a
// substitute for its members: the members are the actual source graphs.
func decodeBundle(data []byte) ([]bundleMember, bool, error) {
	marker := []byte("# semantic_bundle_members_v1 ")
	start := bytes.Index(data, marker)
	if start < 0 {
		return nil, false, nil
	}
	text := string(data[start:])
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) == 0 {
		return nil, true, fmt.Errorf("empty membership marker")
	}
	var payload strings.Builder
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "# ") {
			break
		}
		payload.WriteString(strings.TrimPrefix(line, "# "))
	}
	packed, err := base64.StdEncoding.DecodeString(payload.String())
	if err != nil {
		return nil, true, fmt.Errorf("membership base64: %w", err)
	}
	indexBytes, err := gunzip(packed)
	if err != nil {
		return nil, true, fmt.Errorf("membership index gzip: %w", err)
	}
	var members []bundleMember
	if err := json.Unmarshal(indexBytes, &members); err != nil {
		return nil, true, fmt.Errorf("membership index JSON: %w", err)
	}
	sort.Slice(members, func(i, j int) bool { return members[i].Path < members[j].Path })
	for i := range members {
		if members[i].Path == "" || members[i].SE == "" {
			return nil, true, fmt.Errorf("membership entry %d is incomplete", i)
		}
		memberPacked, err := base64.StdEncoding.DecodeString(members[i].SE)
		if err != nil {
			return nil, true, fmt.Errorf("member %s base64: %w", members[i].Path, err)
		}
		members[i].Data, err = gunzip(memberPacked)
		if err != nil {
			return nil, true, fmt.Errorf("member %s gzip: %w", members[i].Path, err)
		}
		digest := sha256.Sum256(members[i].Data)
		if !strings.EqualFold(fmt.Sprintf("%x", digest[:]), members[i].SHA256) {
			return nil, true, fmt.Errorf("member %s SHA-256 mismatch", members[i].Path)
		}
	}
	return members, true, nil
}

func gunzip(data []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	out, readErr := io.ReadAll(zr)
	closeErr := zr.Close()
	if readErr != nil {
		return nil, readErr
	}
	return out, closeErr
}

func merge(programs []*backend.SemanticProgram) (*backend.UniversalASTDocument, error) {
	if len(programs) == 0 {
		return nil, fmt.Errorf("no input graphs")
	}
	first := programs[0].UniversalAST
	merged := *first
	merged.Nodes = nil
	merged.Relations = nil
	merged.Evidence = backend.SemanticEvidence{}
	merged.Surface = nil
	merged.Extensions = cloneMap(first.Extensions)
	merged.Metadata = cloneStringMap(first.Metadata)
	if merged.Metadata == nil {
		merged.Metadata = map[string]string{}
	}
	// The merged document is no longer one native-Go source module. Keeping
	// native-go-uast-v1 here would activate the single-module orphan-fragment
	// repair and delete every additional Scope root that this graph merge just
	// attached. Use an explicit neutral graph contract so all member roots stay
	// executable and the generic structural closure handles them uniformly.
	merged.Metadata["frontend"] = "semantic-uast-graph-merge-v1"
	merged.Metadata["source"] = "uast-graph"
	merged.Metadata["frontend_route"] = "CANONICALIZE_ONLY"
	merged.Metadata["graph_merge"] = "uast-disjoint-namespace-v1"
	merged.Metadata["graph_merge_inputs"] = strconv.Itoa(len(programs))

	var roots []int
	var modules []string
	nodeBase := 0
	for index, program := range programs {
		u := program.UniversalAST
		nodeMap := map[int]int{}
		for i, node := range u.Nodes {
			if _, exists := nodeMap[node.ID]; exists {
				return nil, fmt.Errorf("input %d has duplicate node %d", index, node.ID)
			}
			nodeMap[node.ID] = nodeBase + i
		}
		root := syntaxRoot(u)
		if root < 0 {
			return nil, fmt.Errorf("input %d has no syntax root", index)
		}
		roots = append(roots, nodeMap[root])
		for _, node := range u.Nodes {
			rewritten := node
			rewritten.ID = nodeMap[node.ID]
			rewritten.Fields = rewriteFields(node.Fields, nodeMap)
			// Field masks are matrix-derived and remain valid after an ID rewrite.
			merged.Nodes = append(merged.Nodes, rewritten)
		}
		for _, relation := range u.Relations {
			from, ok := nodeMap[relation.From]
			if !ok {
				return nil, fmt.Errorf("input %d relation source %d missing", index, relation.From)
			}
			rewritten := relation
			rewritten.From = from
			if relation.To.Domain == "node" {
				old, err := strconv.Atoi(relation.To.ID)
				if err != nil {
					return nil, fmt.Errorf("input %d relation target %q is not a node id", index, relation.To.ID)
				}
				to, ok := nodeMap[old]
				if !ok {
					return nil, fmt.Errorf("input %d relation target %d missing", index, old)
				}
				rewritten.To.ID = strconv.Itoa(to)
			}
			merged.Relations = append(merged.Relations, rewritten)
		}
		if len(u.Origin.Modules) > 0 {
			modules = append(modules, u.Origin.Modules...)
		}
		if u.Origin.SourceLanguage != "" {
			modules = append(modules, fmt.Sprintf("graph-%d:%s", index, u.Origin.SourceLanguage))
		}
		nodeBase += len(u.Nodes)
	}

	// Every imported document contributes exactly one root. The first root is
	// the common root; remaining roots become ordered statement children. This
	// is a structural graph rewrite, not a textual append.
	primary := roots[0]
	ordinal := 0
	for _, relation := range merged.Relations {
		if relation.Kind == "syntax.child" && relation.From == primary {
			ordinal++
		}
	}
	for _, root := range roots[1:] {
		attrs := map[string]json.RawMessage{}
		role, _ := json.Marshal("statement")
		ord, _ := json.Marshal(ordinal)
		attrs["role"], attrs["ordinal"] = role, ord
		merged.Relations = append(merged.Relations, backend.UniversalASTRelation{
			Kind: "syntax.child", From: primary,
			To: backend.UniversalASTReference{Domain: "node", ID: strconv.Itoa(root)}, Attributes: attrs,
		})
		ordinal++
	}
	sort.Strings(modules)
	merged.Origin.Modules = unique(modules)
	evidence, err := backend.AnalyzeUniversalEvidence(&merged)
	if err != nil {
		return nil, fmt.Errorf("derive merged evidence: %w", err)
	}
	merged.Evidence = evidence
	return &merged, nil
}

func syntaxRoot(u *backend.UniversalASTDocument) int {
	incoming := map[int]bool{}
	for _, relation := range u.Relations {
		if relation.Kind == "syntax.child" && relation.To.Domain == "node" {
			if id, err := strconv.Atoi(relation.To.ID); err == nil {
				incoming[id] = true
			}
		}
	}
	roots := make([]int, 0)
	for _, node := range u.Nodes {
		if !incoming[node.ID] {
			roots = append(roots, node.ID)
		}
	}
	if len(roots) == 0 {
		return -1
	}
	sort.Ints(roots)
	for _, id := range roots {
		for _, node := range u.Nodes {
			if node.ID == id && strings.EqualFold(node.StructuralKind, "Scope") {
				return id
			}
		}
	}
	return roots[0]
}

func rewriteFields(fields map[string]json.RawMessage, nodeMap map[int]int) map[string]json.RawMessage {
	if fields == nil {
		return nil
	}
	out := make(map[string]json.RawMessage, len(fields))
	for key, raw := range fields {
		var value any
		if json.Unmarshal(raw, &value) != nil {
			out[key] = raw
			continue
		}
		if key == "id" {
			if old, ok := asInt(value); ok {
				if next, exists := nodeMap[old]; exists {
					value = next
				}
			}
		}
		value = rewriteReferences(value, nodeMap)
		encoded, err := json.Marshal(value)
		if err == nil {
			out[key] = encoded
		} else {
			out[key] = raw
		}
	}
	return out
}

func rewriteReferences(value any, nodeMap map[int]int) any {
	switch x := value.(type) {
	case []any:
		for i := range x {
			x[i] = rewriteReferences(x[i], nodeMap)
		}
	case map[string]any:
		if domain, ok := x["domain"].(string); ok && domain == "node" {
			if old, ok := asInt(x["id"]); ok {
				if next, exists := nodeMap[old]; exists {
					x["id"] = strconv.Itoa(next)
				}
			}
		}
		for key, item := range x {
			x[key] = rewriteReferences(item, nodeMap)
		}
	}
	return value
}

func asInt(value any) (int, bool) {
	switch x := value.(type) {
	case float64:
		return int(x), x == float64(int(x))
	case json.Number:
		n, err := strconv.Atoi(string(x))
		return n, err == nil
	case string:
		n, err := strconv.Atoi(x)
		return n, err == nil
	default:
		return 0, false
	}
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func unique(values []string) []string {
	out := values[:0]
	seen := map[string]bool{}
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "semantic-graph-merge: "+format+"\n", args...)
	os.Exit(1)
}
