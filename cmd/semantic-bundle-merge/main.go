// Copyright (c) 2026 Tarek Wasfy
package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

)

type member struct {
	Path   string          `json:"path"`
	SHA256 string          `json:"sha256"`
	SE     string          `json:"se_gzip_base64"`
}

func main() {
	root := flag.String("root", "", "directory containing the individual .se files")
	list := flag.String("list", "", "newline-separated member paths relative to root")
	carrier := flag.String("carrier", "", "valid single-program .se carrier")
	out := flag.String("o", "", "merged .se output")
	flag.Parse()
	if *root == "" || *list == "" || *carrier == "" || *out == "" {
		fatal("usage: semantic-bundle-merge -root <dir> -list <file> -carrier <file> -o <file>")
	}

	carrierBytes, err := os.ReadFile(*carrier)
	if err != nil { fatal("read carrier: %v", err) }

	listBytes, err := os.ReadFile(*list)
	if err != nil { fatal("read list: %v", err) }
	paths := make([]string, 0)
	for _, line := range strings.Split(strings.ReplaceAll(string(listBytes), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") { continue }
		paths = append(paths, line)
	}
	sort.Strings(paths)
	members := make([]member, 0, len(paths))
	for _, rel := range paths {
		path := filepath.Join(*root, rel)
		data, err := os.ReadFile(path)
		if err != nil { fatal("read member %s: %v", rel, err) }
		sum := sha256.Sum256(data)
		var memberPacked bytes.Buffer
		zw := gzip.NewWriter(&memberPacked)
		if _, err = zw.Write(data); err == nil { err = zw.Close() }
		if err != nil { fatal("compress member %s: %v", rel, err) }
		members = append(members, member{Path: rel, SHA256: hex.EncodeToString(sum[:]), SE: base64.StdEncoding.EncodeToString(memberPacked.Bytes())})
	}
	index, err := json.Marshal(members)
	if err != nil { fatal("marshal bundle index: %v", err) }
	var packed bytes.Buffer
	zw := gzip.NewWriter(&packed)
	if _, err = zw.Write(index); err == nil { err = zw.Close() }
	if err != nil { fatal("compress bundle index: %v", err) }
	payload := base64.StdEncoding.EncodeToString(packed.Bytes())
	closeAt := bytes.LastIndex(carrierBytes, []byte("\n}\n"))
	if closeAt < 0 { fatal("carrier has no final program block") }
	var marker strings.Builder
	marker.WriteString("\n# semantic_bundle_members_v1 encoding=json+gzip+base64 count=")
	marker.WriteString(fmt.Sprint(len(members)))
	marker.WriteString(" sha256=")
	marker.WriteString(fmt.Sprintf("%x", sha256.Sum256(index)))
	marker.WriteByte('\n')
	for len(payload) > 0 {
		n := 120
		if len(payload) < n { n = len(payload) }
		marker.WriteString("# ")
		marker.WriteString(payload[:n])
		marker.WriteByte('\n')
		payload = payload[n:]
	}
	result := make([]byte, 0, len(carrierBytes)+marker.Len())
	result = append(result, carrierBytes[:closeAt]...)
	result = append(result, carrierBytes[closeAt:]...)
	result = append(result, marker.String()...)
	if err = os.WriteFile(*out, result, 0o644); err != nil { fatal("write output: %v", err) }
	fmt.Printf("MERGED_MEMBERS=%d\nMERGED_BYTES=%d\nOUTPUT=%s\n", len(members), len(result), *out)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "semantic-bundle-merge: "+format+"\n", args...)
	os.Exit(1)
}
