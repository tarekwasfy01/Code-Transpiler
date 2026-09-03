package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type result struct{ Language, CaseIndex, CaseName, State, FailureKind, Diagnostic string }

func main() {
	root := os.Getenv("CORPUS_INPUT")
	if root == "" {
		root = "matrices/tree_sitter_full/15_corpus_cases.csv"
		if _, statErr := os.Stat(root); statErr != nil {
			root = "matrices/frontend_closure/tree_sitter_input/15_corpus_cases.csv"
		}
	}
	out := os.Getenv("CORPUS_OUT")
	if out == "" {
		out = "outputs/generic-corpus-fixpoint"
	}
	os.MkdirAll(out, 0755)
	f, e := os.Open(root)
	if e != nil {
		fmt.Fprintf(os.Stderr, "generic-corpus-fixpoint: corpus input unavailable (%s): %v\n", root, e)
		return
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	h, e := r.Read()
	if e != nil {
		panic(e)
	}
	ix := map[string]int{}
	for i, k := range h {
		ix[k] = i
	}
	type job struct {
		n   int
		row []string
	}
	var jobs []job
	for n := 0; n < 1744; n++ {
		row, e := r.Read()
		if e != nil {
			break
		}
		jobs = append(jobs, job{n, row})
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].row[ix["language"]] < jobs[j].row[ix["language"]] })
	stateFile := filepath.Join(out, "checkpoint.json")
	done := map[int]bool{}
	if b, e := os.ReadFile(stateFile); e == nil {
		_ = json.Unmarshal(b, &done)
	}
	path := filepath.Join(out, "results.csv")
	exists := false
	completedIDs := map[int]bool{}
	if _, e := os.Stat(path); e == nil {
		exists = true
		if rf, e := os.Open(path); e == nil {
			rr := csv.NewReader(rf)
			rr.FieldsPerRecord = -1
			_, _ = rr.Read()
			for {
				row, er := rr.Read()
				if er != nil {
					break
				}
				if len(row) > 0 {
					if id, er := strconv.Atoi(row[0]); er == nil {
						completedIDs[id] = true
					}
				}
			}
			rf.Close()
		}
	}
	of, _ := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	defer of.Close()
	w := csv.NewWriter(of)
	if !exists {
		w.Write([]string{"case_id", "language", "case_index", "case_name", "state", "failure_kind", "diagnostic"})
		w.Flush()
	}
	var mu sync.Mutex
	queue := make(chan job)
	var wg sync.WaitGroup
	workers := 6
	timeout := 15 * time.Second
	if raw := os.Getenv("CORPUS_TIMEOUT_SECONDS"); raw != "" {
		if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
			timeout = time.Duration(seconds) * time.Second
		}
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range queue {
				mu.Lock()
				skip := done[j.n] || completedIDs[j.n]
				mu.Unlock()
				runtime.Gosched()
				if skip {
					continue
				}
				lang := j.row[ix["language"]]
				src := j.row[ix["source_text"]]
				// Corpus records contain source followed by a delimiter and the
				// expected Tree-sitter tree. Only the source segment belongs to
				// the parser; feeding the oracle tree back as input caused the
				// apparent timeout cluster.
				if cut := strings.Index(src, "\n--------------------------------------------------------------------------------\n"); cut >= 0 {
					src = src[:cut]
				}
				ctx, cancel := context.WithTimeout(context.Background(), timeout)
				_, ok, err := matrixir.NewGenericLexerLREngine(lang).ParseRealContext(ctx, src, "matrices/REAL_TS_MATRIX/execution_ready")
				cancel()
				var z result
				if err == context.DeadlineExceeded || err == context.Canceled {
					z = result{State: "TIMEOUT", FailureKind: "TIMEOUT", Diagnostic: "case timeout"}
				} else if ok {
					z = result{State: "PASS"}
				} else {
					k := "ACTION"
					d := "parse failed"
					if err != nil {
						d = err.Error()
						if strings.Contains(d, "lex:") {
							k = "LEXER"
						} else if strings.Contains(d, "goto") {
							k = "GOTO"
						} else if strings.Contains(d, "recovery") {
							k = "ACTION"
						}
					}
					z = result{State: "FAILURE", FailureKind: k, Diagnostic: d}
				}
				mu.Lock()
				w.Write([]string{strconv.Itoa(j.n), lang, j.row[ix["case_index"]], j.row[ix["case_name"]], z.State, z.FailureKind, z.Diagnostic})
				w.Flush()
				done[j.n] = true
				b, _ := json.Marshal(done)
				_ = os.WriteFile(stateFile, b, 0644)
				mu.Unlock()
			}
		}()
	}
	for _, j := range jobs {
		queue <- j
	}
	close(queue)
	wg.Wait()
	fmt.Printf("CORPUS_TOTAL=%d COMPLETED=%d\n", len(jobs), len(done))
}
