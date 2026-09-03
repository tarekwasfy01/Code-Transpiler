package runtimeassets

import (
	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
	"os"
	"path/filepath"
	"testing"
)

func TestMaterializedRuntimeIsActualEmbeddedSource(t *testing.T) {
	for _, target := range Targets() {
		files, err := List(target)
		if err != nil {
			t.Fatal(err)
		}
		want, err := backend.RuntimeSource(target)
		if err != nil {
			t.Fatal(err)
		}
		dir, err := Materialize(target, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(dir, files[0].Name))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want || len(got) == 0 {
			t.Fatalf("runtime mismatch: %s", target)
		}
	}
	for _, target := range []string{"../outside", "c/../../outside", "unknown"} {
		if _, err := Materialize(target, t.TempDir()); err == nil {
			t.Fatalf("invalid target accepted: %s", target)
		}
	}
}
