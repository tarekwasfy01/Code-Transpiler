// Copyright (c) 2026 Tarek Wasfy
// Bootstrap Go standard-library packages into the Semantic module store.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/tarekwasfy01/Code-Transpiler/v2/v2/internal/backend"
)

func main() {
	root := flag.String("goroot", runtime.GOROOT(), "Go root")
	workers := flag.Int("workers", 6, "parallel package workers")
	flag.Parse()
	src := filepath.Join(*root, "src")
	store, err := backend.DefaultSemanticModuleStore()
	if err != nil {
		panic(err)
	}
	var dirs []string
	_ = filepath.WalkDir(src, func(path string, d os.DirEntry, e error) error {
		if e != nil || !d.IsDir() {
			return e
		}
		rel, _ := filepath.Rel(src, path)
		if rel == "." || strings.Contains(rel, string(filepath.Separator)+"internal"+string(filepath.Separator)) || strings.Contains(rel, string(filepath.Separator)+"vendor"+string(filepath.Separator)) {
			return nil
		}
		matches, _ := filepath.Glob(filepath.Join(path, "*.go"))
		for _, f := range matches {
			if !strings.HasSuffix(f, "_test.go") {
				dirs = append(dirs, path)
				break
			}
		}
		return nil
	})
	seen := map[string]bool{}
	uniq := dirs[:0]
	for _, d := range dirs {
		if !seen[d] {
			seen[d] = true
			uniq = append(uniq, d)
		}
	}
	dirs = uniq
	jobs := make(chan string)
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, fail := 0, 0
	var firstErr string
	worker := func() {
		defer wg.Done()
		for d := range jobs {
			var e error
			files, _ := filepath.Glob(filepath.Join(d, "*.go"))
			if len(files) == 0 {
				e = fmt.Errorf("no go files")
			} else {
				data, re := os.ReadFile(files[0])
				e = re
				if e == nil {
					var prog *backend.SemanticProgram
					prog, e = backend.LowerSource("go", files[0], string(data))
					if e == nil {
						var m *backend.SemanticModule
						m, e = backend.NewSemanticModule(filepath.ToSlash(strings.TrimPrefix(d, src+string(filepath.Separator))), "go", string(data), prog)
						if e == nil {
							e = store.SaveModule(m)
						}
					}
				}
			}
			mu.Lock()
			if e != nil {
				fail++
				if firstErr == "" {
					firstErr = filepath.Base(d) + ": " + e.Error()
				}
			} else {
				ok++
			}
			mu.Unlock()
		}
	}
	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go worker()
	}
	for _, d := range dirs {
		jobs <- d
	}
	close(jobs)
	wg.Wait()
	fmt.Printf("STDlib packages=%d imported=%d failed=%d store=%s first_error=%s\n", len(dirs), ok, fail, store.Root, firstErr)
}
