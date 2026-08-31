// Package runtimeassets exposes the runtime source already compiled into the
// executable. No separately installed compiler is represented as bundled.
package runtimeassets

import (
	"fmt"
	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
	"os"
	"path/filepath"
)

type File struct {
	Name string
	Data []byte
}

func Targets() []string {
	out := make([]string, len(backend.Languages))
	for i, l := range backend.Languages {
		out[i] = l.ID
	}
	return out
}
func List(target string) ([]File, error) {
	l, ok := backend.ByID(target)
	if !ok {
		return nil, fmt.Errorf("unknown runtime target %q", target)
	}
	code, err := backend.RuntimeSource(target)
	if err != nil {
		return nil, err
	}
	return []File{{Name: "runtime" + l.Extension, Data: []byte(code)}}, nil
}
func Materialize(target, base string) (string, error) {
	files, err := List(target)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "runtime-"+target)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	for _, f := range files {
		if err = os.WriteFile(filepath.Join(dir, f.Name), f.Data, 0600); err != nil {
			return "", err
		}
	}
	return dir, nil
}
