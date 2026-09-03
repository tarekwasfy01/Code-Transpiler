// uast-corpus-transpile streams a source corpus through the productive
// source->UAST->target path. It never executes corpus source or generated
// output: the result is an evidence/error matrix keyed only by hashes and
// normalized diagnostics.
package main

import (
	"bufio"
	"compress/gzip"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tarekwasfy01/Code-Transpiler/internal/manytomany"
)

type corpusRecord struct {
	CorpusRecordID string `json:"corpus_record_id"`
	LanguageID     string `json:"language_id"`
	SourceHash     string `json:"source_hash"`
	SourceCode     string `json:"source_code"`
	ParserErrors   int    `json:"parser_error_count"`
}

type summary struct {
	Input          string `json:"input"`
	Target         string `json:"target"`
	Read           int    `json:"read"`
	Succeeded      int    `json:"succeeded"`
	Failed         int    `json:"failed"`
	SourceExecuted bool   `json:"source_executed"`
}

func diagnostic(err error) string {
	if err == nil {
		return ""
	}
	s := strings.Join(strings.Fields(err.Error()), " ")
	if len(s) > 180 {
		s = s[:180]
	}
	return s
}

func writeCSV(path string, header []string, rows [][]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write(header); err != nil {
		return err
	}
	for _, row := range rows {
		if err := w.Write(row); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

func main() {
	input := flag.String("input", "", "MLCPD .jsonl.gz record stream")
	target := flag.String("target", "go", "registered target language")
	out := flag.String("out", "outputs/runtime-direct-promotion/corpus", "source-free matrix output directory")
	limit := flag.Int("limit", 0, "maximum records; 0 processes every record")
	checkpoint := flag.Int("checkpoint", 1000, "write summary and matrices after this many records")
	flag.Parse()
	if *input == "" {
		panic("-input is required")
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		panic(err)
	}
	f, err := os.Open(*input)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		panic(err)
	}
	defer gz.Close()

	s := summary{Input: filepath.Clean(*input), Target: *target, SourceExecuted: false}
	errors := map[string]int{}
	successes := map[string]int{}
	flush := func() error {
		errorKeys := make([]string, 0, len(errors))
		for key := range errors {
			errorKeys = append(errorKeys, key)
		}
		sort.Strings(errorKeys)
		errorRows := make([][]string, 0, len(errorKeys))
		for _, key := range errorKeys {
			parts := strings.SplitN(key, "|", 2)
			errorRows = append(errorRows, []string{parts[0], *target, parts[1], fmt.Sprintf("%d", errors[key])})
		}
		if err := writeCSV(filepath.Join(*out, "transpile_error_matrix.csv"), []string{"source_language", "target", "failure_signature", "occurrences"}, errorRows); err != nil {
			return err
		}
		successKeys := make([]string, 0, len(successes))
		for key := range successes {
			successKeys = append(successKeys, key)
		}
		sort.Strings(successKeys)
		successRows := make([][]string, 0, len(successKeys))
		for _, key := range successKeys {
			successRows = append(successRows, []string{key, *target, fmt.Sprintf("%d", successes[key])})
		}
		if err := writeCSV(filepath.Join(*out, "transpile_success_matrix.csv"), []string{"source_language", "target", "occurrences"}, successRows); err != nil {
			return err
		}
		data, err := json.MarshalIndent(s, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(*out, "transpile_summary.json"), data, 0o644)
	}

	scanner := bufio.NewScanner(gz)
	scanner.Buffer(make([]byte, 64*1024), 32*1024*1024)
	for scanner.Scan() {
		if *limit > 0 && s.Read >= *limit {
			break
		}
		var record corpusRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			s.Read++
			s.Failed++
			errors["unknown|INVALID_CORPUS_RECORD"]++
			continue
		}
		s.Read++
		language := strings.TrimSpace(record.LanguageID)
		if language == "" {
			language = "unknown"
		}
		if _, err := manytomany.Transpile(language, *target, record.SourceCode); err != nil {
			s.Failed++
			errors[language+"|"+diagnostic(err)]++
		} else {
			s.Succeeded++
			successes[language]++
		}
		if *checkpoint > 0 && s.Read%*checkpoint == 0 {
			if err := flush(); err != nil {
				panic(err)
			}
			fmt.Printf("processed=%d succeeded=%d failed=%d\n", s.Read, s.Succeeded, s.Failed)
		}
	}
	if err := scanner.Err(); err != nil {
		panic(err)
	}
	if err := flush(); err != nil {
		panic(err)
	}
	fmt.Printf("complete processed=%d succeeded=%d failed=%d source_executed=false\n", s.Read, s.Succeeded, s.Failed)
}
