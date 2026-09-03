package matrixir

import (
	"regexp"
	"strconv"
	"strings"
)

// NormalizedMachineEvent is the common, source-independent trace row used by
// the differential runner. Unknown values are -1; they are compared as
// unknowns rather than being inferred from diagnostics.
type NormalizedMachineEvent struct {
	Event             string `json:"event"`
	ActionType        string `json:"action_type"`
	ParserState       int    `json:"parser_state"`
	LexState          int    `json:"lex_state"`
	Lookahead         int    `json:"lookahead"`
	Symbol            int    `json:"symbol"`
	ActionList        int    `json:"action_list"`
	ActionIndex       int    `json:"action_index"`
	StackVersion      int    `json:"stack_version"`
	ProductionID      int    `json:"production_id"`
	DynamicPrecedence int    `json:"dynamic_precedence"`
	StackStates       []int  `json:"stack_states,omitempty"`
}

// NormalizeGenericTrace converts the table engine trace into the shared
// machine-event format. The trace contains one row per parser-machine step;
// lexical lookahead and the selected action remain explicit fields.
func NormalizeGenericTrace(in []MachineTraceStep) []NormalizedMachineEvent {
	out := make([]NormalizedMachineEvent, 0, len(in))
	for _, s := range in {
		action := s.Action
		// LEX is an observation emitted by the generic GLR tracer. The
		// reference runtime's internal trace records the token as part of the
		// following machine action, so it is omitted from the comparable event
		// stream rather than being mistaken for a parser operation.
		if action == "LEX" {
			continue
		}
		// A paused GLR version is scheduler bookkeeping, not a parser-machine
		// operation. The reference runtime does not expose this marker, so it
		// must not create a false first divergence.
		if action == "NO_ACTION" || action == "EMPTY_ACTION_LIST" {
			continue
		}
		if action == "ACCEPT_INPUT" {
			action = "ACCEPT"
		}
		e := NormalizedMachineEvent{Event: "STEP", ActionType: action, ParserState: s.ParserState, LexState: s.LexState, Lookahead: s.Lookahead, Symbol: s.Symbol, ActionList: s.ActionList, ActionIndex: s.ActionIndex, StackVersion: s.StackVersion, ProductionID: s.ProductionID, DynamicPrecedence: s.DynamicPrecedence, StackStates: append([]int(nil), s.Stack...)}
		if action != "" {
			e.Event = action
		}
		out = append(out, e)
	}
	canonicalizeStackVersions(out)
	return out
}

// Stack version numbers are allocator-local in both machines. Preserve their
// equality/inequality pattern while removing the irrelevant numeric identity
// so the differential result reflects real fork/merge behavior.
func canonicalizeStackVersions(events []NormalizedMachineEvent) {
	ids := map[int]int{}
	next := 0
	for i := range events {
		if events[i].StackVersion < 0 {
			continue
		}
		id := events[i].StackVersion
		if v, ok := ids[id]; ok {
			events[i].StackVersion = v
		} else {
			ids[id] = next
			events[i].StackVersion = next
			next++
		}
	}
}

var oracleKV = regexp.MustCompile(`([a-z_]+)[:=](-?[0-9]+)`)

// NormalizeOracleTrace consumes the temporary runtime's TRACE_INTERNAL and
// TRACE PARSE lines. It deliberately uses only machine fields present in the
// trace and never parses source text or diagnostics.
func NormalizeOracleTrace(text string) []NormalizedMachineEvent {
	var out []NormalizedMachineEvent
	parserState, lexState, stackVersion := -1, -1, -1
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "TRACE PARSE process") {
			m := kv(line)
			if v, ok := m["state"]; ok {
				parserState = v
			}
			if v, ok := m["version"]; ok {
				stackVersion = v
			}
		} else if strings.HasPrefix(line, "TRACE PARSE lex_internal") {
			m := kv(line)
			if v, ok := m["state"]; ok {
				lexState = v
			}
		} else if strings.HasPrefix(line, "TRACE PARSE shift") {
			// The public parser trace carries the action's source state; the
			// internal SHIFT line carries only its destination.  Use the source
			// state so it has the same meaning as the generic trace.
			out = append(out, NormalizedMachineEvent{Event: "SHIFT", ActionType: "SHIFT", ParserState: parserState, LexState: lexState, Lookahead: -1, Symbol: -1, ActionList: -1, ActionIndex: -1, StackVersion: stackVersion, ProductionID: -1, DynamicPrecedence: -1})
		} else if strings.HasPrefix(line, "TRACE_INTERNAL REDUCE") {
			m := kv(line)
			sym, prod, prec, ver := m["symbol"], m["production"], m["precedence"], m["version"]
			out = append(out, NormalizedMachineEvent{Event: "REDUCE", ActionType: "REDUCE", ParserState: parserState, LexState: lexState, Lookahead: -1, Symbol: sym, ActionList: -1, ActionIndex: -1, StackVersion: ver, ProductionID: prod, DynamicPrecedence: prec})
		} else if strings.HasPrefix(line, "TRACE_INTERNAL SHIFT") {
			// Internal runtime traces contain the authoritative shift ordering,
			// while parser state/lookahead are not exposed on this line. Unknown
			// numeric fields intentionally remain wildcards in comparison.
			m := kv(line)
			action := "SHIFT"
			if m["extra"] != 0 {
				action = "SHIFT_EXTRA"
			}
			// Internal SHIFT carries the authoritative destination state.  Use it
			// as parser_state so the oracle and generic traces describe the same
			// machine boundary.
			out = append(out, NormalizedMachineEvent{Event: action, ActionType: action, ParserState: m["state"], LexState: lexState, Lookahead: -1, Symbol: -1, ActionList: -1, ActionIndex: -1, StackVersion: m["version"], ProductionID: -1, DynamicPrecedence: -1})
		} else if strings.HasPrefix(line, "TRACE PARSE lexed_lookahead") {
			// Older oracle logs print a symbolic name ("package") rather than
			// the numeric symbol id.  Such a row cannot be compared to the
			// execution-ready table trace without guessing, so leave it out;
			// numeric lex rows are retained when present.
			m := kv(line)
			if _, ok := m["sym"]; ok {
				out = append(out, NormalizedMachineEvent{Event: "LEX", ActionType: "LEX", ParserState: -1, LexState: -1, Lookahead: m["sym"], Symbol: m["sym"], ActionList: -1, ActionIndex: -1, StackVersion: -1, ProductionID: -1, DynamicPrecedence: -1})
			}
		} else if strings.HasPrefix(line, "TRACE PARSE accept") {
			out = append(out, NormalizedMachineEvent{Event: "ACCEPT", ActionType: "ACCEPT", ParserState: parserState, LexState: lexState, Lookahead: -1, Symbol: -1, ActionList: -1, ActionIndex: -1, StackVersion: stackVersion, ProductionID: -1, DynamicPrecedence: -1})
		} else if strings.HasPrefix(line, "TRACE PARSE recover") || strings.HasPrefix(line, "TRACE_INTERNAL RECOVER") {
			m := kv(line)
			out = append(out, NormalizedMachineEvent{Event: "RECOVER", ActionType: "RECOVER", ParserState: parserState, LexState: lexState, Lookahead: m["lookahead"], Symbol: m["symbol"], ActionList: m["action_list"], ActionIndex: -1, StackVersion: stackVersion, ProductionID: -1, DynamicPrecedence: -1})
		}
	}
	canonicalizeStackVersions(out)
	return out
}

func kv(line string) map[string]int {
	out := map[string]int{}
	for _, m := range oracleKV.FindAllStringSubmatch(line, -1) {
		v, _ := strconv.Atoi(m[2])
		out[m[1]] = v
	}
	return out
}

// FirstMachineDivergence returns the first row whose normalized vectors differ.
func FirstMachineDivergence(reference, generic []NormalizedMachineEvent) (int, *NormalizedMachineEvent, *NormalizedMachineEvent) {
	n := len(reference)
	if len(generic) < n {
		n = len(generic)
	}
	for i := 0; i < n; i++ {
		if !sameMachineEvent(reference[i], generic[i]) {
			r, g := reference[i], generic[i]
			return i, &r, &g
		}
	}
	if len(reference) != len(generic) {
		// The temporary runtime runners do not print an explicit ACCEPT event;
		// successful generic parses do. Treat that terminal bookkeeping event as
		// equivalent when every observable preceding event matched.
		if len(generic) > len(reference) && n == len(reference) {
			allAccept := true
			for _, e := range generic[n:] {
				if e.Event != "ACCEPT" {
					allAccept = false
					break
				}
			}
			if allAccept {
				return -1, nil, nil
			}
		}
		i := n
		var r, g *NormalizedMachineEvent
		if i < len(reference) {
			x := reference[i]
			r = &x
		}
		if i < len(generic) {
			x := generic[i]
			g = &x
		}
		return i, r, g
	}
	return -1, nil, nil
}

// FirstMachineDivergenceStrict is used for conformance reports.  Unlike the
// historical compatibility comparator, unknown fields are never wildcards:
// an omitted parser/lexer state is a real divergence until the oracle and the
// generic trace both provide the same value.
func FirstMachineDivergenceStrict(reference, generic []NormalizedMachineEvent) (int, *NormalizedMachineEvent, *NormalizedMachineEvent) {
	n := len(reference)
	if len(generic) < n {
		n = len(generic)
	}
	for i := 0; i < n; i++ {
		if !sameMachineEventStrict(reference[i], generic[i]) {
			r, g := reference[i], generic[i]
			return i, &r, &g
		}
	}
	if len(reference) != len(generic) {
		i := n
		var r, g *NormalizedMachineEvent
		if i < len(reference) {
			x := reference[i]
			r = &x
		}
		if i < len(generic) {
			x := generic[i]
			g = &x
		}
		return i, r, g
	}
	return -1, nil, nil
}

func sameMachineEventStrict(a, b NormalizedMachineEvent) bool {
	if a.Event != b.Event || a.ActionType != b.ActionType || a.ParserState != b.ParserState || a.LexState != b.LexState || a.Lookahead != b.Lookahead || a.Symbol != b.Symbol || a.ActionList != b.ActionList || a.ActionIndex != b.ActionIndex || a.StackVersion != b.StackVersion || a.ProductionID != b.ProductionID || a.DynamicPrecedence != b.DynamicPrecedence {
		return false
	}
	if len(a.StackStates) != len(b.StackStates) {
		return false
	}
	for i := range a.StackStates {
		if a.StackStates[i] != b.StackStates[i] {
			return false
		}
	}
	return true
}

func sameMachineEvent(a, b NormalizedMachineEvent) bool {
	if a.Event != b.Event || a.ActionType != b.ActionType {
		return false
	}
	if !sameInt(a.ParserState, b.ParserState) || !sameInt(a.LexState, b.LexState) || !sameInt(a.Lookahead, b.Lookahead) || !sameInt(a.Symbol, b.Symbol) || !sameInt(a.ActionList, b.ActionList) || !sameInt(a.ActionIndex, b.ActionIndex) || !sameInt(a.StackVersion, b.StackVersion) || !sameInt(a.ProductionID, b.ProductionID) || !sameInt(a.DynamicPrecedence, b.DynamicPrecedence) {
		return false
	}
	if len(a.StackStates) == 0 || len(b.StackStates) == 0 {
		return true
	}
	if len(a.StackStates) != len(b.StackStates) {
		return false
	}
	for i := range a.StackStates {
		if !sameInt(a.StackStates[i], b.StackStates[i]) {
			return false
		}
	}
	return true
}

func sameInt(a, b int) bool { return a < 0 || b < 0 || a == b }
