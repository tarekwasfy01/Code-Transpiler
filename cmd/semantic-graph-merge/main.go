// Copyright (c) 2026 Tarek Wasfy
package main

import (
	"bufio"
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

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
)

// This command performs the graph operation that a file concatenation cannot
// provide: it imports complete native-SE UAST documents, gives every source
// graph a disjoint node namespace, rewrites typed node references, and joins
// their syntax roots under one deterministic program root.
func main() {
	out := flag.String("o", "", "output .json, .se, .sp, or .spz file")
	list := flag.String("inputs-from", "", "UTF-8 text file containing one input .se path per line")
	flag.Parse()
	inputs := append([]string(nil), flag.Args()...)
	if *list != "" {
		f, err := os.Open(*list)
		if err != nil {
			fatalf("open input list: %v", err)
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			if input := strings.TrimSpace(scanner.Text()); input != "" && !strings.HasPrefix(input, "#") {
				inputs = append(inputs, input)
			}
		}
		readErr := scanner.Err()
		closeErr := f.Close()
		if readErr != nil {
			fatalf("read input list: %v", readErr)
		}
		if closeErr != nil {
			fatalf("close input list: %v", closeErr)
		}
	}
	if *out == "" || len(inputs) == 0 {
		fmt.Fprintln(os.Stderr, "usage: semantic-graph-merge -o merged.json|merged.se|merged.sp|merged.spz [-inputs-from paths.txt] [input1.se input2.se ...]")
		os.Exit(2)
	}

	merger := &graphMerger{}
	seenMembers := map[string]string{}
	bundleCount := 0
	for _, name := range inputs {
		isBundle, err := consumeInput(name, func(label string, data []byte) error {
			program, parseErr := backend.ParseSemanticSE(data)
			if parseErr != nil {
				return fmt.Errorf("parse %s: %w", label, parseErr)
			}
			if program.UniversalAST == nil || len(program.UniversalAST.Nodes) == 0 {
				return fmt.Errorf("%s has no canonical UAST graph", label)
			}
			return merger.add(program)
		}, func(member bundleMember) error {
			if old, exists := seenMembers[member.Path]; exists {
				if old != member.SHA256 {
					return fmt.Errorf("bundle member path %q has conflicting hashes", member.Path)
				}
				return nil
			}
			seenMembers[member.Path] = member.SHA256
			graph, parseErr := backend.ParseSemanticSEGraph(member.Data)
			if parseErr != nil {
				return fmt.Errorf("parse bundle member %s: %w", member.Path, parseErr)
			}
			program := &backend.SemanticProgram{UniversalAST: graph}
			if graph == nil || len(graph.Nodes) == 0 {
				return fmt.Errorf("bundle member %s has no canonical UAST graph", member.Path)
			}
			return merger.add(program)
		})
		if err != nil {
			fatalf("read/parse %s: %v", name, err)
		}
		if isBundle {
			bundleCount++
			continue
		}
	}

	merged, err := merger.finish()
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
	var data []byte
	switch strings.ToLower(filepath.Ext(*out)) {
	case ".json":
		// finish() and ApplySemanticClosure have already validated the canonical
		// UAST. Serialize that graph once; SP intentionally duplicates its full
		// canonical JSON payload and SE reparses it for readable rendering.
		data, err = json.Marshal(merged)
	case ".spz":
		// A project graph is machine-transport data, not a hand-authored source
		// file. Avoid expanding every UAST value through the human-readable SE
		// formatter; compile-csc accepts SPZ directly.
		data, err = program.MarshalSemanticSPZ()
	case ".sp":
		data, err = program.MarshalSemanticSP()
	default:
		data, err = program.MarshalSemanticSESemanticOnly()
	}
	if err != nil {
		fatalf("serialize merged graph: %v", err)
	}
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		fatalf("write %s: %v", *out, err)
	}
	fmt.Printf("GRAPH_MERGE=PASS\nBUNDLES=%d\nINPUTS=%d\nNODES=%d\nRELATIONS=%d\nBYTES=%d\nOUTPUT=%s\n", bundleCount, merger.inputs, len(merged.Nodes), len(merged.Relations), len(data), *out)
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
func consumeInput(name string, consumePlain func(string, []byte) error, consumeMember func(bundleMember) error) (bool, error) {
	f, err := os.Open(name)
	if err != nil {
		return false, err
	}
	defer f.Close()
	reader := bufio.NewReaderSize(f, 256*1024)
	marker := []byte("# semantic_bundle_members_v1 ")
	for {
		line, readErr := reader.ReadBytes('\n')
		if bytes.HasPrefix(line, marker) {
			return true, consumeBundlePayload(reader, bytes.TrimSpace(bytes.TrimPrefix(bytes.TrimSuffix(line, []byte("\n")), marker)), consumeMember)
		}
		if readErr != nil {
			if readErr != io.EOF {
				return false, readErr
			}
			break
		}
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return false, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return false, err
	}
	return false, consumePlain(name, data)
}

func consumeBundlePayload(reader *bufio.Reader, first []byte, consumeMember func(bundleMember) error) error {
	var encoded bytes.Buffer
	encoded.Write(first)
	for {
		line, err := reader.ReadBytes('\n')
		line = bytes.TrimSpace(line)
		if len(line) > 0 {
			if !bytes.HasPrefix(line, []byte("# ")) {
				break
			}
			encoded.Write(bytes.TrimPrefix(line, []byte("# ")))
		}
		if err != nil {
			if err != io.EOF {
				return err
			}
			break
		}
	}
	packed, err := base64.StdEncoding.DecodeString(encoded.String())
	if err != nil {
		return fmt.Errorf("membership base64: %w", err)
	}
	index, err := gunzip(packed)
	if err != nil {
		return fmt.Errorf("membership index gzip: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(index))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('[') {
		return fmt.Errorf("membership index is not an array")
	}
	for decoder.More() {
		var member bundleMember
		if err := decoder.Decode(&member); err != nil {
			return fmt.Errorf("membership entry: %w", err)
		}
		if member.Path == "" || member.SE == "" {
			return fmt.Errorf("membership entry is incomplete")
		}
		memberPacked, err := base64.StdEncoding.DecodeString(member.SE)
		if err != nil {
			return fmt.Errorf("member %s base64: %w", member.Path, err)
		}
		member.Data, err = gunzip(memberPacked)
		if err != nil {
			return fmt.Errorf("member %s gzip: %w", member.Path, err)
		}
		digest := sha256.Sum256(member.Data)
		if !strings.EqualFold(fmt.Sprintf("%x", digest[:]), member.SHA256) {
			return fmt.Errorf("member %s SHA-256 mismatch", member.Path)
		}
		if err := consumeMember(member); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}

func decodeBundle(data []byte) ([]bundleMember, bool, error) {
	marker := []byte("# semantic_bundle_members_v1 ")
	// A semantic source graph can legitimately contain the marker text inside
	// a string-valued source/evidence field. Only the standalone comment line
	// appended by semantic-bundle-merge is a bundle boundary.
	normalized := bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	start := -1
	for offset := 0; offset < len(normalized); {
		end := bytes.IndexByte(normalized[offset:], '\n')
		if end < 0 {
			end = len(normalized) - offset
		}
		lineEnd := offset + end
		if bytes.HasPrefix(normalized[offset:lineEnd], marker) {
			start = offset
			break
		}
		if lineEnd == len(normalized) {
			break
		}
		offset = lineEnd + 1
	}
	if start < 0 {
		return nil, false, nil
	}
	text := string(normalized[start:])
	lines := strings.Split(text, "\n")
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

type graphMerger struct {
	merged   *backend.UniversalASTDocument
	roots    []int
	modules  []string
	nodeBase int
	inputs   int
}

func (m *graphMerger) add(program *backend.SemanticProgram) error {
	if program == nil || program.UniversalAST == nil {
		return fmt.Errorf("input %d has no UAST", m.inputs)
	}
	u := program.UniversalAST
	if m.merged == nil {
		first := *u
		first.Nodes = nil
		first.Relations = nil
		first.Evidence = backend.SemanticEvidence{}
		first.Surface = nil
		first.Extensions = cloneMap(u.Extensions)
		first.Metadata = cloneStringMap(u.Metadata)
		if first.Metadata == nil {
			first.Metadata = map[string]string{}
		}
		// This is a disjoint graph, not a single native-Go source module.
		first.Metadata["frontend"] = "semantic-uast-graph-merge-v1"
		first.Metadata["source"] = "uast-graph"
		first.Metadata["frontend_route"] = "CANONICALIZE_ONLY"
		first.Metadata["graph_merge"] = "uast-disjoint-namespace-v1"
		m.merged = &first
	}
	index := m.inputs
	nodeMap := make(map[int]int, len(u.Nodes))
	for i, node := range u.Nodes {
		if _, exists := nodeMap[node.ID]; exists {
			return fmt.Errorf("input %d has duplicate node %d", index, node.ID)
		}
		nodeMap[node.ID] = m.nodeBase + i
	}
	root := syntaxRoot(u)
	if root < 0 {
		return fmt.Errorf("input %d has no syntax root", index)
	}
	m.roots = append(m.roots, nodeMap[root])
	for _, node := range u.Nodes {
		rewritten := node
		rewritten.ID = nodeMap[node.ID]
		rewritten.Fields = rewriteFields(node.Fields, nodeMap)
		m.merged.Nodes = append(m.merged.Nodes, rewritten)
	}
	for _, relation := range u.Relations {
		from, ok := nodeMap[relation.From]
		if !ok {
			return fmt.Errorf("input %d relation source %d missing", index, relation.From)
		}
		rewritten := relation
		rewritten.From = from
		if relation.To.Domain == "node" {
			old, err := strconv.Atoi(relation.To.ID)
			if err != nil {
				return fmt.Errorf("input %d relation target %q is not a node id", index, relation.To.ID)
			}
			to, ok := nodeMap[old]
			if !ok {
				return fmt.Errorf("input %d relation target %d missing", index, old)
			}
			rewritten.To.ID = strconv.Itoa(to)
		}
		m.merged.Relations = append(m.merged.Relations, rewritten)
	}
	m.modules = append(m.modules, u.Origin.Modules...)
	if u.Origin.SourceLanguage != "" {
		m.modules = append(m.modules, fmt.Sprintf("graph-%d:%s", index, u.Origin.SourceLanguage))
	}
	m.nodeBase += len(u.Nodes)
	m.inputs++
	if m.inputs%10 == 0 {
		fmt.Fprintf(os.Stderr, "MERGE_PROGRESS inputs=%d nodes=%d relations=%d\n", m.inputs, len(m.merged.Nodes), len(m.merged.Relations))
	}
	return nil
}

func (m *graphMerger) finish() (*backend.UniversalASTDocument, error) {
	if m.merged == nil || len(m.roots) == 0 {
		return nil, fmt.Errorf("no input graphs")
	}
	m.merged.Metadata["graph_merge_inputs"] = strconv.Itoa(m.inputs)
	primary, ordinal := m.roots[0], 0
	for _, relation := range m.merged.Relations {
		if relation.Kind == "syntax.child" && relation.From == primary {
			ordinal++
		}
	}
	for _, root := range m.roots[1:] {
		attrs := map[string]json.RawMessage{}
		role, _ := json.Marshal("statement")
		ord, _ := json.Marshal(ordinal)
		attrs["role"], attrs["ordinal"] = role, ord
		m.merged.Relations = append(m.merged.Relations, backend.UniversalASTRelation{
			Kind: "syntax.child", From: primary,
			To: backend.UniversalASTReference{Domain: "node", ID: strconv.Itoa(root)}, Attributes: attrs,
		})
		ordinal++
	}
	sort.Strings(m.modules)
	m.merged.Origin.Modules = unique(m.modules)
	evidence, err := backend.AnalyzeUniversalEvidence(m.merged)
	if err != nil {
		return nil, fmt.Errorf("derive merged evidence: %w", err)
	}
	m.merged.Evidence = evidence
	return m.merged, nil
}

func merge(programs []*backend.SemanticProgram) (*backend.UniversalASTDocument, error) {
	m := &graphMerger{}
	for _, program := range programs {
		if err := m.add(program); err != nil {
			return nil, err
		}
	}
	return m.finish()
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
	kinds := make(map[int]string, len(u.Nodes))
	for _, node := range u.Nodes {
		kinds[node.ID] = node.StructuralKind
	}
	for _, id := range roots {
		if strings.EqualFold(kinds[id], "Scope") {
			return id
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
