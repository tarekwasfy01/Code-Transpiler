package matrixir

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"unicode/utf8"
)

var tableCacheMu sync.Mutex

// Tables are immutable after loading. Keep one partition per language so six
// corpus workers do not repeatedly rescan and sort the 268MB dispatch CSV.
var tableCache = map[string]RealTables{}

func atoi(s string) int { var v int; fmt.Sscanf(s, "%d", &v); return v }

// GenericLexerLREngine is the language-neutral execution facade for the
// offline table set. Tables are identified by language; semantic output is a
// CanonicalProgram and never a legacy AST.
type MachineTraceStep struct {
	ParserState, LexState, Lookahead, Symbol, ActionList int
	Action                                               string
	// ActionIndex and the reduction metadata are copied from the selected
	// table row so differential traces can be compared without diagnostics.
	ActionIndex, StackVersion, ProductionID, DynamicPrecedence int
	Stack                                                      []int
}
type GenericLexerLREngine struct {
	Language     string
	MachineTrace []MachineTraceStep
	// Trace collection is opt-in. Corpus execution only needs ParseStats and
	// ParseNode; copying the complete stack for every lexer/reduction event was
	// the dominant memory cost and made valid GLR cases look like timeouts.
	TraceEnabled bool
}

type GenericLexerEngine struct{ Language string }
type GenericParserEngine struct {
	Language string
	Tables   *RealTables
}
type GenericGLRConflictEngine struct{}
type GenericProducerEngine struct{}
type ParseStats struct {
	Shift, Reduce, Goto, Accept, Forks                            int
	LastParserState, LastLexState, LastSymbolID, LastActionListID int
	LastProductionID                                              int
}
type LexToken struct {
	SymbolID, Start, End int
	Text                 string
}

// ParseNode is the neutral result of a table reduction. It contains no
// language-specific AST and is suitable as input to the producer registry.
type ParseNode struct {
	SymbolID, ProductionID, Start, End int
	ParserState                        int
	DynamicPrecedence                  int
	Extra                              bool
	Missing                            bool
	Children                           []*ParseNode
	Fields                             map[int][]int
}

func NewGenericLexerEngine(language string) *GenericLexerEngine {
	return &GenericLexerEngine{Language: language}
}
func NewGenericParserEngine(language string) *GenericParserEngine {
	return &GenericParserEngine{Language: language}
}
func (e *GenericParserEngine) AttachTables(t RealTables) { e.Tables = &t }
func (e *GenericParserEngine) Dispatch(state, symbol int) (ParseDispatchEntry, bool) {
	if e.Tables == nil {
		return ParseDispatchEntry{}, false
	}
	if e.Tables.ParseIndex != nil {
		d, ok := e.Tables.ParseIndex[parseDispatchKey{state: state, symbol: symbol}]
		if ok {
			return d, true
		}
		// The indexed form is an acceleration structure, never a second
		// semantic source.  Fall back to the canonical sorted dispatch rows
		// when a partition was loaded without a complete index (this occurs
		// for supplemented Tree-sitter tables).  Keeping the lookup primitive
		// here makes all callers observe identical ACTION/GOTO semantics.
	}
	p := e.Tables.Parse
	i := sort.Search(len(p), func(i int) bool {
		return p[i].ParseState > state || (p[i].ParseState == state && p[i].SymbolID >= symbol)
	})
	if i < len(p) && p[i].ParseState == state && p[i].SymbolID == symbol {
		return p[i], true
	}
	// Tree-sitter's parse_table_map aliases parser states to their primary
	// table row.  The alias is used only for ACTION/GOTO lookup; SHIFT targets
	// remain the raw action values.  Some generated partitions retain only the
	// primary row in the exported dispatch CSV, so consult it when the expanded
	// row is absent.
	if e.Tables.PrimaryStates != nil {
		if primary, exists := e.Tables.PrimaryStates[state]; exists && primary != state {
			j := sort.Search(len(p), func(j int) bool {
				return p[j].ParseState > primary || (p[j].ParseState == primary && p[j].SymbolID >= symbol)
			})
			if j < len(p) && p[j].ParseState == primary && p[j].SymbolID == symbol {
				return p[j], true
			}
		}
	}
	return ParseDispatchEntry{}, false
}
func (e *GenericParserEngine) ActionList(id int) []ParseAction {
	out := []ParseAction{}
	if e.Tables == nil {
		return out
	}
	if e.Tables.ActionsByList != nil {
		if acts, ok := e.Tables.ActionsByList[id]; ok {
			return acts
		}
		// Supplemented partitions may have rows only in the flat action table;
		// use it as the canonical fallback instead of treating a missing cache
		// entry as an empty semantic action list.
	}
	p := e.Tables.Actions
	i := sort.Search(len(p), func(i int) bool { return p[i].ActionListID >= id })
	for ; i < len(p) && p[i].ActionListID == id; i++ {
		out = append(out, p[i])
	}
	return out
}
func chooseAction(acts []ParseAction) (ParseAction, bool) {
	// This helper is only for deterministic bootstrap callers. Tree-sitter does
	// not resolve an ambiguous action list here: REDUCE actions fork stack
	// versions and dynamic precedence participates later in version/tree choice.
	for _, a := range acts {
		if a.Kind == "SHIFT_REPEAT" {
			continue
		}
		if a.Kind != "RECOVER" && a.Kind != "ERROR" && a.Kind != "EMPTY" {
			return a, true
		}
	}
	return ParseAction{}, false
}

// ParseSymbols executes the canonical ACTION/GOTO machine for a token-symbol
// stream. It is independent of Canonicalize and is used by the bootstrap
// harness while the source lexer is being connected to symbol IDs.
func (e *GenericParserEngine) ParseSymbols(symbols []int) (ParseStats, bool) {
	var st ParseStats
	if e.Tables == nil {
		return st, false
	}
	initial := e.Tables.InitialState
	if initial == 0 && len(e.Tables.Parse) == 0 {
		initial = 1
	}
	stack := []int{initial}
	pos := 0
	for pos < len(symbols)+1 {
		sym := 0
		if pos < len(symbols) {
			sym = symbols[pos]
		}
		d, ok := e.Dispatch(stack[len(stack)-1], sym)
		if !ok {
			return st, false
		}
		if d.DispatchKind == "STATE" {
			stack = append(stack, d.NextState)
			st.Goto++
			continue
		}
		acts := e.ActionList(d.ActionListID)
		if len(acts) > 1 {
			st.Forks += len(acts) - 1
		}
		if len(acts) == 0 {
			return st, false
		}
		a, valid := chooseAction(acts)
		if !valid {
			return st, false
		}
		switch a.Kind {
		case "SHIFT", "SHIFT_REPEAT", "SHIFT_EXTRA":
			nextState := a.TargetState
			if a.Kind == "SHIFT_EXTRA" {
				nextState = stack[len(stack)-1]
			}
			stack = append(stack, nextState)
			st.Shift++
			pos++
		case "REDUCE":
			if a.ChildCount > len(stack)-1 {
				return st, false
			}
			stack = stack[:len(stack)-a.ChildCount]
			g, ok := e.Dispatch(stack[len(stack)-1], a.LhsSymbolID)
			if !ok || g.DispatchKind != "STATE" {
				return st, false
			}
			stack = append(stack, g.NextState)
			st.Reduce++
		case "ACCEPT_INPUT", "ACCEPT":
			st.Accept++
			return st, true
		case "RECOVER":
			return st, false
		default:
			return st, false
		}
	}
	return st, false
}
func NewGenericProducerEngine() *GenericProducerEngine       { return &GenericProducerEngine{} }
func NewGenericGLRConflictEngine() *GenericGLRConflictEngine { return &GenericGLRConflictEngine{} }

func (e *GenericLexerEngine) Lex(source string) []Lexeme { return Tokenize(e.Language, source) }
func lexModeForState(t RealTables, parseState int) LexModeEntry {
	if t.ModeByState != nil {
		if m, ok := t.ModeByState[parseState]; ok {
			return m
		}
		return LexModeEntry{ParseState: parseState}
	}
	for _, m := range t.Modes {
		if m.ParseState == parseState {
			return m
		}
	}
	return LexModeEntry{ParseState: parseState}
}

func lexRows(t RealTables, function string, state int) []LexDispatchEntry {
	p := t.Lex
	i := sort.Search(len(p), func(i int) bool {
		return p[i].LexerFunction > function || (p[i].LexerFunction == function && p[i].LexState >= state)
	})
	j := i
	for j < len(p) && p[j].LexerFunction == function && p[j].LexState == state {
		j++
	}
	return p[i:j]
}

// parseRowsForState returns only dispatch entries for one parser state.  The
// execution-ready table is sorted by (state,symbol); recovery used to scan
// the complete multi-million-row table for every missing-token candidate,
// turning a short parse into an apparent timeout and retaining large
// temporary slices.  Keep the operation table-driven while indexing the
// already sorted rows with two binary searches.
func parseRowsForState(t RealTables, state int) []ParseDispatchEntry {
	if t.StateRows != nil {
		return t.StateRows[state]
	}
	rows := t.Parse
	lo := sort.Search(len(rows), func(i int) bool { return rows[i].ParseState >= state })
	if lo == len(rows) || rows[lo].ParseState != state {
		return nil
	}
	hi := sort.Search(len(rows), func(i int) bool { return rows[i].ParseState > state })
	return rows[lo:hi]
}

func hasParseAction(t RealTables, state, symbol int) bool {
	if t.ParseIndex != nil {
		d, ok := t.ParseIndex[parseDispatchKey{state: state, symbol: symbol}]
		if !ok {
			// ParseIndex is an optimization and may omit rows supplied by a
			// parser.c supplement. Resolve against the canonical dispatch rows.
			rows := parseRowsForState(t, state)
			for _, row := range rows {
				if row.SymbolID == symbol {
					d, ok = row, true
					break
				}
			}
		}
		if !ok {
			return false
		}
		if d.DispatchKind == "STATE" {
			return d.NextState != 0
		}
		if d.ActionListID < 0 {
			return false
		}
		acts := t.ActionsByList[d.ActionListID]
		if len(acts) == 0 {
			acts = t.Actions
		}
		for _, a := range acts {
			if a.ActionListID != d.ActionListID {
				continue
			}
			if a.Kind != "EMPTY" {
				return true
			}
		}
		return false
	}
	p := t.Parse
	i := sort.Search(len(p), func(i int) bool {
		return p[i].ParseState > state || (p[i].ParseState == state && p[i].SymbolID >= symbol)
	})
	if i >= len(p) || p[i].ParseState != state || p[i].SymbolID != symbol {
		return false
	}
	if p[i].DispatchKind == "STATE" {
		return p[i].NextState != 0
	}
	if p[i].ActionListID < 0 {
		return false
	}
	for _, a := range t.Actions {
		if a.ActionListID < p[i].ActionListID {
			continue
		}
		if a.ActionListID > p[i].ActionListID {
			break
		}
		if a.Kind != "EMPTY" {
			return true
		}
	}
	return false
}

func isReservedWord(t RealTables, parseState, symbol int) bool {
	mode := lexModeForState(t, parseState)
	return mode.ReservedWordSetID != 0 && t.ReservedWords[mode.ReservedWordSetID] != nil && t.ReservedWords[mode.ReservedWordSetID][symbol]
}

// runLexerFunction reproduces the generated START_LEXER/ADVANCE/SKIP/
// ACCEPT_TOKEN machine from parser.c. Acceptance marks the current end
// position; SKIP advances token_start but does not terminate the lexer.
func runLexerFunction(t RealTables, function string, initialState, start int, source string) (symbol, tokenStart, end int, ok bool) {
	state, p := initialState, start
	tokenStart = start
	lastSym, lastEnd, accepted := 0, start, false
	visitedEOF := map[int]bool{}
	maxSteps := (len(source)-start+1)*8 + 256
	for steps := 0; steps < maxSteps; steps++ {
		eof := p >= len(source)
		// The generated lexer can expose the builtin end token in its initial
		// state.  It is only an acceptance after input has been consumed (or at
		// EOF); accepting it at the token start would return a zero-width EOF for
		// every ordinary token and make the parser replay the same lookahead.
		if s, yes := t.AcceptsByFunction[function+":"+fmt.Sprint(state)]; yes && (eof || (s != 0 && p > tokenStart)) {
			lastSym, lastEnd, accepted = s, p, true
		}
		var r rune
		size := 0
		if !eof {
			r, size = utf8.DecodeRuneInString(source[p:])
			if r == utf8.RuneError && size == 0 {
				size = 1
			}
		}
		matched := false
		for _, x := range lexRows(t, function, state) {
			match := false
			if x.PredicateID != "" {
				match = EvalLexPredicate(t, x.PredicateID, r, eof)
			}
			if x.PredicateID == "" && x.MapCodepoint != 0 {
				match = !eof && int(r) == x.MapCodepoint
			}
			if x.PredicateID == "" && x.MapCodepoint == 0 {
				match = true
			}
			if !match {
				continue
			}
			matched = true
			state = x.NextState
			if eof {
				// ADVANCE at EOF changes lexer state but cannot consume input.
				// Generated lexers use this to enter the EOF accept state.
				if visitedEOF[state] {
					matched = false
					break
				}
				visitedEOF[state] = true
			} else {
				p += size
				if x.Skip {
					tokenStart = p
					accepted = false
					lastSym, lastEnd = 0, p
				}
			}
			break
		}
		if !matched {
			break
		}
	}
	if accepted {
		return lastSym, tokenStart, lastEnd, true
	}
	return 0, tokenStart, start, false
}

// NextToken executes Tree-sitter's internal lexer first and invokes the
// generated keyword lexer only when the main lexer returned the grammar's
// keyword-capture token. A keyword is promoted only when it is valid in the
// current parse state or belongs to that state's reserved-word set.
func (e *GenericLexerEngine) NextToken(t RealTables, parseState, pos int, source string) (LexToken, bool) {
	mode := lexModeForState(t, parseState)
	if mode.LexState == 65535 || mode.LexState == -1 {
		return LexToken{SymbolID: 0, Start: pos, End: pos}, true
	}
	sym, tokenStart, end, ok := runLexerFunction(t, "ts_lex", mode.LexState, pos, source)
	if !ok {
		// Tree-sitter's lexer enters ERROR_STATE and consumes one code point
		// instead of producing a null token. Keep this generic: the error symbol
		// comes from symbol metadata and no source-language rule is consulted.
		if pos >= len(source) {
			return LexToken{SymbolID: 0, Start: pos, End: pos}, true
		}
		_, size := utf8.DecodeRuneInString(source[pos:])
		if size <= 0 {
			size = 1
		}
		errSym := t.ErrorSymbol
		if errSym == 0 {
			errSym = -1
		}
		return LexToken{SymbolID: errSym, Start: pos, End: pos + size, Text: source[pos : pos+size]}, true
	}
	if t.HasKeywordLexer && t.KeywordCaptureSymbol != 0 && sym == t.KeywordCaptureSymbol && end >= tokenStart {
		segment := source[tokenStart:end]
		kwSym, _, kwEnd, kwOK := runLexerFunction(t, "ts_lex_keywords", 0, 0, segment)
		if kwOK && kwEnd == len(segment) && kwSym != 0 && (hasParseAction(t, parseState, kwSym) || isReservedWord(t, parseState, kwSym)) {
			sym = kwSym
		}
	}
	return LexToken{SymbolID: sym, Start: tokenStart, End: end, Text: source[tokenStart:end]}, true
}

func (e *GenericLexerEngine) NextSymbol(t RealTables, parseState, pos int, source string) (symbol, next int, ok bool) {
	tok, ok := e.NextToken(t, parseState, pos, source)
	if !ok {
		return 0, pos, false
	}
	return tok.SymbolID, tok.End, true
}

func EvalLexPredicate(t RealTables, id string, r rune, eof bool) bool {
	s := []int{}
	pop := func() int {
		if len(s) == 0 {
			return 0
		}
		v := s[len(s)-1]
		s = s[:len(s)-1]
		return v
	}
	for _, p := range t.Predicates[id] {
		switch p.Opcode {
		case "PUSH_EOF":
			if eof {
				s = append(s, 1)
			} else {
				s = append(s, 0)
			}
		case "PUSH_INT":
			s = append(s, atoi(p.Arg1))
		case "PUSH_LOOKAHEAD":
			s = append(s, int(r))
		case "CMP_LE":
			b, a := pop(), pop()
			if a <= b {
				s = append(s, 1)
			} else {
				s = append(s, 0)
			}
		case "CMP_GE":
			b, a := pop(), pop()
			if a >= b {
				s = append(s, 1)
			} else {
				s = append(s, 0)
			}
		case "CMP_EQ":
			b, a := pop(), pop()
			if a == b {
				s = append(s, 1)
			} else {
				s = append(s, 0)
			}
		case "CMP_NE":
			b, a := pop(), pop()
			if a != b {
				s = append(s, 1)
			} else {
				s = append(s, 0)
			}
		case "CMP_LT":
			b, a := pop(), pop()
			if a < b {
				s = append(s, 1)
			} else {
				s = append(s, 0)
			}
		case "CMP_GT":
			b, a := pop(), pop()
			if a > b {
				s = append(s, 1)
			} else {
				s = append(s, 0)
			}
		case "AND":
			b, a := pop(), pop()
			if a != 0 && b != 0 {
				s = append(s, 1)
			} else {
				s = append(s, 0)
			}
		case "OR":
			b, a := pop(), pop()
			if a != 0 || b != 0 {
				s = append(s, 1)
			} else {
				s = append(s, 0)
			}
		case "NOT":
			a := pop()
			if a == 0 {
				s = append(s, 1)
			} else {
				s = append(s, 0)
			}
		case "SET", "SET_CONTAINS":
			ok := false
			for _, c := range t.CharacterSets[p.Arg1] {
				if int(r) >= c.Start && int(r) <= c.End {
					ok = true
					break
				}
			}
			if ok {
				s = append(s, 1)
			} else {
				s = append(s, 0)
			}
		case "RANGE":
			a, b := atoi(p.Arg1), atoi(p.Arg2)
			if int(r) >= a && int(r) <= b {
				s = append(s, 1)
			} else {
				s = append(s, 0)
			}
		}
	}
	return len(s) > 0 && s[len(s)-1] != 0
}
func (e *GenericParserEngine) Parse(source string) (CanonicalProgram, error) {
	return Canonicalize(e.Language, source)
}
func (e *GenericGLRConflictEngine) Resolve(events []CanonicalSemanticEvent) []CanonicalSemanticEvent {
	return events
}
func (e *GenericProducerEngine) Produce(program CanonicalProgram) []CanonicalSemanticEvent {
	return program.SemanticEvents
}

func NewGenericLexerLREngine(language string) *GenericLexerLREngine {
	return &GenericLexerLREngine{Language: language}
}

// Parse executes the shared lexical/semantic pipeline. The existing parser is
// used only as the bootstrap implementation until generated ACTION/GOTO
// tables are promoted; callers already receive the canonical event contract.
func (e *GenericLexerLREngine) Parse(source string) (CanonicalProgram, error) {
	p, err := NewGenericParserEngine(e.Language).Parse(source)
	if err != nil {
		return CanonicalProgram{}, err
	}
	p.SemanticEvents = NewGenericGLRConflictEngine().Resolve(NewGenericProducerEngine().Produce(p))
	return p, nil
}

func (e *GenericLexerLREngine) Trace(source string) (CanonicalProgram, []LegacyTraceRow, error) {
	return TraceCanonicalize(e.Language, source)
}

// ParseReal executes the table-backed lexer/parser path. It intentionally
// returns execution statistics and does not construct semantic events.
func (e *GenericLexerLREngine) ParseReal(source, tableDir string) (ParseStats, bool, error) {
	_, st, ok, err := e.parseRealNodesContext(context.Background(), source, tableDir)
	return st, ok, err
}

// ParseRealContext executes the real-table parser with cooperative
// cancellation.  Callers that impose a per-input deadline can therefore stop
// a pathological GLR branch without leaving a parser goroutine behind.
func (e *GenericLexerLREngine) ParseRealContext(ctx context.Context, source, tableDir string) (ParseStats, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	e.TraceEnabled = false
	_, st, ok, err := e.parseRealNodesContext(ctx, source, tableDir)
	return st, ok, err
}

// ParseRealNodes executes the same real-table path and returns the neutral
// reduction tree. No legacy parser or source re-tokenizer is involved.
func (e *GenericLexerLREngine) ParseRealNodes(source, tableDir string) (*ParseNode, ParseStats, bool, error) {
	e.MachineTrace = nil
	e.TraceEnabled = false
	return e.parseRealNodesContext(context.Background(), source, tableDir)
}

// ParseRealNodesContext is the cancellable neutral ParseNode entry point.
func (e *GenericLexerLREngine) ParseRealNodesContext(ctx context.Context, source, tableDir string) (*ParseNode, ParseStats, bool, error) {
	e.MachineTrace = nil
	e.TraceEnabled = false
	if ctx == nil {
		ctx = context.Background()
	}
	return e.parseRealNodesContext(ctx, source, tableDir)
}

func (e *GenericLexerLREngine) ParseRealTrace(source, tableDir string) ([]MachineTraceStep, ParseStats, bool, error) {
	e.MachineTrace = nil
	e.TraceEnabled = true
	defer func() { e.TraceEnabled = false }()
	_, st, ok, err := e.parseRealNodesContext(context.Background(), source, tableDir)
	return append([]MachineTraceStep(nil), e.MachineTrace...), st, ok, err
}

func (e *GenericLexerLREngine) parseRealNodes(source, tableDir string) (*ParseNode, ParseStats, bool, error) {
	return e.parseRealNodesContext(context.Background(), source, tableDir)
}

func (e *GenericLexerLREngine) parseRealNodesContext(ctx context.Context, source, tableDir string) (*ParseNode, ParseStats, bool, error) {
	return e.parseRealNodesGLRContext(ctx, source, tableDir)
}

// parseRealNodesLinear is retained only as a bootstrap regression oracle. The
// production real-table path above uses the GLR runtime semantics.
func (e *GenericLexerLREngine) parseRealNodesLinear(source, tableDir string) (*ParseNode, ParseStats, bool, error) {
	t, loadErr := loadCachedRealTables(tableDir, e.Language)
	if loadErr != nil {
		return nil, ParseStats{}, false, loadErr
	}
	// The lexer is driven by the current parser state; no eager token list or
	// language-specific tokenizer is involved.
	var st ParseStats
	initial := t.InitialState
	if initial == 0 && len(t.Parse) == 0 {
		initial = 1
	}
	states := []int{initial}
	nodes := []*ParseNode{}
	pos := 0
	var look *LexToken
	lex := NewGenericLexerEngine(e.Language)
	p := NewGenericParserEngine(e.Language)
	p.AttachTables(t)
	maxSteps := len(source)*2000 + 20000
	if maxSteps < 20000 {
		maxSteps = 20000
	}
	for steps := 0; steps < maxSteps; steps++ {
		state := states[len(states)-1]
		lexState := 0
		for _, m := range t.Modes {
			if m.ParseState == state {
				lexState = m.LexState
				break
			}
		}
		e.MachineTrace = append(e.MachineTrace, MachineTraceStep{ParserState: state, LexState: lexState, ActionIndex: -1, StackVersion: 0, ProductionID: -1, DynamicPrecedence: 0, Stack: append([]int(nil), states...)})
		st.LastParserState = state
		if look == nil {
			sym, end, yes := lex.NextSymbol(t, state, pos, source)
			if !yes {
				return nil, st, false, fmt.Errorf("lex: no transition at parser state %d byte %d", state, pos)
			}
			look = &LexToken{SymbolID: sym, Start: pos, End: end, Text: source[pos:end]}
			st.LastSymbolID = sym
			e.MachineTrace[len(e.MachineTrace)-1].Lookahead = sym
			e.MachineTrace[len(e.MachineTrace)-1].Symbol = sym
		}
		d, yes := p.Dispatch(state, look.SymbolID)
		if !yes {
			return nil, st, false, fmt.Errorf("parse: no dispatch state=%d symbol=%d", state, look.SymbolID)
		}
		if d.DispatchKind == "STATE" {
			e.MachineTrace[len(e.MachineTrace)-1].Action = "GOTO"
			states = append(states, d.NextState)
			st.Goto++
			continue
		}
		acts := p.ActionList(d.ActionListID)
		st.LastActionListID = d.ActionListID
		e.MachineTrace[len(e.MachineTrace)-1].ActionList = d.ActionListID
		if len(acts) == 0 {
			return nil, st, false, fmt.Errorf("parse: empty action list %d", d.ActionListID)
		}
		if len(acts) > 1 {
			st.Forks += len(acts) - 1
		}
		a, valid := chooseAction(acts)
		if !valid {
			return nil, st, false, fmt.Errorf("parse: recovery-only action list %d", d.ActionListID)
		}
		e.MachineTrace[len(e.MachineTrace)-1].ActionIndex = a.Ordinal
		e.MachineTrace[len(e.MachineTrace)-1].ProductionID = a.ProductionID
		e.MachineTrace[len(e.MachineTrace)-1].DynamicPrecedence = a.DynamicPrecedence
		switch a.Kind {
		case "SHIFT", "SHIFT_REPEAT", "SHIFT_EXTRA":
			e.MachineTrace[len(e.MachineTrace)-1].Action = a.Kind
			nextState := a.TargetState
			if a.Kind == "SHIFT_EXTRA" {
				nextState = states[len(states)-1]
			}
			states = append(states, nextState)
			nodes = append(nodes, &ParseNode{SymbolID: look.SymbolID, Start: look.Start, End: look.End, Fields: map[int][]int{}})
			st.Shift++
			st.LastParserState = nextState
			pos = look.End
			look = nil
		case "REDUCE":
			e.MachineTrace[len(e.MachineTrace)-1].Action = a.Kind
			st.LastProductionID = a.ProductionID
			pop := a.ChildCount
			// A reduce may only consume states that are actually present.  The
			// previous implementation clamped this value, which silently changed
			// the grammar stack and made the following GOTO non-deterministic.
			if pop > len(states)-1 {
				return nil, st, false, fmt.Errorf("parse: reduce stack underflow state=%d production=%d child_count=%d", state, a.ProductionID, a.ChildCount)
			}
			var children []*ParseNode
			if pop > 0 {
				if pop > len(nodes) {
					pop = len(nodes)
				}
				children = append(children, nodes[len(nodes)-pop:]...)
				nodes = nodes[:len(nodes)-pop]
			}
			states = states[:len(states)-pop]
			g, good := p.Dispatch(states[len(states)-1], a.LhsSymbolID)
			if !good || g.DispatchKind != "STATE" {
				return nil, st, false, fmt.Errorf("parse: missing goto state=%d lhs=%d", states[len(states)-1], a.LhsSymbolID)
			}
			states = append(states, g.NextState)
			st.Goto++
			n := &ParseNode{SymbolID: a.LhsSymbolID, ProductionID: a.ProductionID, ParserState: state, DynamicPrecedence: a.DynamicPrecedence, Children: children, Fields: map[int][]int{}}
			if len(children) > 0 {
				n.Start = children[0].Start
				n.End = children[len(children)-1].End
			} else {
				n.Start = pos
				n.End = pos
			}
			for _, fm := range t.FieldMap {
				if fm.Language == e.Language && fm.ProductionID == a.ProductionID && fm.ChildIndex >= 0 && fm.ChildIndex < len(children) {
					n.Fields[fm.FieldID] = append(n.Fields[fm.FieldID], children[fm.ChildIndex].SymbolID)
				}
			}
			nodes = append(nodes, n)
			st.Reduce++
		case "ACCEPT_INPUT", "ACCEPT":
			e.MachineTrace[len(e.MachineTrace)-1].Action = a.Kind
			st.Accept++
			if len(nodes) > 0 {
				return nodes[len(nodes)-1], st, true, nil
			}
			return nil, st, true, nil
		default:
			return nil, st, false, fmt.Errorf("parse: unsupported action %q state=%d symbol=%d", a.Kind, state, look.SymbolID)
		}
	}
	return nil, st, false, fmt.Errorf("parse: step budget exceeded state=%d symbol=%d", st.LastParserState, st.LastSymbolID)
}
