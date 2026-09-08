// Copyright (c) 2026 Tarek Wasfy
package backend

// Semantic Programming (SP) is a readable, versioned transport envelope for
// the canonical SemanticProgram/UAST JSON.  The JSON payload remains the
// lossless authority; the envelope deliberately does not introduce another IR.

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

const SemanticSPVersion = 1

// Semantic transport extensions.  .se is the current name for the readable
// SemanticProgram format; .sp and .spz remain wire-compatible legacy aliases.
const (
	SemanticSEVersion           = SemanticSPVersion
	SemanticReadableExtension   = ".se"
	SemanticCompressedExtension = ".spz"
	SemanticLegacyExtension     = ".sp"
	SemanticLegacyCompressedExt = ".spz"
)

// Stable SE schema keys are part of the format, so they do not need to be
// repeated in every document. Unknown extension fields remain lossless via
// their regular field.<name> spelling.
var semanticSEKnownKeys = []string{"basis_sha256", "contracts", "evaluation", "evidence", "extensions", "index_base", "language_facet", "language_profile", "metadata", "nodes", "origin", "projection", "relations", "schema_version", "surface", "type_contract", "type_graph", "type_relations", "type_table", "value_model"}

// SemanticSEFieldClass describes how a field is represented in the compact
// format. The classifier is deliberately conservative: only values that are
// provably reconstructible are omitted by the writer.
type SemanticSEFieldClass string

const (
	SemanticSEExplicit   SemanticSEFieldClass = "EXPLICIT"
	SemanticSEReferenced SemanticSEFieldClass = "REFERENCED"
	SemanticSEDerived    SemanticSEFieldClass = "DERIVED"
	SemanticSEDefault    SemanticSEFieldClass = "DEFAULT"
	SemanticSEDebugOnly  SemanticSEFieldClass = "DEBUG_ONLY"
)

func classifySemanticSEField(name string, raw json.RawMessage) SemanticSEFieldClass {
	switch name {
	case "schema_version":
		return SemanticSEDefault // represented by the envelope schema
	case "basis_sha256":
		return SemanticSEDerived
	case "surface":
		return SemanticSEDebugOnly
	case "language_profile":
		return SemanticSEReferenced
	default:
		return SemanticSEExplicit
	}
}

// MarshalSemanticSE emits the same lossless SemanticProgram envelope as SP,
// under the new .se format name.
func (p *SemanticProgram) MarshalSemanticSE() ([]byte, error) {
	return p.MarshalSemanticSEReadable()
}

// MarshalSemanticSESemanticOnly emits the canonical semantic core without the
// DEBUG_ONLY source surface. The surface hash/length can be retained by a
// caller separately when source preservation is required.
func (p *SemanticProgram) MarshalSemanticSESemanticOnly() ([]byte, error) {
	return p.marshalSemanticSE(false)
}

// MarshalSemanticSEWithSource is the explicit provenance-preserving form of
// .se.  The default command-line export intentionally uses the semantic-only
// form; callers that need byte-for-byte source provenance must opt in.
func (p *SemanticProgram) MarshalSemanticSEWithSource() ([]byte, error) {
	jsonData, err := p.MarshalSemanticJSON()
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(jsonData, &fields); err != nil {
		return nil, err
	}
	if raw, ok := fields["surface"]; ok {
		var surface map[string]any
		if json.Unmarshal(raw, &surface) == nil {
			if encoded, ok := surface["bytes_base64"].(string); ok && encoded != "" {
				if source, e := base64.StdEncoding.DecodeString(encoded); e == nil {
					var packed bytes.Buffer
					zw := gzip.NewWriter(&packed)
					if _, e = zw.Write(source); e == nil {
						e = zw.Close()
					}
					if e == nil {
						delete(surface, "bytes_base64")
						surface["bytes_gzip_base64"] = base64.StdEncoding.EncodeToString(packed.Bytes())
						if rawSurface, e2 := json.Marshal(surface); e2 == nil {
							fields["surface"] = rawSurface
						}
					}
				}
			}
		}
	}
	return marshalSemanticSEFields(fields, true)
}

func marshalSemanticSEFields(fields map[string]json.RawMessage, preserveSource bool) ([]byte, error) {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b bytes.Buffer
	b.WriteString("# Semantic Programming Language and Code Transpiler\n# Copyright (c) 2026 Tarek Wasfy\nse 1\nprogram {\nschema=1\n")
	known := make(map[string]int, len(semanticSEKnownKeys))
	for i, k := range semanticSEKnownKeys {
		known[k] = i
	}
	for _, k := range keys {
		if !preserveSource && k == "surface" {
			continue
		}
		if classifySemanticSEField(k, fields[k]) == SemanticSEDefault {
			continue
		}
		if _, ok := known[k]; ok {
			// Native SE uses the stable semantic keyword itself. Numeric fN slots
			// remain accepted by the parser for legacy documents, but are never
			// emitted by the canonical writer.
			value := renderNativeSEValue(fields[k])
			if k == "language_facet" {
				value = renderFacetRanges(fields[k])
			}
			fmt.Fprintf(&b, "%s=%s\n", k, value)
		} else {
			fmt.Fprintf(&b, "field.%s=%s\n", k, renderNativeSEValue(fields[k]))
		}
	}
	b.WriteString("}\n")
	return b.Bytes(), nil
}

func (p *SemanticProgram) marshalSemanticSE(preserveSource bool) ([]byte, error) {
	jsonData, err := p.MarshalSemanticJSON()
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(jsonData, &fields); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b bytes.Buffer
	b.WriteString("# Semantic Programming Language and Code Transpiler\n# Copyright (c) 2026 Tarek Wasfy\nse 1\nprogram {\nschema=1\n")
	known := make(map[string]int, len(semanticSEKnownKeys))
	for i, k := range semanticSEKnownKeys {
		known[k] = i
	}
	for _, k := range keys {
		if !preserveSource && k == "surface" {
			continue
		}
		if classifySemanticSEField(k, fields[k]) == SemanticSEDefault {
			continue
		}
		if _, ok := known[k]; ok {
			value := renderNativeSEValue(fields[k])
			if k == "language_facet" {
				value = renderFacetRanges(fields[k])
			}
			fmt.Fprintf(&b, "%s=%s\n", k, value)
		} else {
			fmt.Fprintf(&b, "field.%s=%s\n", k, renderNativeSEValue(fields[k]))
		}
	}
	b.WriteString("}\n")
	return b.Bytes(), nil
}

func nativeSETypeScopeSections(p *SemanticProgram) []byte {
	if p == nil || p.UniversalAST == nil {
		return nil
	}
	var b bytes.Buffer
	b.WriteString("    types {\n")
	for _, t := range p.UniversalAST.TypeTable {
		fmt.Fprintf(&b, "        type @%d {\n            kind = %s\n", t.ID, valueOrUnknown(t.Type.Kind))
		if t.Type.TypeOrigin != "" {
			fmt.Fprintf(&b, "            origin = %s\n", t.Type.TypeOrigin)
		}
		b.WriteString("        }\n")
	}
	b.WriteString("    }\n\n    scopes {\n")
	for _, s := range p.UniversalAST.Evidence.Scopes {
		fmt.Fprintf(&b, "        scope %%%d {\n            kind = %s\n            parent = %%%d\n        }\n", s.ID, valueOrUnknown(s.Kind), s.Parent)
	}
	b.WriteString("    }\n\n")
	return b.Bytes()
}

func valueOrUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

func nativeSERelationSection(p *SemanticProgram) []byte {
	var b bytes.Buffer
	b.WriteString("    relations {\n")
	if p != nil && p.UniversalAST != nil {
		for _, r := range p.UniversalAST.Relations {
			fmt.Fprintf(&b, "        %s %%%d -> %s:%s {\n", r.Kind, r.From, r.To.Domain, r.To.ID)
			if raw := r.Attributes["ordinal"]; len(raw) > 0 {
				var n int
				if json.Unmarshal(raw, &n) == nil {
					fmt.Fprintf(&b, "            ordinal = %d\n", n)
				}
			}
			if raw := r.Attributes["role"]; len(raw) > 0 {
				var s string
				if json.Unmarshal(raw, &s) == nil {
					fmt.Fprintf(&b, "            role = %s\n", s)
				}
			}
			b.WriteString("        }\n")
		}
	}
	b.WriteString("    }\n")
	return b.Bytes()
}

// nativeSENodeSections renders the stable, directly readable node projection
// while the field.* records continue to carry the complete lossless payload.
func nativeSENodeSections(p *SemanticProgram) []byte {
	if p == nil || p.UniversalAST == nil || len(p.UniversalAST.Nodes) == 0 {
		return nil
	}
	var b bytes.Buffer
	b.WriteString("    nodes {\n")
	for _, n := range p.UniversalAST.Nodes {
		fmt.Fprintf(&b, "        node %%%d {\n", n.ID)
		if n.StructuralKind != "" {
			fmt.Fprintf(&b, "            structural_kind = %s\n", n.StructuralKind)
		}
		if raw := n.Fields["kind"]; len(raw) > 0 {
			var kind string
			if json.Unmarshal(raw, &kind) == nil && kind != "" {
				fmt.Fprintf(&b, "            kind = %s\n", kind)
			}
		}
		if raw := n.Fields["scope_id"]; len(raw) > 0 {
			var scope int
			if json.Unmarshal(raw, &scope) == nil {
				fmt.Fprintf(&b, "            scope = %%%d\n", scope)
			}
		}
		if len(n.SemanticFacets) > 0 {
			b.WriteString("            facet = [")
			for i, f := range n.SemanticFacets {
				if i > 0 {
					b.WriteString(", ")
				}
				b.WriteString(f)
			}
			b.WriteString("]\n")
		}
		b.WriteString("        }\n\n")
	}
	b.WriteString("    }\n")
	return b.Bytes()
}

// renderNativeSEValue emits typed native SE values instead of JSON braces.
func renderNativeSEValue(raw json.RawMessage) string {
	var v any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if dec.Decode(&v) != nil {
		return strings.TrimSpace(string(raw))
	}
	return renderNativeSEAny(v)
}
func renderNativeSEAny(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		if x {
			return "true"
		}
		return "false"
	case string:
		return strconv.Quote(x)
	case json.Number:
		return string(x)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(x), 'g', -1, 32)
	case int:
		return strconv.Itoa(x)
	case []any:
		parts := make([]string, len(x))
		for i := range x {
			parts[i] = renderNativeSEAny(x[i])
		}
		return "list [ " + strings.Join(parts, " ") + " ]"
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			if k == "field_mask" {
				continue
			} // schema-derived from kind/facets

			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			key := k
			if !isSEIdentifier(key) {
				key = strconv.Quote(key)
			}
			parts = append(parts, key+" = "+renderNativeSEAny(x[k]))
		}
		return "object { " + strings.Join(parts, " ") + " }"
	default:
		return strconv.Quote(fmt.Sprint(v))
	}
}
func isSEIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if !(r == '_' || r == '-' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || (i > 0 && r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}
func renderFacetRanges(raw json.RawMessage) string {
	var bits []float64
	if json.Unmarshal(raw, &bits) != nil {
		return renderNativeSEValue(raw)
	}
	parts := []string{}
	for i := 0; i < len(bits); {
		if bits[i] == 0 {
			i++
			continue
		}
		start := i
		for i+1 < len(bits) && bits[i+1] != 0 {
			i++
		}
		if i == start {
			parts = append(parts, strconv.Itoa(start))
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", start, i))
		}
		i++
	}
	return fmt.Sprintf("ranges %d [ %s ]", len(bits), strings.Join(parts, " "))
}

// ParseSemanticSE imports a readable .se document.  The envelope is kept
// wire-compatible with .sp so existing files and tooling remain usable.
func ParseSemanticSE(data []byte) (*SemanticProgram, error) { return ParseSemanticSP(data) }

func FormatSemanticSE(data []byte) ([]byte, error) {
	p, err := ParseSemanticSE(data)
	if err != nil {
		return nil, err
	}
	return p.MarshalSemanticSEReadable()
}

// MarshalSemanticSECompact is the canonical minimal UTF-8 text form.
func (p *SemanticProgram) MarshalSemanticSECompact() ([]byte, error) {
	return p.MarshalSemanticSESemanticOnly()
}

// MarshalSemanticSEReadable keeps exactly the same grammar and values as the
// compact form, but formats top-level members with four-space indentation and
// blank lines between semantic sections. The parser ignores this whitespace,
// so both modes have identical SemanticProgram meaning.
func (p *SemanticProgram) MarshalSemanticSEReadable() ([]byte, error) {
	raw, err := p.MarshalSemanticSESemanticOnly()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	var b bytes.Buffer
	seenSection := false
	sectionKeys := map[string]bool{"contracts": true, "evidence": true, "extensions": true, "metadata": true, "nodes": true, "relations": true, "type_contract": true, "type_graph": true, "type_relations": true, "type_table": true}
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if t == "program {" || t == "}" || strings.HasPrefix(t, "#") || t == "se 1" {
			b.WriteString(t + "\n")
			continue
		}
		if t == "schema=1" {
			b.WriteString("    schema = 1\n")
			continue
		}
		if strings.Contains(t, "=") {
			parts := strings.SplitN(t, "=", 2)
			key, value := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
			if seenSection && sectionKeys[key] {
				b.WriteByte('\n')
			}
			if sectionKeys[key] {
				seenSection = true
			}
			if parsed, e := parseNativeSEValue(value); e == nil {
				b.WriteString("    " + key + " = " + prettySEValue(parsed, 4) + "\n")
			} else {
				b.WriteString("    " + key + " = " + value + "\n")
			}
		} else {
			b.WriteString("    " + t + "\n")
		}
	}
	return b.Bytes(), nil
}

func prettySEValue(v spValue, indent int) string {
	pad := strings.Repeat(" ", indent)
	switch x := v.(type) {
	case map[string]spValue:
		if len(x) == 0 {
			return "object { }"
		}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		b.WriteString("object {\n")
		for _, k := range keys {
			b.WriteString(pad + "    " + k + " = " + prettySEValue(x[k], indent+4) + "\n")
		}
		b.WriteString(pad + "}")
		return b.String()
	case []spValue:
		if len(x) == 0 {
			return "list [ ]"
		}
		inline := len(x) <= 8
		for _, e := range x {
			switch e.(type) {
			case map[string]spValue, []spValue:
				inline = false
			}
		}
		if inline {
			parts := make([]string, len(x))
			for i, e := range x {
				parts[i] = prettySEValue(e, indent)
			}
			return "list [ " + strings.Join(parts, " ") + " ]"
		}
		var b strings.Builder
		b.WriteString("list [\n")
		for _, e := range x {
			b.WriteString(pad + "    " + prettySEValue(e, indent+4) + "\n")
		}
		b.WriteString(pad + "]")
		return b.String()
	default:
		return renderNativeSEAny(x)
	}
}

// MarshalSemanticSP emits deterministic, line-oriented SP.  Metadata fields
// are intentionally human readable while semantic data is carried losslessly
// in the canonical JSON payload.
func (p *SemanticProgram) MarshalSemanticSP() ([]byte, error) {
	jsonData, err := p.MarshalSemanticJSON()
	if err != nil {
		return nil, err
	}
	var meta struct {
		Language string `json:"language"`
		Profile  string `json:"language_profile"`
	}
	_ = json.Unmarshal(jsonData, &meta)
	language := meta.Language
	if language == "" {
		language = meta.Profile
	}
	var b bytes.Buffer
	b.WriteString("# Copyright (c) 2026 Tarek Wasfy\n")
	fmt.Fprintf(&b, "sp %d\nprogram {\n    schema = %d\n", SemanticSPVersion, SemanticSPVersion)
	fmt.Fprintf(&b, "    source_language = %s\n", strconv.Quote(language))
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(jsonData, &fields); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "    field.%s = %s\n", k, strings.TrimSpace(string(fields[k])))
	}
	// Keep the canonical byte representation alongside the structured fields.
	// jsontext.Value fields intentionally preserve their object-key order; a
	// generic SP value tree cannot recover that order after parsing an object.
	// The canonical payload therefore remains the authoritative lossless path,
	// while field.* stays available for inspection and legacy structured input.
	fmt.Fprintf(&b, "    canonical_json = %s\n", strconv.Quote(string(jsonData)))
	b.WriteString("}\n")
	return b.Bytes(), nil
}

// ParseSemanticSP validates and imports the versioned SP envelope.
func ParseSemanticSP(data []byte) (*SemanticProgram, error) {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	lineNo := 0
	next := func() string {
		for lineNo < len(lines) {
			lineNo++
			s := strings.TrimSpace(lines[lineNo-1])
			if s == "" || strings.HasPrefix(s, "#") {
				continue
			}
			return s
		}
		return ""
	}
	header := next()
	parts := strings.Fields(header)
	if len(parts) != 2 || (parts[0] != "sp" && parts[0] != "se") {
		return nil, fmt.Errorf("semantic:%d: expected 'sp <version>' or 'se <version>'", lineNo)
	}
	isNativeSE := parts[0] == "se"
	version, err := strconv.Atoi(parts[1])
	if err != nil || version != SemanticSPVersion {
		return nil, fmt.Errorf("sp:%d: unsupported SP version %q", lineNo, parts[1])
	}
	if next() != "program {" {
		return nil, fmt.Errorf("sp:%d: expected program block", lineNo)
	}
	var payload string
	legacyBase64 := false
	structuredFields := map[string]spValue{}
	compactKeys := []string(nil)
	if isNativeSE {
		compactKeys = append([]string(nil), semanticSEKnownKeys...)
	}
	compactFields := map[string]spValue{}
	schema := 0
	seen := map[string]bool{}
	closed := false
	for {
		s := next()
		if s == "" {
			break
		}
		if s == "}" {
			closed = true
			break
		}
		// Native SE sections are currently mirrored by the lossless field
		// records. Accept and skip them while the section-specific readers are
		// added incrementally; SP remains strict for backward compatibility.
		if isNativeSE && !strings.Contains(s, "=") && strings.HasSuffix(s, "{") {
			depth := 1
			for depth > 0 {
				x := next()
				if x == "" {
					return nil, fmt.Errorf("se:%d: unterminated native section", lineNo)
				}
				depth += strings.Count(x, "{") - strings.Count(x, "}")
			}
			continue
		}
		key, value, ok := strings.Cut(s, "=")
		if !ok {
			return nil, fmt.Errorf("sp:%d: expected key = value", lineNo)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if isNativeSE && nativeSEBraceBalance(value) > 0 {
			for nativeSEBraceBalance(value) > 0 {
				part := next()
				if part == "" {
					return nil, fmt.Errorf("se:%d: unterminated structured value", lineNo)
				}
				value += " " + strings.TrimSpace(part)
			}
		}
		if seen[key] {
			return nil, fmt.Errorf("sp:%d: duplicate program field %q", lineNo, key)
		}
		seen[key] = true
		switch key {
		case "field_keys":
			v, e := parseSPValue(value)
			if e != nil {
				return nil, fmt.Errorf("se:%d: invalid field_keys", lineNo)
			}
			arr, ok := v.([]spValue)
			if !ok {
				return nil, fmt.Errorf("se:%d: field_keys must be an array", lineNo)
			}
			for _, x := range arr {
				s, ok := x.(string)
				if !ok {
					return nil, fmt.Errorf("se:%d: field_keys entries must be strings", lineNo)
				}
				compactKeys = append(compactKeys, s)
			}
		case "schema":
			schema, err = strconv.Atoi(value)
			if err != nil {
				return nil, fmt.Errorf("sp:%d: invalid schema", lineNo)
			}
		case "source_language":
			if _, err := strconv.Unquote(value); err != nil {
				return nil, fmt.Errorf("sp:%d: invalid source_language", lineNo)
			}
		case "canonical_json":
			payload, err = strconv.Unquote(value)
			if err != nil {
				return nil, fmt.Errorf("sp:%d: invalid canonical_json", lineNo)
			}
		case "semantic_json_base64":
			// Legacy compatibility import. New writers never emit this field.
			legacyBase64 = true
			payload, err = strconv.Unquote(value)
			if err != nil {
				return nil, fmt.Errorf("sp:%d: invalid semantic_json_base64", lineNo)
			}
		default:
			if strings.HasPrefix(key, "f") {
				if _, e := strconv.Atoi(strings.TrimPrefix(key, "f")); e == nil {
					raw, e := parseSPValue(value)
					if e != nil {
						return nil, fmt.Errorf("se:%d: invalid compact field", lineNo)
					}
					compactFields[key] = raw
					continue
				}
			}
			name := strings.TrimPrefix(key, "field.")
			if name == "" {
				return nil, fmt.Errorf("semantic:%d: empty program field", lineNo)
			}
			var raw spValue
			if isNativeSE {
				raw, err = parseNativeSEValue(value)
			} else {
				raw, err = parseSPValue(value)
			}
			if err != nil {
				return nil, fmt.Errorf("sp:%d: invalid semantic field %q: %w", lineNo, name, err)
			}
			structuredFields[name] = raw
		}
	}
	if len(compactFields) > 0 {
		for alias, raw := range compactFields {
			n, _ := strconv.Atoi(strings.TrimPrefix(alias, "f"))
			if n < 0 || n >= len(compactKeys) {
				return nil, fmt.Errorf("se:%d: compact field index out of range", lineNo)
			}
			structuredFields[compactKeys[n]] = raw
		}
	}
	if isNativeSE {
		if _, ok := structuredFields["schema_version"]; !ok {
			structuredFields["schema_version"] = float64(SemanticSPVersion)
		}
	}
	if !closed || schema != SemanticSPVersion || (!legacyBase64 && payload == "" && len(structuredFields) == 0) {
		return nil, fmt.Errorf("sp:%d: incomplete program block", lineNo)
	}
	for lineNo < len(lines) {
		if s := strings.TrimSpace(lines[lineNo]); s != "" && !strings.HasPrefix(s, "#") {
			return nil, fmt.Errorf("sp:%d: trailing content after program block", lineNo+1)
		}
		lineNo++
	}
	if legacyBase64 {
		// Decode without making the encoder part of the structured format.
		const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
		_ = alphabet
		decoded, e := decodeLegacyBase64(payload)
		if e != nil {
			return nil, fmt.Errorf("sp:%d: invalid semantic payload: %w", lineNo, e)
		}
		return ParseSemanticJSON(decoded)
	}
	if payload != "" {
		return ParseSemanticJSON([]byte(payload))
	}
	if len(structuredFields) > 0 {
		return parseStructuredSemanticDocument(structuredFields)
	}
	return ParseSemanticJSON([]byte(payload))
}

func nativeSEBraceBalance(s string) int {
	depth := 0
	quote := false
	escaped := false
	for _, r := range s {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && quote {
			escaped = true
			continue
		}
		if r == '"' {
			quote = !quote
			continue
		}
		if quote {
			continue
		}
		if r == '{' || r == '[' {
			depth++
		}
		if r == '}' || r == ']' {
			depth--
		}
	}
	return depth
}

func isNativeSEValue(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "object {") || strings.HasPrefix(s, "list [") || strings.HasPrefix(s, "ranges ")
}

type nativeSEParser struct {
	s string
	i int
}

func parseNativeSEValue(s string) (spValue, error) {
	p := &nativeSEParser{s: strings.TrimSpace(s)}
	v, e := p.value()
	if e == nil {
		p.ws()
		if p.i != len(p.s) {
			e = fmt.Errorf("trailing value")
		}
	}
	return v, e
}
func (p *nativeSEParser) ws() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\t' || p.s[p.i] == '\n' || p.s[p.i] == '\r') {
		p.i++
	}
}
func (p *nativeSEParser) word() string {
	p.ws()
	st := p.i
	for p.i < len(p.s) && p.s[p.i] != ' ' && p.s[p.i] != '\t' && p.s[p.i] != '\n' && p.s[p.i] != '\r' && p.s[p.i] != ']' && p.s[p.i] != '}' && p.s[p.i] != '=' {
		p.i++
	}
	return p.s[st:p.i]
}
func (p *nativeSEParser) value() (spValue, error) {
	p.ws()
	if p.i >= len(p.s) {
		return nil, fmt.Errorf("missing value")
	}
	if strings.HasPrefix(p.s[p.i:], "object") {
		p.i += 6
		p.ws()
		if p.i >= len(p.s) || p.s[p.i] != '{' {
			return nil, fmt.Errorf("object missing {")
		}
		p.i++
		out := map[string]spValue{}
		for {
			p.ws()
			if p.i < len(p.s) && p.s[p.i] == '}' {
				p.i++
				return out, nil
			}
			k := p.word()
			if k == "" {
				return nil, fmt.Errorf("object key missing")
			}
			p.ws()
			if p.i >= len(p.s) || p.s[p.i] != '=' {
				return nil, fmt.Errorf("object key missing =")
			}
			p.i++
			v, e := p.value()
			if e != nil {
				return nil, e
			}
			out[k] = v
		}
	}
	if strings.HasPrefix(p.s[p.i:], "list") {
		p.i += 4
		p.ws()
		if p.i >= len(p.s) || p.s[p.i] != '[' {
			return nil, fmt.Errorf("list missing [")
		}
		p.i++
		out := []spValue{}
		for {
			p.ws()
			if p.i < len(p.s) && p.s[p.i] == ']' {
				p.i++
				return out, nil
			}
			v, e := p.value()
			if e != nil {
				return nil, e
			}
			out = append(out, v)
		}
	}
	if strings.HasPrefix(p.s[p.i:], "ranges") {
		p.i += 6
		p.ws()
		dimWord := p.word()
		dim, eDim := strconv.Atoi(dimWord)
		if eDim != nil || dim < 0 {
			return nil, fmt.Errorf("ranges missing dimension")
		}
		p.ws()
		if p.i >= len(p.s) || p.s[p.i] != '[' {
			return nil, fmt.Errorf("ranges missing [")
		}
		p.i++
		out := make([]spValue, dim)
		for i := range out {
			out[i] = float64(0)
		}
		for {
			p.ws()
			if p.i < len(p.s) && p.s[p.i] == ']' {
				p.i++
				return out, nil
			}
			w := p.word()
			if w == "" {
				return nil, fmt.Errorf("empty range")
			}
			parts := strings.SplitN(w, "-", 2)
			a, e := strconv.Atoi(parts[0])
			if e != nil || a < 0 {
				return nil, fmt.Errorf("invalid range %q", w)
			}
			z := a
			if len(parts) == 2 {
				z, e = strconv.Atoi(parts[1])
				if e != nil || z < a {
					return nil, fmt.Errorf("invalid range %q", w)
				}
			}
			for i := a; i <= z; i++ {
				if i >= dim {
					return nil, fmt.Errorf("range exceeds dimension")
				}
				out[i] = float64(1)
			}
		}
	}
	if p.s[p.i] == '"' {
		st := p.i
		p.i++
		for p.i < len(p.s) {
			if p.s[p.i] == '\\' {
				p.i += 2
				continue
			}
			if p.s[p.i] == '"' {
				p.i++
				break
			}
			p.i++
		}
		var x string
		e := json.Unmarshal([]byte(p.s[st:p.i]), &x)
		return x, e
	}
	w := p.word()
	switch w {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null":
		return nil, nil
	}
	if f, e := strconv.ParseFloat(w, 64); e == nil {
		return f, nil
	}
	return nil, fmt.Errorf("invalid token %q", w)
}

// spValue is the small value tree produced by the SP semantic lexer. It is
// deliberately independent of JSON bytes: SP is parsed directly into the
// typed SemanticDocument below. JSON remains only a legacy compatibility
// import format and an output format.
type spValue any

type spValueParser struct {
	s string
	i int
}

func parseSPValue(s string) (spValue, error) {
	p := &spValueParser{s: strings.TrimSpace(s)}
	v, err := p.value()
	if err != nil {
		return nil, err
	}
	p.ws()
	if p.i != len(p.s) {
		return nil, fmt.Errorf("trailing value data")
	}
	return v, nil
}
func (p *spValueParser) ws() {
	for p.i < len(p.s) {
		switch p.s[p.i] {
		case ' ', '\t', '\r', '\n':
			p.i++
		default:
			return
		}
	}
}
func (p *spValueParser) value() (spValue, error) {
	p.ws()
	if p.i >= len(p.s) {
		return nil, fmt.Errorf("empty value")
	}
	switch p.s[p.i] {
	case '"':
		return p.string()
	case '{':
		return p.object()
	case '[':
		return p.array()
	case 't':
		if strings.HasPrefix(p.s[p.i:], "true") {
			p.i += 4
			return true, nil
		}
	case 'f':
		if strings.HasPrefix(p.s[p.i:], "false") {
			p.i += 5
			return false, nil
		}
	case 'n':
		if strings.HasPrefix(p.s[p.i:], "null") {
			p.i += 4
			return nil, nil
		}
	default:
		return p.number()
	}
	return nil, fmt.Errorf("invalid value")
}
func (p *spValueParser) string() (spValue, error) {
	start := p.i
	p.i++
	esc := false
	for p.i < len(p.s) {
		c := p.s[p.i]
		p.i++
		if c == '"' && !esc {
			q := p.s[start:p.i]
			v, err := strconv.Unquote(q)
			return v, err
		}
		if c == '\\' && !esc {
			esc = true
		} else {
			esc = false
		}
	}
	return nil, fmt.Errorf("unterminated string")
}
func (p *spValueParser) number() (spValue, error) {
	start := p.i
	for p.i < len(p.s) && strings.ContainsRune("+-0123456789.eE", rune(p.s[p.i])) {
		p.i++
	}
	if start == p.i {
		return nil, fmt.Errorf("invalid number")
	}
	n, err := strconv.ParseFloat(p.s[start:p.i], 64)
	if err != nil {
		return nil, err
	}
	return n, nil
}
func (p *spValueParser) array() (spValue, error) {
	p.i++
	out := []spValue{}
	p.ws()
	if p.i < len(p.s) && p.s[p.i] == ']' {
		p.i++
		return out, nil
	}
	for {
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		p.ws()
		if p.i >= len(p.s) {
			return nil, fmt.Errorf("unterminated array")
		}
		if p.s[p.i] == ']' {
			p.i++
			return out, nil
		}
		if p.s[p.i] != ',' {
			return nil, fmt.Errorf("expected comma")
		}
		p.i++
	}
}
func (p *spValueParser) object() (spValue, error) {
	p.i++
	out := map[string]spValue{}
	p.ws()
	if p.i < len(p.s) && p.s[p.i] == '}' {
		p.i++
		return out, nil
	}
	for {
		k, err := p.string()
		if err != nil {
			return nil, err
		}
		p.ws()
		if p.i >= len(p.s) || p.s[p.i] != ':' {
			return nil, fmt.Errorf("expected colon")
		}
		p.i++
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		out[k.(string)] = v
		p.ws()
		if p.i >= len(p.s) {
			return nil, fmt.Errorf("unterminated object")
		}
		if p.s[p.i] == '}' {
			p.i++
			return out, nil
		}
		if p.s[p.i] != ',' {
			return nil, fmt.Errorf("expected comma")
		}
		p.i++
	}
}

func parseStructuredSemanticDocument(fields map[string]spValue) (*SemanticProgram, error) {
	// Programs that already own a canonical UAST are serialized directly by
	// MarshalSemanticJSON. Parse that shape into the UAST without routing
	// through JSON or fabricating a compatibility document.
	if _, ok := fields["basis_sha256"]; ok {
		var u UniversalASTDocument
		if err := assignSPFields(reflect.ValueOf(&u).Elem(), fields); err != nil {
			return nil, err
		}
		if _, err := NormalizeUniversalAST(&u); err != nil {
			return nil, err
		}
		return &SemanticProgram{UniversalAST: &u}, nil
	}
	var doc SemanticDocument
	if err := assignSPFields(reflect.ValueOf(&doc).Elem(), fields); err != nil {
		return nil, err
	}
	return ParseSemanticDocument(doc)
}

func assignSPFields(dst reflect.Value, fields map[string]spValue) error {
	for key, value := range fields {
		if key == "source_language" {
			continue
		}
		field := fieldByJSONName(dst, key)
		if !field.IsValid() || !field.CanSet() {
			return fmt.Errorf("unknown semantic field %q", key)
		}
		if err := assignSPValue(field, value); err != nil {
			return fmt.Errorf("field %s: %w", key, err)
		}
	}
	return nil
}

func fieldByJSONName(v reflect.Value, name string) reflect.Value {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := strings.Split(f.Tag.Get("json"), ",")[0]
		if tag == name {
			return v.Field(i)
		}
	}
	return reflect.Value{}
}
func assignSPValue(dst reflect.Value, src spValue) error {
	// Value types with an explicit JSON unmarshaller own their structural
	// representation. This covers sparse COO matrices and future contract
	// values with private storage without adding package/type special cases.
	if dst.CanAddr() {
		if unmarshaler, ok := dst.Addr().Interface().(json.Unmarshaler); ok {
			raw, err := json.Marshal(src)
			if err != nil {
				return err
			}
			return unmarshaler.UnmarshalJSON(raw)
		}
	}
	// jsontext.Value is a named []byte carrying one raw JSON value.  Preserve
	// that value when importing legacy structured fields instead of treating it
	// as an ordinary array of bytes.
	if dst.Type().Name() == "Value" && strings.HasSuffix(dst.Type().PkgPath(), "/jsontext") {
		raw, err := json.Marshal(src)
		if err != nil {
			return err
		}
		v := reflect.ValueOf(raw)
		if v.Type().AssignableTo(dst.Type()) {
			dst.Set(v)
			return nil
		}
		if v.Type().ConvertibleTo(dst.Type()) {
			dst.Set(v.Convert(dst.Type()))
			return nil
		}
	}
	if dst.Type().Name() == "RawMessage" && dst.Type().PkgPath() == "encoding/json" {
		raw, err := json.Marshal(src)
		if err != nil {
			return err
		}
		dst.SetBytes(raw)
		return nil
	}
	if dst.Kind() == reflect.Pointer {
		if src == nil {
			dst.Set(reflect.Zero(dst.Type()))
			return nil
		}
		dst.Set(reflect.New(dst.Type().Elem()))
		return assignSPValue(dst.Elem(), src)
	}
	if dst.Kind() == reflect.Interface {
		n, err := spNative(src)
		if err != nil {
			return err
		}
		if n == nil {
			dst.Set(reflect.Zero(dst.Type()))
		} else {
			dst.Set(reflect.ValueOf(n))
		}
		return nil
	}
	switch dst.Kind() {
	case reflect.Struct:
		m, ok := src.(map[string]spValue)
		if !ok {
			return fmt.Errorf("expected object")
		}
		for k, v := range m {
			f := fieldByJSONName(dst, k)
			if !f.IsValid() || !f.CanSet() {
				continue
			}
			if err := assignSPValue(f, v); err != nil {
				return fmt.Errorf("%s: %w", k, err)
			}
		}
		return nil
	case reflect.Slice:
		if src == nil {
			dst.Set(reflect.Zero(dst.Type()))
			return nil
		}
		a, ok := src.([]spValue)
		if !ok {
			return fmt.Errorf("expected array for %s, got %T", dst.Type(), src)
		}
		dst.Set(reflect.MakeSlice(dst.Type(), len(a), len(a)))
		for i, v := range a {
			if err := assignSPValue(dst.Index(i), v); err != nil {
				return err
			}
		}
		return nil
	case reflect.Map:
		if src == nil {
			dst.Set(reflect.Zero(dst.Type()))
			return nil
		}
		m, ok := src.(map[string]spValue)
		if !ok {
			return fmt.Errorf("expected object")
		}
		dst.Set(reflect.MakeMap(dst.Type()))
		for k, v := range m {
			e := reflect.New(dst.Type().Elem()).Elem()
			if err := assignSPValue(e, v); err != nil {
				return err
			}
			dst.SetMapIndex(reflect.ValueOf(k).Convert(dst.Type().Key()), e)
		}
		return nil
	case reflect.String:
		n, ok := src.(string)
		if !ok {
			return fmt.Errorf("expected string")
		}
		dst.SetString(n)
		return nil
	case reflect.Bool:
		n, ok := src.(bool)
		if !ok {
			return fmt.Errorf("expected bool")
		}
		dst.SetBool(n)
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, ok := src.(float64)
		if !ok {
			return fmt.Errorf("expected number")
		}
		dst.SetInt(int64(n))
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, ok := src.(float64)
		if !ok {
			return fmt.Errorf("expected number")
		}
		dst.SetUint(uint64(n))
		return nil
	case reflect.Float32, reflect.Float64:
		n, ok := src.(float64)
		if !ok {
			return fmt.Errorf("expected number")
		}
		dst.SetFloat(n)
		return nil
	}
	return fmt.Errorf("unsupported field type %s", dst.Type())
}
func spNative(v spValue) (any, error) {
	switch x := v.(type) {
	case map[string]spValue:
		m := map[string]any{}
		for k, v := range x {
			n, err := spNative(v)
			if err != nil {
				return nil, err
			}
			m[k] = n
		}
		return m, nil
	case []spValue:
		a := make([]any, len(x))
		for i, v := range x {
			n, err := spNative(v)
			if err != nil {
				return nil, err
			}
			a[i] = n
		}
		return a, nil
	default:
		return x, nil
	}
}

func decodeLegacyBase64(s string) ([]byte, error) {
	// Kept local to the compatibility importer; writers do not depend on it.
	const enc = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var out []byte
	var val, bits int
	for _, c := range s {
		if c == '=' {
			break
		}
		i := strings.IndexRune(enc, c)
		if i < 0 {
			return nil, fmt.Errorf("invalid base64 character")
		}
		val = (val << 6) | i
		bits += 6
		if bits >= 8 {
			bits -= 8
			out = append(out, byte(val>>bits))
			val &= (1 << bits) - 1
		}
	}
	return out, nil
}

// FormatSemanticSP canonicalizes an SP document through SemanticProgram.
func FormatSemanticSP(data []byte) ([]byte, error) {
	p, err := ParseSemanticSP(data)
	if err != nil {
		return nil, err
	}
	return p.MarshalSemanticSP()
}

// MarshalSemanticSPZ stores canonical SP bytes in a small independent RLE
// block stream. It has no external compression dependency.
func (p *SemanticProgram) MarshalSemanticSPZ() ([]byte, error) {
	sp, err := p.MarshalSemanticSP()
	if err != nil {
		return nil, err
	}
	return encodeSPZ(sp), nil
}

func ParseSemanticSPZ(data []byte) (*SemanticProgram, error) {
	plain, err := decodeSPZ(data)
	if err != nil {
		return nil, err
	}
	return ParseSemanticSP(plain)
}

func encodeSPZ(in []byte) []byte {
	var b bytes.Buffer
	b.WriteString("SPZ2")
	last := map[uint32]int{}
	for i := 0; i < len(in); {
		bestLen, bestOff := 0, 0
		if i+3 < len(in) {
			key := uint32(in[i])<<16 | uint32(in[i+1])<<8 | uint32(in[i+2])
			if p, ok := last[key]; ok && i-p <= 65535 {
				n := 0
				for n < 127 && i+n < len(in) && in[p+n] == in[i+n] {
					n++
				}
				if n >= 4 {
					bestLen = n
					bestOff = i - p
				}
			}
			last[key] = i
		}
		if bestLen >= 4 {
			b.WriteByte(0x80 | byte(bestLen-4))
			b.WriteByte(byte(bestOff >> 8))
			b.WriteByte(byte(bestOff))
			for k := 0; k < bestLen && i+k+2 < len(in); k++ {
				key := uint32(in[i+k])<<16 | uint32(in[i+k+1])<<8 | uint32(in[i+k+2])
				last[key] = i + k
			}
			i += bestLen
			continue
		}
		start := i
		i++
		for i < len(in) && i-start < 127 {
			if i+3 < len(in) {
				key := uint32(in[i])<<16 | uint32(in[i+1])<<8 | uint32(in[i+2])
				if p, ok := last[key]; ok && i-p <= 65535 {
					n := 0
					for n < 4 && i+n < len(in) && in[p+n] == in[i+n] {
						n++
					}
					if n >= 4 {
						break
					}
				}
				last[key] = i
			}
			i++
		}
		b.WriteByte(byte(i - start))
		b.Write(in[start:i])
	}
	return b.Bytes()
}
func decodeSPZ(in []byte) ([]byte, error) {
	if len(in) < 4 || string(in[:4]) != "SPZ2" {
		return nil, fmt.Errorf("spz: invalid header")
	}
	var b bytes.Buffer
	for i := 4; i < len(in); {
		t := in[i]
		i++
		if t&0x80 == 0 {
			n := int(t)
			if n == 0 || i+n > len(in) {
				return nil, fmt.Errorf("spz: truncated literal")
			}
			b.Write(in[i : i+n])
			i += n
			continue
		}
		if i+1 >= len(in) {
			return nil, fmt.Errorf("spz: truncated match")
		}
		n := int(t&0x7f) + 4
		off := int(in[i])<<8 | int(in[i+1])
		i += 2
		if off == 0 || off > b.Len() {
			return nil, fmt.Errorf("spz: invalid offset")
		}
		start := b.Len() - off
		for k := 0; k < n; k++ {
			data := b.Bytes()
			b.WriteByte(data[start+k%off])
		}
	}
	return b.Bytes(), nil
}

func FormatSemanticSPZ(data []byte) ([]byte, error) {
	p, err := ParseSemanticSPZ(data)
	if err != nil {
		return nil, err
	}
	return p.MarshalSemanticSPZ()
}
