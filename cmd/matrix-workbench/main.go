// matrix-workbench computes current implementation gaps without modifying source.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

type probe struct {
	File     string                  `json:"file"`
	SHA256   string                  `json:"sha256"`
	Status   string                  `json:"status"`
	Error    string                  `json:"error,omitempty"`
	Analysis *backend.NativeAnalysis `json:"analysis,omitempty"`
}
type testEvidence struct {
	Status   string `json:"status"`
	Passed   int    `json:"passed_tests"`
	Failed   int    `json:"failed_tests"`
	Skipped  int    `json:"skipped_tests"`
	Packages int    `json:"passed_packages"`
	Error    string `json:"error,omitempty"`
}
type report struct {
	Schema          string                       `json:"schema"`
	Created         string                       `json:"created"`
	SourceHashes    map[string]string            `json:"source_sha256"`
	Implementation  backend.ImplementationMatrix `json:"implementation"`
	Capabilities    backend.CapabilityMatrix     `json:"capabilities"`
	StageGaps       matrixir.SparseMatrix        `json:"stage_gap_vector"`
	TargetGaps      matrixir.SparseMatrix        `json:"target_gap_vector"`
	FeatureCoupling matrixir.SparseMatrix        `json:"shared_unsupported_targets"`
	Reachability    matrixir.SparseMatrix        `json:"declared_route_reachability"`
	Probes          []probe                      `json:"probes"`
	Tests           testEvidence                 `json:"tests"`
	Limits          []string                     `json:"limits"`
}

func hashFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
func writeJSON(path string, v any) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	e := json.NewEncoder(f)
	e.SetIndent("", "  ")
	err = e.Encode(v)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
func writeCSV(path string, rows [][]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	w.WriteAll(rows)
	err = w.Error()
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func runTests(ctx context.Context, out string) testEvidence {
	r := testEvidence{Status: "FAIL"}
	f, err := os.Create(filepath.Join(out, "tests.jsonl"))
	if err != nil {
		r.Error = err.Error()
		return r
	}
	defer f.Close()
	cmd := exec.CommandContext(ctx, "go", "test", "-json", ".", "./cmd/...", "./internal/...", "-count=1")
	cmd.Env = os.Environ()
	if os.Getenv("GOCACHE") == "" {
		cache, _ := filepath.Abs(".audit-cache/go-build")
		cmd.Env = append(cmd.Env, "GOCACHE="+cache)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		r.Error = err.Error()
		return r
	}
	stderr, err := os.Create(filepath.Join(out, "tests.stderr.txt"))
	if err != nil {
		r.Error = err.Error()
		return r
	}
	defer stderr.Close()
	cmd.Stderr = stderr
	if err = cmd.Start(); err != nil {
		r.Error = err.Error()
		return r
	}
	d := json.NewDecoder(io.TeeReader(stdout, f))
	for {
		var event struct{ Action, Test, Package string }
		err = d.Decode(&event)
		if err == io.EOF {
			break
		}
		if err != nil {
			r.Error = err.Error()
			_, _ = io.Copy(io.Discard, stdout)
			break
		}
		if event.Test != "" {
			switch event.Action {
			case "pass":
				r.Passed++
			case "fail":
				r.Failed++
			case "skip":
				r.Skipped++
			}
		} else if event.Action == "pass" {
			r.Packages++
		}
	}
	if err := cmd.Wait(); err != nil {
		r.Error = err.Error()
	} else if r.Error == "" {
		r.Status = "PASS"
	}
	if ctx.Err() != nil {
		r.Status = "TIMEOUT"
		r.Error = ctx.Err().Error()
	}
	return r
}

func run() error {
	out := flag.String("out", "", "New report directory (must not exist)")
	sources := flag.String("sources", "tests/matrix-workbench", "Directory of native Go *.go.txt analysis probes")
	tests := flag.Bool("test", false, "Run project tests and retain JSON evidence")
	timeout := flag.Duration("timeout", 10*time.Minute, "Project test timeout")
	flag.Parse()
	if *timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	if _, err := os.Stat("go.mod"); err != nil {
		return fmt.Errorf("run from the repository root: %w", err)
	}
	if *out == "" {
		*out = filepath.Join("outputs", "matrix-workbench", time.Now().Format("20060102-150405.000000000"))
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0755); err != nil {
		return err
	}
	if err := os.Mkdir(*out, 0755); err != nil {
		return fmt.Errorf("create fresh report directory: %w", err)
	}
	r := report{Schema: "code-transpiler.matrix-workbench.v1", Created: time.Now().UTC().Format(time.RFC3339), SourceHashes: map[string]string{}, Tests: testEvidence{Status: "NOT_RUN"}, Probes: []probe{}, Limits: []string{
		"Support matrices are compiler declarations, not execution evidence.",
		"Route closure is a candidate graph only; intermediate translation fidelity is not proven.",
		"Probe PASS means native Go analysis succeeded, not executable translation.",
		"Test counts include subtests; skipped external compiler tests are not runtime proof.",
		"The handoff's 310-feature completion score is not inferred from these narrower live matrices.",
	}}
	for _, dir := range []string{"internal", "cmd"} {
		if err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			h, err := hashFile(path)
			if err == nil {
				r.SourceHashes[filepath.ToSlash(path)] = h
			}
			return err
		}); err != nil {
			return err
		}
	}
	rootFiles, err := filepath.Glob("*.go")
	if err != nil {
		return err
	}
	for _, path := range append(rootFiles, "go.mod", "go.sum") {
		h, err := hashFile(path)
		if err != nil {
			return err
		}
		r.SourceHashes[path] = h
	}
	r.Implementation = backend.TypedImplementationMatrix()
	r.Capabilities = backend.SemanticCapabilityMatrix(nil)
	r.StageGaps, err = r.Implementation.Reject(r.Implementation.Operations)
	if err != nil {
		return err
	}
	r.TargetGaps, err = r.Capabilities.RejectedTargets(r.Capabilities.Features)
	if err != nil {
		return err
	}
	r.FeatureCoupling, err = r.Capabilities.Unsupported.Multiply(r.Capabilities.Unsupported.Transpose())
	if err != nil {
		return err
	}
	r.Reachability, err = routeClosure(r.Implementation.Routes)
	if err != nil {
		return err
	}
	files, err := filepath.Glob(filepath.Join(*sources, "*.go.txt"))
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no analysis probes in %s", *sources)
	}
	probeFailures := 0
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		h := sha256.Sum256(b)
		p := probe{File: filepath.ToSlash(path), SHA256: hex.EncodeToString(h[:]), Status: "PASS"}
		p.Analysis, err = (backend.GoNativeFrontend{}).Analyze(path, string(b))
		if err != nil {
			p.Status = "FAIL"
			p.Error = err.Error()
			probeFailures++
		}
		r.Probes = append(r.Probes, p)
		fmt.Printf("Analysis %s: %s\n", p.Status, path)
	}
	rows := [][]string{{"kind", "feature_or_operation", "stage_or_target", "missing"}}
	for i, name := range r.Implementation.Operations {
		for j, stage := range r.Implementation.Stages {
			if r.Implementation.Unsupported.At(i, j) != 0 {
				rows = append(rows, []string{"operation", name, stage, "1"})
			}
		}
	}
	for i, name := range r.Capabilities.Features {
		for j, target := range r.Capabilities.Targets {
			if r.Capabilities.Unsupported.At(i, j) != 0 {
				rows = append(rows, []string{"feature", name, target, "1"})
			}
		}
	}
	if err := writeCSV(filepath.Join(*out, "gaps.csv"), rows); err != nil {
		return err
	}
	// Equal-weight gap counts, no subjective completion percentages.
	priorities := [][]string{{"feature", "missing_targets"}}
	indices := make([]int, len(r.Capabilities.Features))
	counts := make([]int, len(indices))
	for i := range indices {
		indices[i] = i
		for j := range r.Capabilities.Targets {
			counts[i] += int(r.Capabilities.Unsupported.At(i, j))
		}
	}
	sort.SliceStable(indices, func(i, j int) bool { return counts[indices[i]] > counts[indices[j]] })
	for _, i := range indices {
		priorities = append(priorities, []string{r.Capabilities.Features[i], fmt.Sprint(counts[i])})
	}
	if err := writeCSV(filepath.Join(*out, "feature-worklist.csv"), priorities); err != nil {
		return err
	}
	if *tests {
		fmt.Println("Running project tests; evidence will be saved in tests.jsonl.")
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		r.Tests = runTests(ctx, *out)
		cancel()
	}
	if err := writeJSON(filepath.Join(*out, "report.json"), r); err != nil {
		return err
	}
	summary := fmt.Sprintf("# Local matrix calculation\n\nOperations: %d x %d stages; missing cells: %d.\n\nFeatures: %d x %d targets; missing cells: %d.\n\nDeclared complete-operation routes: %d; candidate reachable pairs: %d. Neither count proves runtime correctness.\n\nNative analysis probes: %d PASS, %d FAIL.\n\nProject tests: %s; %d passed tests/subtests, %d failed, %d skipped, %d passed packages.\n\nSee report.json for matrices, source hashes and limitations; gaps.csv and feature-worklist.csv for missing implementations.\n", len(r.Implementation.Operations), len(r.Implementation.Stages), r.Implementation.Unsupported.NonZeros(), len(r.Capabilities.Features), len(r.Capabilities.Targets), r.Capabilities.Unsupported.NonZeros(), r.Implementation.Routes.NonZeros(), r.Reachability.NonZeros(), len(r.Probes)-probeFailures, probeFailures, r.Tests.Status, r.Tests.Passed, r.Tests.Failed, r.Tests.Skipped, r.Tests.Packages)
	if err := os.WriteFile(filepath.Join(*out, "SUMMARY.md"), []byte(summary), 0644); err != nil {
		return err
	}
	fmt.Print(summary)
	fmt.Println("Report:", *out)
	if probeFailures > 0 || (*tests && r.Tests.Status != "PASS") {
		return fmt.Errorf("verification failed; see report")
	}
	return nil
}

// Boolean closure retains actual cycles; it does not invent identity routes.
func routeClosure(routes matrixir.SparseMatrix) (matrixir.SparseMatrix, error) {
	if routes.Rows != routes.Cols {
		return matrixir.SparseMatrix{}, fmt.Errorf("route matrix must be square")
	}
	r := matrixir.NewSparseMatrix(routes.Rows, routes.Cols)
	routes.Each(func(i, j int, v float64) {
		if v != 0 {
			r.Set(i, j, 1)
		}
	})
	for k := 0; k < r.Rows; k++ {
		for i := 0; i < r.Rows; i++ {
			if r.At(i, k) == 0 {
				continue
			}
			for j := 0; j < r.Cols; j++ {
				if r.At(k, j) != 0 {
					r.Set(i, j, 1)
				}
			}
		}
	}
	return r, nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
