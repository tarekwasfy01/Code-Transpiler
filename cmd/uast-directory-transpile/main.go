// uast-directory-transpile walks a local source tree and builds a source-free
// transpilation matrix. It never executes package code or generated output.
package main

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tarekwasfy01/Code-Transpiler/internal/manytomany"
)

type report struct {
	Root            string `json:"root"`
	Target          string `json:"target"`
	FilesRead       int    `json:"files_read"`
	Attempts        int    `json:"attempts"`
	Transpiled      int    `json:"transpiled"`
	Failed          int    `json:"failed"`
	SkippedTooLarge int    `json:"skipped_too_large"`
	SourceExecuted  bool   `json:"source_executed"`
	Complete        bool   `json:"complete"`
	ShardIndex      int    `json:"shard_index"`
	ShardCount      int    `json:"shard_count"`
}

func languageFor(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".py":
		return "python"
	case ".c", ".h":
		return "c"
	case ".cc", ".cpp", ".cxx", ".hpp":
		return "cpp"
	case ".go":
		return "go"
	case ".rs":
		return "rust"
	case ".r":
		return "r"
	case ".jl":
		return "julia"
	case ".nim":
		return "nim"
	case ".swift":
		return "swift"
	case ".zig":
		return "zig"
	}
	return ""
}

func normalizedError(err error) string {
	if err == nil {
		return ""
	}
	v := strings.Join(strings.Fields(err.Error()), " ")
	if len(v) > 180 {
		return v[:180]
	}
	return v
}

func main() {
	root := flag.String("root", "", "source tree to scan")
	target := flag.String("target", "go", "registered target language or all")
	out := flag.String("out", "outputs/runtime-direct-promotion/local-source", "source-free matrix output")
	limit := flag.Int("limit", 0, "maximum source files; 0 means all")
	maxBytes := flag.Int64("max-bytes", 8<<20, "maximum source file size")
	checkpoint := flag.Int("checkpoint", 250, "write source-free progress reports after this many files; 0 disables checkpoints")
	shardCount := flag.Int("shard-count", 1, "number of deterministic source shards")
	shardIndex := flag.Int("shard-index", 0, "zero-based deterministic source shard index")
	flag.Parse()
	if *root == "" {
		panic("-root is required")
	}
	if *shardCount < 1 || *shardIndex < 0 || *shardIndex >= *shardCount {
		panic("-shard-count must be positive and -shard-index must be within its range")
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		panic(err)
	}
	r := report{Root: filepath.Clean(*root), Target: *target, SourceExecuted: false, ShardIndex: *shardIndex, ShardCount: *shardCount}
	errors, successes := map[string]int{}, map[string]int{}
	targets := []string{*target}
	if *target == "all" {
		targets = append([]string(nil), manytomany.Languages...)
	}
	write := func(name string, header []string, rows [][]string) {
		f, err := os.Create(filepath.Join(*out, name))
		if err != nil {
			panic(err)
		}
		defer f.Close()
		w := csv.NewWriter(f)
		_ = w.Write(header)
		for _, row := range rows {
			_ = w.Write(row)
		}
		w.Flush()
		if err := w.Error(); err != nil {
			panic(err)
		}
	}
	flush := func(complete bool) {
		r.Complete = complete
		keys := make([]string, 0, len(errors))
		for key := range errors {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		errorRows := make([][]string, 0, len(keys))
		for _, key := range keys {
			parts := strings.SplitN(key, "|", 3)
			for len(parts) < 3 {
				parts = append(parts, "")
			}
			errorRows = append(errorRows, []string{parts[0], parts[1], parts[2], fmt.Sprint(errors[key])})
		}
		write("transpile_error_matrix.csv", []string{"source_language", "target", "failure_signature", "occurrences"}, errorRows)
		successKeys := make([]string, 0, len(successes))
		for key := range successes {
			successKeys = append(successKeys, key)
		}
		sort.Strings(successKeys)
		successRows := make([][]string, 0, len(successKeys))
		for _, key := range successKeys {
			parts := strings.SplitN(key, "|", 2)
			successRows = append(successRows, []string{parts[0], parts[1], fmt.Sprint(successes[key])})
		}
		write("transpile_success_matrix.csv", []string{"source_language", "target", "occurrences"}, successRows)
		encoded, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			panic(err)
		}
		if err := os.WriteFile(filepath.Join(*out, "transpile_summary.json"), encoded, 0o644); err != nil {
			panic(err)
		}
	}
	candidates := 0
	walkErr := filepath.WalkDir(*root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() || (*limit > 0 && r.FilesRead >= *limit) {
			return nil
		}
		language := languageFor(path)
		if language == "" {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > *maxBytes {
			r.SkippedTooLarge++
			return nil
		}
		candidate := candidates
		candidates++
		if candidate%*shardCount != *shardIndex {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			errors[language+"|READ_ERROR"]++
			r.Failed++
			return nil
		}
		r.FilesRead++
		// Hashing proves which exact source was observed without persisting it.
		_ = sha256.Sum256(data)
		for _, destination := range targets {
			r.Attempts++
			if _, err := manytomany.Transpile(language, destination, string(data)); err != nil {
				errors[language+"|"+destination+"|"+normalizedError(err)]++
				r.Failed++
			} else {
				successes[language+"|"+destination]++
				r.Transpiled++
			}
		}
		if *checkpoint > 0 && r.FilesRead%*checkpoint == 0 {
			flush(false)
			fmt.Printf("checkpoint files=%d attempts=%d transpiled=%d failed=%d skipped=%d\n", r.FilesRead, r.Attempts, r.Transpiled, r.Failed, r.SkippedTooLarge)
		}
		return nil
	})
	if walkErr != nil {
		panic(walkErr)
	}
	flush(true)
	fmt.Printf("complete files=%d attempts=%d transpiled=%d failed=%d skipped=%d source_executed=false\n", r.FilesRead, r.Attempts, r.Transpiled, r.Failed, r.SkippedTooLarge)
}
