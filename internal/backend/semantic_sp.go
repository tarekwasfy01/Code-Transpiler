// Copyright (c) 2026 Tarek Wasfy
package backend

// Semantic Programming (SP) is a readable, versioned transport envelope for
// the canonical SemanticProgram/UAST JSON.  The JSON payload remains the
// lossless authority; the envelope deliberately does not introduce another IR.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const SemanticSPVersion = 1

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
	if len(parts) != 2 || parts[0] != "sp" {
		return nil, fmt.Errorf("sp:%d: expected 'sp <version>'", lineNo)
	}
	version, err := strconv.Atoi(parts[1])
	if err != nil || version != SemanticSPVersion {
		return nil, fmt.Errorf("sp:%d: unsupported SP version %q", lineNo, parts[1])
	}
	if next() != "program {" {
		return nil, fmt.Errorf("sp:%d: expected program block", lineNo)
	}
	var payload string
	legacyBase64 := false
	structuredFields := map[string]json.RawMessage{}
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
		key, value, ok := strings.Cut(s, "=")
		if !ok {
			return nil, fmt.Errorf("sp:%d: expected key = value", lineNo)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if seen[key] {
			return nil, fmt.Errorf("sp:%d: duplicate program field %q", lineNo, key)
		}
		seen[key] = true
		switch key {
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
			if !strings.HasPrefix(key, "field.") {
				return nil, fmt.Errorf("sp:%d: unknown program field %q", lineNo, key)
			}
			name := strings.TrimPrefix(key, "field.")
			var raw json.RawMessage
			if err := json.Unmarshal([]byte(value), &raw); err != nil {
				return nil, fmt.Errorf("sp:%d: invalid semantic field %q", lineNo, name)
			}
			structuredFields[name] = append([]byte(nil), raw...)
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
	if len(structuredFields) > 0 {
		assembled, e := json.Marshal(structuredFields)
		if e != nil {
			return nil, fmt.Errorf("sp:%d: invalid semantic fields: %w", lineNo, e)
		}
		return ParseSemanticJSON(assembled)
	}
	return ParseSemanticJSON([]byte(payload))
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
