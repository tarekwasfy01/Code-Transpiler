package matrixir

import (
	"encoding/csv"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type ParseDispatchEntry struct {
	Language                             string
	ParseState, SymbolID                 int
	SymbolName, SymbolKind, DispatchKind string
	NextState, ActionListID              int
}
type parseDispatchKey struct{ state, symbol int }
type ParseAction struct {
	Language                                                              string
	ActionListID, Ordinal, Count                                          int
	Kind                                                                  string
	TargetState, LhsSymbolID, ChildCount, DynamicPrecedence, ProductionID int
	Reusable                                                              bool
}
type LexDispatchEntry struct {
	Language, LexerFunction                                              string
	LexState, TransitionOrdinal, NextState, AcceptSymbolID, MapCodepoint int
	PredicateKind, Start, End, PredicateID                               string
	EOF, Skip, Immediate                                                 bool
}
type LexModeEntry struct{ ParseState, LexState, ExternalLexState, ReservedWordSetID int }

// SymbolMetadata mirrors the generated symbol table.  It is runtime metadata,
// not a second AST: visibility/named/supertype flags are needed when reducing
// hidden and aliased nodes.
type SymbolMetadata struct {
	Language                  string
	ID                        int
	Name, DisplayName, Kind   string
	Visible, Named, Supertype bool
}
type AliasSequenceEntry struct {
	Language                                string
	ProductionID, ChildIndex, AliasSymbolID int
	AliasSymbolName                         string
}
type ExternalScannerKernel struct {
	Language, ScannerPath, SHA256, Class string
	ValidSymbolRefs, ResultSymbols       []string
	Stateful                             bool
}
type LexPredicateInstruction struct {
	PredicateID        string
	Ordinal            int
	Opcode, Arg1, Arg2 string
}
type LexCharacterRange struct {
	SetID      string
	Start, End int
}
type Production struct {
	Language                     string
	ID                           int
	FieldMapSlice, AliasSequence string
}
type FieldMapEntry struct {
	Language                          string
	ProductionID, ChildIndex, FieldID int
	FieldName                         string
	Inherited                         bool
}
type RealTables struct {
	Parse                []ParseDispatchEntry
	ParseIndex           map[parseDispatchKey]ParseDispatchEntry
	StateRows            map[int][]ParseDispatchEntry
	Actions              []ParseAction
	ActionsByList        map[int][]ParseAction
	Lex                  []LexDispatchEntry
	Modes                []LexModeEntry
	ModeByState          map[int]LexModeEntry
	Predicates           map[string][]LexPredicateInstruction
	CharacterSets        map[string][]LexCharacterRange
	Accepts              map[int]int
	AcceptsByFunction    map[string]int
	Productions          []Production
	FieldMap             []FieldMapEntry
	FieldMapByProduction map[int][]FieldMapEntry
	PrimaryStates        map[int]int
	InitialState         int
	IdentifierSymbol     int
	ErrorSymbol          int
	KeywordCaptureSymbol int
	HasKeywordLexer      bool
	ReservedWords        map[int]map[int]bool
	Symbols              map[int]SymbolMetadata
	Aliases              map[int][]AliasSequenceEntry
	ExternalScanners     *ExternalScannerKernel
	// ExternalValid is the lossless ts_external_scanner_states matrix keyed by
	// the serialized external-lex state.  It is optional for older exports; in
	// that case the parser derives a conservative valid-symbol vector from the
	// dispatch table.
	ExternalValid map[int][]bool
}

func readCSVFile(path string) ([][]string, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	if _, e = r.Read(); e != nil {
		return nil, e
	}
	var o [][]string
	for {
		v, e := r.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		o = append(o, v)
	}
	return o, nil
}

func scanCSVFile(path string, fn func([]string)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	if _, err = r.Read(); err != nil {
		return err
	}
	for {
		row, err := r.Read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		fn(row)
	}
}
func languageTablePath(dir, name, language string) string {
	base := strings.TrimSuffix(name, ".csv") + "_" + language + ".csv"
	partition := filepath.Join(dir, base)
	if _, err := os.Stat(partition); err == nil {
		return partition
	}
	return filepath.Join(dir, name)
}
func intv(s string) int   { n, _ := strconv.Atoi(s); return n }
func boolv(s string) bool { return s == "true" || s == "1" }

// LoadRealTables reads the lossless execution-ready export and filters one language partition.
func LoadRealTables(dir, language string) (RealTables, error) {
	var t RealTables
	t.Predicates = map[string][]LexPredicateInstruction{}
	t.CharacterSets = map[string][]LexCharacterRange{}
	t.Accepts = map[int]int{}
	t.AcceptsByFunction = map[string]int{}
	t.PrimaryStates = map[int]int{}
	t.ReservedWords = map[int]map[int]bool{}
	t.Symbols = map[int]SymbolMetadata{}
	t.Aliases = map[int][]AliasSequenceEntry{}
	if rows, e := readCSVFile(filepath.Join(filepath.Dir(dir), "12_primary_state_ids.csv")); e == nil {
		for _, r := range rows {
			if len(r) >= 3 && r[0] == language {
				t.PrimaryStates[intv(r[1])] = intv(r[2])
			}
		}
	}
	if rows, e := readCSVFile(filepath.Join(dir, "lex_modes.csv")); e != nil {
		return t, e
	} else {
		for _, r := range rows {
			if len(r) >= 4 && r[0] == language {
				reserved := 0
				if len(r) >= 5 {
					reserved = intv(r[4])
				}
				t.Modes = append(t.Modes, LexModeEntry{ParseState: intv(r[1]), LexState: intv(r[2]), ExternalLexState: intv(r[3]), ReservedWordSetID: reserved})
			}
		}
	}
	if e := scanCSVFile(languageTablePath(dir, "parse_dispatch.csv", language), func(r []string) {
		if len(r) >= 8 && strings.TrimPrefix(r[0], "\ufeff") == language {
			state := intv(r[1])
			sym := intv(r[2])
			if t.IdentifierSymbol == 0 && strings.Contains(strings.ToLower(r[3]), "identifier") {
				t.IdentifierSymbol = sym
			}
			t.Parse = append(t.Parse, ParseDispatchEntry{r[0], state, sym, r[3], r[4], r[5], intv(r[6]), intv(r[7])})
		}
	}); e != nil {
		return t, e
	}
	// Older extraction bundles omitted the explicit large parse-table partition
	// for some generated parser layouts.  The optional supplement is produced
	// mechanically from parser.c and is merged without language-specific logic.
	_ = scanCSVFile(languageTablePath(dir, "parse_dispatch_supplement.csv", language), func(r []string) {
		if len(r) >= 8 && strings.TrimPrefix(r[0], "\ufeff") == language {
			state := intv(r[1])
			sym := intv(r[2])
			if t.IdentifierSymbol == 0 && strings.Contains(strings.ToLower(r[3]), "identifier") {
				t.IdentifierSymbol = sym
			}
			t.Parse = append(t.Parse, ParseDispatchEntry{r[0], state, sym, r[3], r[4], r[5], intv(r[6]), intv(r[7])})
		}
	})
	if rows, e := readCSVFile(filepath.Join(dir, "parse_action_lists.csv")); e != nil {
		return t, e
	} else {
		for _, r := range rows {
			if len(r) >= 14 && r[0] == language {
				// Tree-sitter executes the raw shift target from ts_parse_actions.
				// primary_state_ids are incremental-tree metadata, not runtime state aliases.
				t.Actions = append(t.Actions, ParseAction{Language: r[0], ActionListID: intv(r[1]), Ordinal: intv(r[2]), Count: intv(r[3]), Reusable: boolv(r[4]), Kind: r[5], TargetState: intv(r[6]), LhsSymbolID: intv(r[9]), ChildCount: intv(r[11]), DynamicPrecedence: intv(r[12]), ProductionID: intv(r[13])})
			}
		}
	}
	if rows, e := readCSVFile(filepath.Join(dir, "lex_dispatch.csv")); e != nil {
		return t, e
	} else {
		for _, r := range rows {
			if len(r) >= 9 && r[0] == language {
				kind := r[4]
				t.Lex = append(t.Lex, LexDispatchEntry{Language: r[0], LexerFunction: r[1], LexState: intv(r[2]), TransitionOrdinal: intv(r[3]), NextState: intv(r[5]), PredicateKind: kind, PredicateID: r[6], Start: r[7], End: r[7], MapCodepoint: intv(r[7]), Skip: kind == "SKIP"})
			}
		}
	}
	if rows, e := readCSVFile(filepath.Join(dir, "lex_accepts.csv")); e == nil {
		for _, r := range rows {
			if len(r) >= 5 && r[0] == language {
				t.Accepts[intv(r[2])] = intv(r[4])
				t.AcceptsByFunction[r[1]+":"+r[2]] = intv(r[4])
			}
		}
	}
	if rows, e := readCSVFile(filepath.Join(dir, "lex_predicate_bytecode.csv")); e == nil {
		for _, r := range rows {
			if len(r) >= 7 && r[1] == language {
				id := r[0]
				t.Predicates[id] = append(t.Predicates[id], LexPredicateInstruction{id, intv(r[3]), r[4], r[5], r[6]})
			}
		}
	}
	if rows, e := readCSVFile(filepath.Join(dir, "lex_character_sets.csv")); e == nil {
		for _, r := range rows {
			if len(r) >= 5 && r[0] == language {
				id := r[1]
				t.CharacterSets[id] = append(t.CharacterSets[id], LexCharacterRange{id, intv(r[3]), intv(r[4])})
			}
		}
	}
	if rows, e := readCSVFile(filepath.Join(dir, "productions.csv")); e == nil {
		for _, r := range rows {
			if len(r) >= 4 && r[0] == language {
				t.Productions = append(t.Productions, Production{r[0], intv(r[1]), r[2], r[3]})
			}
		}
	}
	if rows, e := readCSVFile(filepath.Join(dir, "field_map.csv")); e == nil {
		for _, r := range rows {
			if len(r) >= 8 && r[0] == language {
				t.FieldMap = append(t.FieldMap, FieldMapEntry{Language: r[0], ProductionID: intv(r[1]), ChildIndex: intv(r[6]), FieldID: intv(r[3]), FieldName: r[4], Inherited: boolv(r[7])})
			}
		}
	}
	if rows, e := readCSVFile(filepath.Join(dir, "symbols.csv")); e == nil {
		for _, r := range rows {
			if len(r) >= 8 && strings.TrimPrefix(r[0], "\ufeff") == language {
				t.Symbols[intv(r[1])] = SymbolMetadata{Language: r[0], ID: intv(r[1]), Name: r[2], DisplayName: r[3], Kind: r[4], Visible: boolv(r[5]), Named: boolv(r[6]), Supertype: boolv(r[7])}
				name := strings.ToLower(r[2] + " " + r[3])
				if strings.Contains(name, "error") && t.ErrorSymbol == 0 {
					t.ErrorSymbol = intv(r[1])
				}
			}
		}
	}
	if rows, e := readCSVFile(filepath.Join(dir, "alias_sequences.csv")); e == nil {
		for _, r := range rows {
			if len(r) >= 5 && strings.TrimPrefix(r[0], "\ufeff") == language {
				a := AliasSequenceEntry{Language: r[0], ProductionID: intv(r[1]), ChildIndex: intv(r[2]), AliasSymbolID: intv(r[3]), AliasSymbolName: r[4]}
				t.Aliases[a.ProductionID] = append(t.Aliases[a.ProductionID], a)
			}
		}
	}
	if rows, e := readCSVFile(filepath.Join(dir, "external_scanner_kernels.csv")); e == nil {
		for _, r := range rows {
			if len(r) >= 8 && strings.TrimPrefix(r[0], "\ufeff") == language {
				t.ExternalScanners = &ExternalScannerKernel{Language: r[0], ScannerPath: r[1], SHA256: r[2], Class: r[3], ValidSymbolRefs: splitList(r[4]), ResultSymbols: splitList(r[5]), Stateful: boolv(r[6])}
				break
			}
		}
	}
	// Some older exports recorded the scanner ABI but omitted the ordered
	// result-symbol map from external_scanner_kernels.csv.  The generated
	// parser still publishes that map in 04_external_symbol_catalog.csv; use
	// it as the lossless fallback so the external scanner receives the exact
	// valid-symbol vector and its result index is translated to the grammar
	// symbol deterministically.  This is table metadata, not a language
	// heuristic, and is loaded once with the immutable table partition.
	if t.ExternalScanners != nil && len(t.ExternalScanners.ResultSymbols) == 0 {
		if rows, e := readCSVFile(filepath.Join(filepath.Dir(dir), "04_external_symbol_catalog.csv")); e == nil {
			type extSym struct {
				id   int
				name string
			}
			var symbols []extSym
			for _, r := range rows {
				if len(r) >= 3 && strings.TrimPrefix(r[0], "\ufeff") == language {
					symbols = append(symbols, extSym{id: intv(r[2]), name: r[1]})
				}
			}
			sort.Slice(symbols, func(i, j int) bool { return symbols[i].id < symbols[j].id })
			for _, s := range symbols {
				if s.id < 0 {
					continue
				}
				for len(t.ExternalScanners.ResultSymbols) <= s.id {
					t.ExternalScanners.ResultSymbols = append(t.ExternalScanners.ResultSymbols, "")
				}
				name := strings.TrimPrefix(s.name, "ts_external_token_")
				name = strings.TrimPrefix(name, "ts_external_token__")
				t.ExternalScanners.ResultSymbols[s.id] = name
			}
		}
	}
	// The valid-symbol bitset is part of the generated parser ABI and is not
	// represented by the compact CSV kernel export.  Load it once from the
	// pinned parser.c so external scanner decisions are driven by the same
	// state matrix as Tree-sitter (never by a language-specific rule).
	if t.ExternalScanners != nil {
		if valid, e := loadExternalScannerStates(filepath.Join(filepath.Dir(dir), "raw_parser_c", language, "parser.c")); e == nil && len(valid) > 0 {
			t.ExternalValid = valid
		}
	}
	if rows, e := readCSVFile(filepath.Join(dir, "language_runtime_metadata.csv")); e == nil {
		for _, r := range rows {
			if len(r) >= 5 && strings.TrimPrefix(r[0], "\ufeff") == language {
				t.HasKeywordLexer = boolv(r[2])
				t.KeywordCaptureSymbol = intv(r[4])
				break
			}
		}
	}
	// Optional supplements produced from ABI-15 parser.c sources. Older table
	// bundles simply omit them; runtime semantics remain table-driven.
	if rows, e := readCSVFile(filepath.Join(dir, "lex_reserved_word_sets.csv")); e == nil {
		byState := map[int]int{}
		for _, r := range rows {
			if len(r) >= 3 && strings.TrimPrefix(r[0], "\ufeff") == language {
				byState[intv(r[1])] = intv(r[2])
			}
		}
		for i := range t.Modes {
			if v, ok := byState[t.Modes[i].ParseState]; ok {
				t.Modes[i].ReservedWordSetID = v
			}
		}
	}
	if rows, e := readCSVFile(filepath.Join(dir, "reserved_words.csv")); e == nil {
		for _, r := range rows {
			if len(r) >= 3 && strings.TrimPrefix(r[0], "\ufeff") == language {
				setID, symbolID := intv(r[1]), intv(r[2])
				if t.ReservedWords[setID] == nil {
					t.ReservedWords[setID] = map[int]bool{}
				}
				t.ReservedWords[setID][symbolID] = true
			}
		}
	}
	// Build direct dispatch indexes once. The source CSV is grouped by
	// language/state but not guaranteed to order symbols; indexing avoids a
	// costly million-row sort for every language partition and gives recovery
	// an O(number-of-actions-in-state) view of the table.
	t.ParseIndex = make(map[parseDispatchKey]ParseDispatchEntry, len(t.Parse))
	t.StateRows = make(map[int][]ParseDispatchEntry)
	t.ModeByState = make(map[int]LexModeEntry, len(t.Modes))
	t.FieldMapByProduction = make(map[int][]FieldMapEntry)
	for _, fm := range t.FieldMap {
		if fm.Language == language {
			t.FieldMapByProduction[fm.ProductionID] = append(t.FieldMapByProduction[fm.ProductionID], fm)
		}
	}
	for _, m := range t.Modes {
		t.ModeByState[m.ParseState] = m
	}
	for _, d := range t.Parse {
		key := parseDispatchKey{state: d.ParseState, symbol: d.SymbolID}
		if _, exists := t.ParseIndex[key]; !exists {
			t.ParseIndex[key] = d
		}
		t.StateRows[d.ParseState] = append(t.StateRows[d.ParseState], d)
	}
	sort.Slice(t.Actions, func(i, j int) bool {
		if t.Actions[i].ActionListID != t.Actions[j].ActionListID {
			return t.Actions[i].ActionListID < t.Actions[j].ActionListID
		}
		return t.Actions[i].Ordinal < t.Actions[j].Ordinal
	})
	t.ActionsByList = make(map[int][]ParseAction)
	for _, a := range t.Actions {
		t.ActionsByList[a.ActionListID] = append(t.ActionsByList[a.ActionListID], a)
	}
	sort.SliceStable(t.Lex, func(i, j int) bool {
		if t.Lex[i].LexerFunction != t.Lex[j].LexerFunction {
			return t.Lex[i].LexerFunction < t.Lex[j].LexerFunction
		}
		if t.Lex[i].LexState != t.Lex[j].LexState {
			return t.Lex[i].LexState < t.Lex[j].LexState
		}
		return t.Lex[i].TransitionOrdinal < t.Lex[j].TransitionOrdinal
	})
	// The generated Tree-sitter parsers all enter through state 1.  State 0
	// is a recovery/sentinel state; selecting the first executable row (as an
	// earlier bootstrap did) starts Kotlin/R and other partitions in the
	// middle of the automaton and produces immediate false reductions.
	if len(t.Parse) > 0 {
		t.InitialState = 1
	}
	return t, nil
}

func loadExternalScannerStates(path string) (map[int][]bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// Keep this extractor deliberately narrow: generated parser.c uses a
	// regular array initializer with one brace block per external state.
	re := regexp.MustCompile(`(?s)ts_external_scanner_states\[[^\]]+\]\[EXTERNAL_TOKEN_COUNT\]\s*=\s*\{(.*?)\n\};`)
	m := re.FindSubmatch(b)
	if len(m) < 2 {
		return nil, nil
	}
	stateRE := regexp.MustCompile(`(?s)\[(\d+)\]\s*=\s*\{(.*?)\}`)
	bitRE := regexp.MustCompile(`\[([A-Za-z0-9_]+)\]\s*=\s*true`)
	// ABI-14/15 generated files use enum identifiers rather than numeric
	// indexes in the bitset initializer.  Recover that enum once so the
	// scanner receives the exact token-index vector.
	nameToID := map[string]int{}
	if em := regexp.MustCompile(`(?s)enum\s+ts_external_scanner_symbol_identifiers\s*\{(.*?)\};`).FindSubmatch(b); len(em) >= 2 {
		enumRE := regexp.MustCompile(`(ts_external_token_[A-Za-z0-9_]+)\s*=\s*(\d+)`)
		for _, x := range enumRE.FindAllSubmatch(em[1], -1) {
			nameToID[string(x[1])] = intv(string(x[2]))
		}
	}
	out := map[int][]bool{}
	for _, sm := range stateRE.FindAllSubmatch(m[1], -1) {
		state := intv(string(sm[1]))
		bits := make([]bool, 0)
		for _, bm := range bitRE.FindAllSubmatch(sm[2], -1) {
			i, parseErr := strconv.Atoi(string(bm[1]))
			if parseErr != nil {
				var ok bool
				i, ok = nameToID[string(bm[1])]
				if !ok {
					continue
				}
			}
			if i >= len(bits) {
				bits = append(bits, make([]bool, i-len(bits)+1)...)
			}
			bits[i] = true
		}
		out[state] = bits
	}
	return out, nil
}

func splitList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ";")
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
