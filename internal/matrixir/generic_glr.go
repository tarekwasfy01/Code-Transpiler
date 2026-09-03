package matrixir

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

const (
	genericErrorState = 0
	// Match Tree-sitter's ordinary stack-version bound. Retaining more than
	// this changes pruning/condensation semantics and can leave stale branches
	// alive until all versions are exhausted.
	genericMaxVersionCount         = 6
	genericErrorCostPerRecovery    = 500
	genericErrorCostPerMissingTree = 110
	genericErrorCostPerSkipTree    = 100
	genericErrorCostPerSkipLine    = 30
	genericErrorCostPerSkipChar    = 1
)

type glrEntry struct {
	State int
	Node  *ParseNode
	Extra bool
	// Prev keeps the persistent stack-DAG edge. Entries remain materialized in
	// order for cheap tracing, while forks/merges share their predecessor
	// nodes exactly like Tree-sitter's StackNode links.
	Prev  int
	Preds []int
}

type glrStatus uint8

const (
	glrActive glrStatus = iota
	glrPaused
	glrHalted
)

type glrVersion struct {
	ID                   int
	Entries              []glrEntry
	Pos                  int
	Look                 *LexToken
	Status               glrStatus
	ErrorCost            int
	NodeCount            int
	NodeCountAtLastError int
	DynamicPrecedence    int
	ExternalLexState     int
	ExternalScannerState []byte
	ExternalScanner      externalScannerRuntime
	LastActionState      int
	NullLookahead        bool
	EOFShifted           bool
	LastExternalPosition int
	LastExternalSymbol   int
	// ERROR_REPEAT guard: do not insert the same zero-width missing token
	// repeatedly while the real lookahead and parser position are unchanged.
	LastMissingState     int
	LastMissingPosition  int
	LastMissingLookahead int
	// ZeroProgressSteps bounds nullable-reduction cycles. A valid reduction
	// chain eventually changes the stack or consumes input; retaining this
	// counter across outer rounds prevents a 256-step local budget from being
	// reset indefinitely on grammars with an ambiguous nullable path.
	ZeroProgressSteps int
}

// closeGLRVersion releases the ABI scanner payload owned by a discarded GLR
// branch. External scanners are C allocations; letting old branches become
// unreachable without calling destroy() causes the corpus runner's memory to
// grow even when the parser itself is bounded to six live versions.
func closeGLRVersion(v *glrVersion) {
	if v != nil && v.ExternalScanner != nil {
		v.ExternalScanner.Close()
		v.ExternalScanner = nil
	}
}

func (v *glrVersion) state() int {
	if len(v.Entries) == 0 {
		return 1
	}
	return v.Entries[len(v.Entries)-1].State
}

func (v *glrVersion) stackStates() []int {
	out := make([]int, len(v.Entries))
	for i := range v.Entries {
		out[i] = v.Entries[i].State
	}
	return out
}

func cloneParseNode(n *ParseNode) *ParseNode {
	// Parse nodes are immutable after they are pushed; sharing them between
	// versions is equivalent to Tree-sitter's retained Subtree links.
	return n
}

func cloneGLRVersion(v *glrVersion, id int) *glrVersion {
	out := *v
	out.ID = id
	out.Entries = append([]glrEntry(nil), v.Entries...)
	out.ExternalScannerState = append([]byte(nil), v.ExternalScannerState...)
	// The shallow copy above must never retain ownership of the source
	// scanner.  If cloning fails the branch remains scanner-less and will use
	// the table-driven internal lexer; aliasing the source payload here would
	// make two GLR versions destroy the same C object.
	out.ExternalScanner = nil
	// Scanner payloads are independent per GLR branch. The serialized bytes
	// are restored into the copied payload before the branch resumes.
	if v.ExternalScanner != nil {
		if scanner, err := v.ExternalScanner.Clone(out.ExternalScannerState); err == nil {
			out.ExternalScanner = scanner
		}
	}
	for i := range out.Entries {
		out.Entries[i].Node = cloneParseNode(out.Entries[i].Node)
	}
	if v.Look != nil {
		x := *v.Look
		out.Look = &x
	}
	return &out
}

// dagPopCount counts grammar nodes across predecessor links, excluding extra
// tokens. It is used by reductions and remains correct when a version shares
// a suffix with another fork.
func dagPopCount(entries []glrEntry, top int, count int) (int, []*ParseNode, bool) {
	cur := top
	var nodes []*ParseNode
	for count > 0 && cur >= 0 && cur < len(entries) {
		entry := entries[cur]
		if !entry.Extra {
			if entry.Node != nil {
				nodes = append(nodes, entry.Node)
			}
			count--
		}
		// Merged stack entries retain predecessor links in Preds.  Prefer the
		// canonical persistent edge, but recover through a merged edge when a
		// branch was condensed and its original Prev was removed.
		next := entry.Prev
		if next < 0 && len(entry.Preds) > 0 {
			next = entry.Preds[0]
		}
		cur = next
	}
	return cur, nodes, count == 0
}

func loadCachedRealTables(tableDir, language string) (RealTables, error) {
	key := tableDir + "|" + language
	tableCacheMu.Lock()
	defer tableCacheMu.Unlock()
	if loaded, ok := tableCache[key]; ok {
		return loaded, nil
	}
	loaded, err := LoadExecutionReadyTables(tableDir, language)
	if err != nil {
		return RealTables{}, err
	}
	tableCache[key] = loaded
	return loaded, nil
}

func traceGLR(e *GenericLexerLREngine, t RealTables, v *glrVersion, tok *LexToken, actionList int, action *ParseAction, name string) {
	if e == nil || !e.TraceEnabled {
		return
	}
	state := v.state()
	if action != nil && action.Kind == "REDUCE" && v.LastActionState != 0 {
		state = v.LastActionState
	}
	mode := lexModeForState(t, state)
	step := MachineTraceStep{
		ParserState: state, LexState: mode.LexState, ActionList: actionList,
		ActionIndex: -1, StackVersion: v.ID, ProductionID: -1,
		Stack: v.stackStates(), Action: name,
	}
	if tok != nil {
		step.Lookahead, step.Symbol = tok.SymbolID, tok.SymbolID
	}
	if action != nil {
		step.ActionIndex = action.Ordinal
		step.ProductionID = action.ProductionID
		step.DynamicPrecedence = action.DynamicPrecedence
		// For a reduction the machine event's symbol is the reduced LHS;
		// lookahead remains the triggering terminal. Recording the lookahead in
		// both fields made the first reduction appear divergent from the oracle.
		if action.Kind == "REDUCE" {
			step.Symbol = action.LhsSymbolID
		}
		if name == "" {
			step.Action = action.Kind
		}
		// The internal Tree-sitter trace reports the destination for SHIFT.
		// Normalize the generic event to the same machine boundary; REDUCE and
		// GOTO continue to report the state at which their dispatch occurred.
		if action.Kind == "SHIFT" || action.Kind == "SHIFT_REPEAT" || action.Kind == "SHIFT_EXTRA" {
			step.ParserState = action.TargetState
		}
	}
	e.MachineTrace = append(e.MachineTrace, step)
}

func reduceGLRVersion(v *glrVersion, a ParseAction, p *GenericParserEngine, t RealTables, language string) error {
	if _, _, ok := dagPopCount(v.Entries, len(v.Entries)-1, a.ChildCount); !ok {
		return fmt.Errorf("parse: reduce stack underflow state=%d production=%d child_count=%d", v.state(), a.ProductionID, a.ChildCount)
	}
	remaining := a.ChildCount
	start := len(v.Entries)
	for start > 1 && remaining > 0 {
		start--
		if !v.Entries[start].Extra {
			remaining--
		}
	}
	if remaining != 0 {
		return fmt.Errorf("parse: reduce stack underflow state=%d production=%d child_count=%d", v.state(), a.ProductionID, a.ChildCount)
	}

	popped := append([]glrEntry(nil), v.Entries[start:]...)
	v.Entries = v.Entries[:start]

	// Tree-sitter excludes only trailing extras from the new parent and pushes
	// those extras back after the parent, preserving their parse state.
	trailing := len(popped)
	for trailing > 0 && popped[trailing-1].Extra {
		trailing--
	}
	parentEntries := popped[:trailing]
	trailingExtras := popped[trailing:]
	children := make([]*ParseNode, 0, len(parentEntries))
	for _, entry := range parentEntries {
		if entry.Node != nil {
			children = append(children, entry.Node)
		}
	}
	// Apply generated alias sequences at reduction time.  Alias metadata comes
	// from the execution-ready parser tables; no source-text or language
	// heuristic is involved.  Hidden anonymous children remain available to the
	// machine, while aliased symbols expose the generated semantic identity.
	for _, alias := range t.Aliases[a.ProductionID] {
		if alias.ChildIndex >= 0 && alias.ChildIndex < len(children) && alias.AliasSymbolID != 0 {
			children[alias.ChildIndex] = cloneParseNode(children[alias.ChildIndex])
			children[alias.ChildIndex].SymbolID = alias.AliasSymbolID
		}
	}

	baseState := v.state()
	g, ok := p.Dispatch(baseState, a.LhsSymbolID)
	if !ok || g.DispatchKind != "STATE" {
		return fmt.Errorf("parse: missing goto state=%d lhs=%d", baseState, a.LhsSymbolID)
	}

	n := &ParseNode{
		SymbolID: a.LhsSymbolID, ProductionID: a.ProductionID,
		ParserState: baseState, DynamicPrecedence: a.DynamicPrecedence,
		Children: children, Fields: map[int][]int{},
	}
	if len(children) > 0 {
		n.Start = children[0].Start
		n.End = children[len(children)-1].End
	} else {
		n.Start, n.End = v.Pos, v.Pos
	}
	fieldMap := t.FieldMapByProduction[a.ProductionID]
	if fieldMap == nil {
		// Keep compatibility with callers that construct RealTables manually.
		fieldMap = t.FieldMap
	}
	for _, fm := range fieldMap {
		if fm.Language == language && fm.ChildIndex >= 0 && fm.ChildIndex < len(children) {
			n.Fields[fm.FieldID] = append(n.Fields[fm.FieldID], children[fm.ChildIndex].SymbolID)
		}
	}

	prev := -1
	if len(v.Entries) > 0 {
		prev = len(v.Entries) - 1
	}
	v.Entries = append(v.Entries, glrEntry{State: g.NextState, Node: n, Prev: prev})
	for _, extra := range trailingExtras {
		extra.State = g.NextState
		extra.Prev = len(v.Entries) - 1
		v.Entries = append(v.Entries, extra)
	}
	v.DynamicPrecedence += a.DynamicPrecedence
	v.NodeCount++
	return nil
}

func shiftGLRVersion(v *glrVersion, a ParseAction, tok LexToken) {
	extra := a.Kind == "SHIFT_EXTRA"
	nextState := a.TargetState
	// SHIFT_EXTRA is consumed without changing the grammar state. The
	// generated target value is not a parser destination for extra tokens.
	if extra {
		nextState = v.state()
	}
	n := &ParseNode{SymbolID: tok.SymbolID, Start: tok.Start, End: tok.End, Extra: extra, Fields: map[int][]int{}}
	prev := -1
	if len(v.Entries) > 0 {
		prev = len(v.Entries) - 1
	}
	v.Entries = append(v.Entries, glrEntry{State: nextState, Node: n, Extra: extra, Prev: prev})
	v.Pos = tok.End
	if tok.SymbolID == 0 {
		// EOF is a zero-width lexer result, not a persistent stack token.  After
		// a recovery/shift involving EOF, restart lexing so reductions and the
		// eventual ACCEPT are dispatched from the new state. Retaining the same
		// EOF lookahead replays the shift action and exhausts otherwise valid
		// GLR versions (notably Nim's EOF recovery paths).
		v.Look = nil
		v.EOFShifted = true
	} else {
		v.Look = nil
		v.EOFShifted = false
	}
	if tok.End > tok.Start {
		v.ZeroProgressSteps = 0
	} else {
		// SHIFT_EXTRA and nullable tokens can be zero-width. They must not
		// reset the no-progress guard, otherwise a grammar cycle can append
		// stack entries forever at one source offset.
		v.ZeroProgressSteps++
		if v.ZeroProgressSteps > 512 {
			v.Status = glrPaused
		}
	}
	v.LastMissingState, v.LastMissingPosition, v.LastMissingLookahead = 0, -1, 0
	v.NodeCount++
}

func glrVersionStatus(v *glrVersion) (cost, nodeCount, precedence int, inError bool) {
	cost = v.ErrorCost
	inError = v.Status == glrPaused || v.state() == genericErrorState
	if v.Status == glrPaused {
		cost += genericErrorCostPerRecovery + genericErrorCostPerSkipTree
	}
	nodeCount = v.NodeCount - v.NodeCountAtLastError
	if nodeCount < 0 {
		nodeCount = 0
	}
	return cost, nodeCount, v.DynamicPrecedence, inError
}

// compareGLRVersions mirrors ts_parser__compare_versions. Negative means a is
// preferred; positive means b is preferred; zero means neither dominates.
func compareGLRVersions(a, b *glrVersion) int {
	ac, an, ap, ae := glrVersionStatus(a)
	bc, bn, bp, be := glrVersionStatus(b)
	if !ae && be {
		if ac < bc {
			return -2
		}
		return -1
	}
	if ae && !be {
		if bc < ac {
			return 2
		}
		return 1
	}
	if ac < bc {
		if (bc-ac)*(1+an) > 18*genericErrorCostPerSkipTree {
			return -2
		}
		return -1
	}
	if bc < ac {
		if (ac-bc)*(1+bn) > 18*genericErrorCostPerSkipTree {
			return 2
		}
		return 1
	}
	if ap > bp {
		return -1
	}
	if bp > ap {
		return 1
	}
	return 0
}

func sameLinearStack(a, b *glrVersion) bool {
	if a.Pos != b.Pos || a.ErrorCost != b.ErrorCost || a.ExternalLexState != b.ExternalLexState || len(a.Entries) != len(b.Entries) {
		return false
	}
	for i := range a.Entries {
		if a.Entries[i].State != b.Entries[i].State || a.Entries[i].Extra != b.Entries[i].Extra {
			return false
		}
		as, bs := 0, 0
		if a.Entries[i].Node != nil {
			as = a.Entries[i].Node.SymbolID
		}
		if b.Entries[i].Node != nil {
			bs = b.Entries[i].Node.SymbolID
		}
		if as != bs {
			return false
		}
	}
	return true
}

// mergeCompatibleGLRVersions implements the safe part of ts_stack_merge for
// the materialized DAG representation. Versions may share a suffix only when
// their machine position, external scanner state and complete state path are
// identical; semantic nodes are immutable and can then be shared directly.
func mergeCompatibleGLRVersions(left, right *glrVersion) bool {
	if left.Status != glrActive || right.Status != glrActive {
		return false
	}
	if left.Pos != right.Pos || left.ExternalLexState != right.ExternalLexState || string(left.ExternalScannerState) != string(right.ExternalScannerState) || len(left.Entries) != len(right.Entries) {
		return false
	}
	for i := range left.Entries {
		if left.Entries[i].State != right.Entries[i].State || left.Entries[i].Extra != right.Entries[i].Extra {
			return false
		}
	}
	for i := range left.Entries {
		if right.Entries[i].Prev != left.Entries[i].Prev {
			left.Entries[i].Preds = appendUniqueInt(left.Entries[i].Preds, right.Entries[i].Prev)
		}
		for _, p := range right.Entries[i].Preds {
			left.Entries[i].Preds = appendUniqueInt(left.Entries[i].Preds, p)
		}
	}
	// Keep the lower-cost path and preserve the highest dynamic precedence on a
	// tie, matching Tree-sitter's subtree-link merge selection.
	if right.ErrorCost < left.ErrorCost || (right.ErrorCost == left.ErrorCost && right.DynamicPrecedence > left.DynamicPrecedence) {
		oldScanner := left.ExternalScanner
		*left = *right
		// Ownership transfers to the merged head; the caller may safely dispose
		// the right hand branch without destroying the scanner now in use.
		right.ExternalScanner = nil
		if oldScanner != nil {
			oldScanner.Close()
		}
	}
	return true
}

func appendUniqueInt(values []int, value int) []int {
	if value < 0 {
		return values
	}
	for _, v := range values {
		if v == value {
			return values
		}
	}
	return append(values, value)
}

func condenseGLRVersions(in []*glrVersion) []*glrVersion {
	out := make([]*glrVersion, 0, len(in))
	for _, v := range in {
		if v.Status != glrHalted || v.ErrorCost > 0 {
			out = append(out, v)
		} else {
			closeGLRVersion(v)
		}
	}
	// Once any active continuation exists, paused versions are discarded. They
	// are only resumed when the active set is empty, matching condense_stack.
	hasActive := false
	for _, v := range out {
		if v.Status == glrActive {
			hasActive = true
			break
		}
	}
	if hasActive {
		active := out[:0]
		for _, v := range out {
			if v.Status == glrActive {
				active = append(active, v)
			} else {
				closeGLRVersion(v)
			}
		}
		out = active
	} else if len(out) > 1 {
		// With no active continuation Tree-sitter resumes only the best paused
		// version and discards the remaining paused heads before recovery.
		best := 0
		for i := 1; i < len(out); i++ {
			if compareGLRVersions(out[best], out[i]) > 0 {
				best = i
			}
		}
		for i, v := range out {
			if i != best {
				closeGLRVersion(v)
			}
		}
		out = []*glrVersion{out[best]}
	}
	// Apply Tree-sitter's pairwise dominance before the hard six-version cap.
	// The previous implementation only merged byte-identical linear stacks;
	// clearly worse branches therefore displaced viable branches at the cap and
	// later surfaced as "all GLR versions exhausted".
	for i := 0; i < len(out); i++ {
		removedI := false
		for j := 0; j < i; j++ {
			cmp := compareGLRVersions(out[j], out[i])
			if cmp == -2 {
				closeGLRVersion(out[i])
				out = append(out[:i], out[i+1:]...)
				i--
				removedI = true
				break
			}
			if cmp == 2 {
				closeGLRVersion(out[j])
				out = append(out[:j], out[j+1:]...)
				i--
				j--
				continue
			}
		}
		if removedI {
			continue
		}
	}
	// Exact linear equivalents can be merged without losing a future pop path.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); {
			if mergeCompatibleGLRVersions(out[i], out[j]) {
				closeGLRVersion(out[j])
				out = append(out[:j], out[j+1:]...)
				continue
			}
			j++
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return compareGLRVersions(out[i], out[j]) < 0 })
	if len(out) > genericMaxVersionCount {
		for _, v := range out[genericMaxVersionCount:] {
			closeGLRVersion(v)
		}
		out = out[:genericMaxVersionCount]
	}
	return out
}

// recoverGLR performs the two language-neutral recovery strategies used by
// Tree-sitter: recover to a previous stack state that accepts the lookahead,
// otherwise skip the lookahead under ERROR_STATE. It intentionally contains
// no language or case identifiers.
func recoverGLR(v *glrVersion, t RealTables) bool {
	if v.Look == nil {
		return false
	}
	tok := *v.Look
	for i := len(v.Entries) - 2; i >= 0; i-- {
		state := v.Entries[i].State
		if state == genericErrorState {
			continue
		}
		if hasParseAction(t, state, tok.SymbolID) {
			removed := len(v.Entries) - (i + 1)
			v.ErrorCost += removed * genericErrorCostPerSkipTree
			v.Entries = v.Entries[:i+1]
			v.Status = glrActive
			return true
		}
	}
	// Tree-sitter may insert a zero-width missing terminal before any
	// lookahead, not only EOF. Use the generated ACTION table only; no token
	// names or language heuristics are involved. The real lookahead is retained
	// so the next machine step can process it normally.
	for _, d := range parseRowsForState(t, v.state()) {
		if d.DispatchKind != "ACTION" || d.ActionListID < 0 {
			continue
		}
		if v.LastMissingState == v.state() && v.LastMissingPosition == v.Pos && v.LastMissingLookahead == tok.SymbolID {
			// The previous candidate made no input progress. Tree-sitter's
			// ERROR_REPEAT handling skips this candidate and proceeds to the
			// ordinary error-token recovery path.
			break
		}
		for _, a := range t.ActionsByList[d.ActionListID] {
			if a.Kind != "SHIFT" && a.Kind != "SHIFT_EXTRA" {
				continue
			}
			n := &ParseNode{SymbolID: d.SymbolID, Start: v.Pos, End: v.Pos, Missing: true, Fields: map[int][]int{}}
			prev := -1
			if len(v.Entries) > 0 {
				prev = len(v.Entries) - 1
			}
			next := a.TargetState
			if a.Kind == "SHIFT_EXTRA" {
				next = v.state()
			}
			v.Entries = append(v.Entries, glrEntry{State: next, Node: n, Prev: prev})
			v.ErrorCost += genericErrorCostPerMissingTree
			v.NodeCount++
			v.ZeroProgressSteps++
			v.Look = &tok
			v.LastMissingState = v.state()
			v.LastMissingPosition = v.Pos
			v.LastMissingLookahead = tok.SymbolID
			v.Status = glrActive
			return true
		}
	}
	width := tok.End - v.Pos
	if width < 0 {
		width = 0
	}
	// Tree-sitter charges skipped bytes and skipped source lines separately.
	// Counting only bytes over-penalized multiline lookahead and changed the
	// recovery winner across otherwise equivalent GLR branches.
	lineCost := strings.Count(tok.Text, "\n") * genericErrorCostPerSkipLine
	v.ErrorCost += genericErrorCostPerRecovery + genericErrorCostPerSkipTree + width*genericErrorCostPerSkipChar + lineCost
	err := &ParseNode{SymbolID: -1, Start: v.Pos, End: tok.End, Fields: map[int][]int{}}
	prev := -1
	if len(v.Entries) > 0 {
		prev = len(v.Entries) - 1
	}
	v.Entries = append(v.Entries, glrEntry{State: genericErrorState, Node: err, Prev: prev})
	v.NodeCountAtLastError = v.NodeCount
	v.Pos = tok.End
	v.Look = nil
	v.Status = glrActive
	return true
}

func bestAccepted(accepted []*glrVersion) *glrVersion {
	if len(accepted) == 0 {
		return nil
	}
	best := accepted[0]
	for _, v := range accepted[1:] {
		if compareGLRVersions(best, v) > 0 {
			best = v
		}
	}
	return best
}

// recoverableRoot mirrors Tree-sitter's contract that a parse tree is still
// returned after error recovery.  A branch that reached ERROR_STATE or
// inserted a missing token is a successful recovered parse, even when no
// ACCEPT action remains after recovery pruning.  Branches that merely stopped
// at an unknown action (without an error node/cost) are still hard failures.
func recoverableRoot(versions []*glrVersion) *ParseNode {
	var best *glrVersion
	for _, v := range versions {
		if v == nil || v.ErrorCost == 0 {
			continue
		}
		if best == nil || compareGLRVersions(v, best) < 0 {
			best = v
		}
	}
	if best == nil {
		return nil
	}
	for i := len(best.Entries) - 1; i >= 1; i-- {
		if n := best.Entries[i].Node; n != nil && !n.Extra {
			return n
		}
	}
	return nil
}

func latestRecoverableNode(v *glrVersion) *ParseNode {
	if v == nil {
		return nil
	}
	for i := len(v.Entries) - 1; i >= 1; i-- {
		if n := v.Entries[i].Node; n != nil && !n.Extra {
			return n
		}
	}
	return nil
}

// parseRealNodesGLR is the full-parse table machine. It ports the runtime
// semantics that matter when old_tree == nil: raw states, parser-state-driven
// lexing, action-list branching, REDUCE forks, SHIFT/SHIFT_EXTRA behavior,
// extra-aware pops, version ranking and language-neutral recovery.
func (e *GenericLexerLREngine) parseRealNodesGLR(source, tableDir string) (*ParseNode, ParseStats, bool, error) {
	return e.parseRealNodesGLRContext(context.Background(), source, tableDir)
}

func (e *GenericLexerLREngine) parseRealNodesGLRContext(ctx context.Context, source, tableDir string) (*ParseNode, ParseStats, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	t, err := loadCachedRealTables(tableDir, e.Language)
	if err != nil {
		return nil, ParseStats{}, false, err
	}
	initial := t.InitialState
	if initial == 0 {
		initial = 1
	}
	parser := NewGenericParserEngine(e.Language)
	parser.AttachTables(t)
	lexer := NewGenericLexerEngine(e.Language)
	var rootScanner externalScannerRuntime
	if t.ExternalScanners != nil {
		if scanner, scanErr := newCGOExternalScanner(e.Language); scanErr == nil {
			rootScanner = scanner
		}
	}
	versions := []*glrVersion{{ID: 0, Entries: []glrEntry{{State: initial}}, ExternalScanner: rootScanner}}
	nextVersionID := 1
	accepted := []*glrVersion{}
	var recoveredRoot *ParseNode
	defer func() {
		for _, v := range versions {
			closeGLRVersion(v)
		}
		for _, v := range accepted {
			closeGLRVersion(v)
		}
	}()
	var st ParseStats
	maxRounds := len(source)*8 + 512
	if maxRounds < 512 {
		maxRounds = 512
	}
	if maxRounds > 10000 {
		maxRounds = 10000
	}
	// A nullable grammar cycle can grow the materialized stack without
	// consuming source bytes. Bound the retained machine state relative to the
	// input, then let the normal condense/recovery path classify the version.
	maxStackEntries := len(source)*64 + 4096
	if maxStackEntries < 4096 {
		maxStackEntries = 4096
	}

	for round := 0; round < maxRounds && len(versions) > 0; round++ {
		if err := ctx.Err(); err != nil {
			return nil, st, false, err
		}
		snapshot := append([]*glrVersion(nil), versions...)
		spawned := []*glrVersion{}
		madeProgress := false
		for _, v := range snapshot {
			if err := ctx.Err(); err != nil {
				return nil, st, false, err
			}
			if v.Status != glrActive {
				continue
			}
			localBudget := 256
			for local := 0; local < localBudget && v.Status == glrActive; local++ {
				if err := ctx.Err(); err != nil {
					return nil, st, false, err
				}
				if len(v.Entries) > maxStackEntries {
					v.Status = glrPaused
					break
				}
				state := v.state()
				st.LastParserState = state
				mode := lexModeForState(t, state)
				st.LastLexState = mode.LexState
				v.ExternalLexState = mode.ExternalLexState
				if v.Look == nil {
					if v.ExternalScanner != nil && mode.ExternalLexState != 0 {
						valid := make([]bool, 512)
						if bits, ok := t.ExternalValid[mode.ExternalLexState]; ok {
							if len(bits) > len(valid) {
								valid = make([]bool, len(bits))
							}
							copy(valid, bits)
						} else {
							for i, name := range t.ExternalScanners.ResultSymbols {
								for id, meta := range t.Symbols {
									// valid_symbols is indexed by the scanner's external
									// token enum. The parser maps that index to a grammar
									// symbol only after scan() accepts it.
									if i < len(valid) && meta.Kind == "terminal" && externalSymbolNameMatch(meta, name) && hasParseAction(t, state, id) {
										valid[i] = true
									}
								}
							}
						}
						var ext ExternalScanResult
						var scanErr error
						ext, scanErr = v.ExternalScanner.Scan(source, v.Pos, valid)
						// A scanner result is an external-token *index*.  Tree-sitter
						// discards results that are not enabled by the current
						// valid-symbol bitset; accepting one here would reinterpret an
						// arbitrary index as a grammar symbol and can create zero-width
						// GLR cycles.  Keep the check generic and table-driven.
						if ext.Accepted && (ext.AcceptedSymbol < 0 || ext.AcceptedSymbol >= len(valid) || !valid[ext.AcceptedSymbol]) {
							ext.Accepted = false
						}
						if scanErr == nil && ext.Accepted && ext.EndOffset >= v.Pos {
							if ext.EndOffset == v.Pos && v.LastExternalPosition == v.Pos && v.LastExternalSymbol == ext.AcceptedSymbol && string(v.ExternalScannerState) == string(ext.Serialized) {
								// A zero-width external token must advance scanner state;
								// identical repeats would otherwise spin forever.
								// Do not turn the repeat into external token index 0: index 0
								// is a valid token for some scanners (notably R START).
								ext.Accepted = false
							} else {
								v.LastExternalPosition, v.LastExternalSymbol = v.Pos, ext.AcceptedSymbol
							}
						}
						if ext.Accepted && ext.EndOffset >= v.Pos {
							sym := externalGrammarSymbol(t, ext.AcceptedSymbol)
							tok := LexToken{SymbolID: sym, Start: v.Pos, End: ext.EndOffset, Text: source[v.Pos:ext.EndOffset]}
							v.Look = &tok
							v.ExternalScannerState = append(v.ExternalScannerState[:0], ext.Serialized...)
							traceGLR(e, t, v, &tok, -1, nil, "LEX_EXTERNAL")
						}
					}
					if v.Look != nil {
						// External scanner produced the lookahead; skip the internal lexer.
						st.LastSymbolID = v.Look.SymbolID
					} else {
						tok, ok := lexer.NextToken(t, state, v.Pos, source)
						if !ok {
							v.Status = glrPaused
							traceGLR(e, t, v, nil, -1, nil, "LEX_ERROR")
							break
						}
						v.Look = &tok
						v.NullLookahead = mode.LexState == -1
						st.LastSymbolID = tok.SymbolID
						traceGLR(e, t, v, &tok, -1, nil, "LEX")
					}
				}

				d, ok := parser.Dispatch(state, v.Look.SymbolID)
				if !ok || d.DispatchKind == "STATE" {
					// Generated keyword lexers first return the keyword capture
					// symbol. After a reduction Tree-sitter retries that same token
					// as an identifier when the new state no longer accepts the
					// keyword. Reuse the existing token span; do not re-lex source.
					if !ok && t.IdentifierSymbol != 0 && v.Look.SymbolID != t.IdentifierSymbol &&
						hasParseAction(t, state, t.IdentifierSymbol) {
						v.Look.SymbolID = t.IdentifierSymbol
						d, ok = parser.Dispatch(state, t.IdentifierSymbol)
					}
				}
				if !ok || d.DispatchKind == "STATE" {
					v.Status = glrPaused
					traceGLR(e, t, v, v.Look, -1, nil, "NO_ACTION")
					break
				}
				acts := parser.ActionList(d.ActionListID)
				v.LastActionState = state
				st.LastActionListID = d.ActionListID
				if len(acts) == 0 {
					v.Status = glrPaused
					traceGLR(e, t, v, v.Look, d.ActionListID, nil, "EMPTY_ACTION_LIST")
					break
				}
				// A singleton REDUCE is deterministic. Tree-sitter continues on
				// the current stack version in this case; cloning the complete
				// stack and external-scanner payload for every nullable reduction
				// created artificial GLR work and was the source of apparent
				// timeouts. Only genuinely competing actions fork below.
				if len(acts) == 1 && acts[0].Kind == "REDUCE" {
					a := acts[0]
					v.LastActionState = state
					if err := reduceGLRVersion(v, a, parser, t, e.Language); err != nil {
						v.Status = glrPaused
						break
					}
					if v.NullLookahead {
						v.Look = nil
						v.NullLookahead = false
					}
					st.Reduce++
					st.Goto++
					traceGLR(e, t, v, v.Look, d.ActionListID, &a, "REDUCE")
					madeProgress = true
					continue
				}

				reductions := []*glrVersion{}
				var shift *ParseAction
				didAccept, didRecover := false, false
				for i := range acts {
					a := acts[i]
					switch a.Kind {
					case "REDUCE":
						branch := cloneGLRVersion(v, nextVersionID)
						nextVersionID++
						if err := reduceGLRVersion(branch, a, parser, t, e.Language); err == nil {
							// A -1 lex mode denotes the end of an extra/nonterminal
							// token. Tree-sitter uses a NULL lookahead and restarts
							// lexing after the reduction rather than retaining EOF.
							if branch.NullLookahead {
								branch.Look = nil
								branch.NullLookahead = false
							}
							reductions = append(reductions, branch)
							st.Reduce++
							st.Goto++
							traceGLR(e, t, branch, branch.Look, d.ActionListID, &a, "REDUCE")
							// A reduction is logged against the version that selected
							// the action. The cloned branch receives a fresh allocator
							// id internally, but that id is not a new runtime version
							// for the machine trace.
							if e.TraceEnabled && len(e.MachineTrace) > 0 {
								e.MachineTrace[len(e.MachineTrace)-1].StackVersion = v.ID
							}
						}
					case "SHIFT":
						if shift == nil {
							x := a
							shift = &x
						}
					case "SHIFT_EXTRA":
						if shift == nil {
							x := a
							shift = &x
						}
					case "SHIFT_REPEAT":
						// Runtime parser ignores repetition shifts during ordinary advance.
						traceGLR(e, t, v, v.Look, d.ActionListID, &a, "SHIFT_REPEAT_SKIP")
					case "ACCEPT_INPUT", "ACCEPT":
						didAccept = true
						traceGLR(e, t, v, v.Look, d.ActionListID, &a, "ACCEPT")
					case "RECOVER":
						didRecover = true
					}
				}

				if len(reductions) > 0 && (shift != nil || len(reductions) > 1) {
					st.Forks += len(reductions)
				}
				if shift != nil {
					spawned = append(spawned, reductions...)
					a := *shift
					traceGLR(e, t, v, v.Look, d.ActionListID, &a, a.Kind)
					shiftGLRVersion(v, a, *v.Look)
					st.Shift++
					madeProgress = true
					break
				}
				if didAccept {
					accepted = append(accepted, cloneGLRVersion(v, nextVersionID))
					nextVersionID++
					v.Status = glrHalted
					st.Accept++
					madeProgress = true
					break
				}
				if didRecover {
					traceGLR(e, t, v, v.Look, d.ActionListID, nil, "RECOVER")
					if !recoverGLR(v, t) {
						v.Status = glrHalted
					} else {
						if n := latestRecoverableNode(v); n != nil {
							recoveredRoot = n
						}
						madeProgress = true
					}
					break
				}
				if len(reductions) > 0 {
					// Runtime renumbers the last reduction version into the current
					// version and continues with the same lookahead.
					if len(reductions) > 1 {
						spawned = append(spawned, reductions[:len(reductions)-1]...)
					}
					last := reductions[len(reductions)-1]
					id := v.ID
					oldScanner := v.ExternalScanner
					zeroProgress := v.ZeroProgressSteps + 1
					*v = *last
					v.ID = id
					v.ZeroProgressSteps = zeroProgress
					// The reduction branch becomes the current version. Transfer its
					// scanner ownership and release the previous branch payload exactly
					// once; otherwise every reduction leaks one C scanner instance.
					last.ExternalScanner = nil
					if oldScanner != nil && oldScanner != v.ExternalScanner {
						oldScanner.Close()
					}
					if zeroProgress > 512 {
						// A nullable REDUCE cycle cannot make the real lookahead
						// progress. Enter the normal recovery path instead of
						// allocating versions until the case timeout fires.
						v.Status = glrPaused
						break
					}
					continue
				}
				v.Status = glrPaused
				break
			}
		}

		versions = append(versions, spawned...)
		versions = condenseGLRVersions(versions)
		active := 0
		for _, v := range versions {
			if v.Status == glrActive {
				active++
			}
		}
		if active == 0 && len(versions) > 0 {
			// As in ts_parser__condense_stack, resume the best paused version and
			// begin generic recovery only when no unpaused version remains.
			best := versions[0]
			if best.Status == glrPaused {
				best.Status = glrActive
				if !recoverGLR(best, t) {
					best.Status = glrHalted
				} else {
					if n := latestRecoverableNode(best); n != nil {
						recoveredRoot = n
					}
					madeProgress = true
				}
			}
			for _, v := range versions[1:] {
				if v.Status == glrPaused {
					v.Status = glrHalted
				}
			}
			versions = condenseGLRVersions(versions)
		}
		if b := bestAccepted(accepted); b != nil {
			bestInProgressCost := int(^uint(0) >> 1)
			for _, v := range versions {
				c, _, _, _ := glrVersionStatus(v)
				if c < bestInProgressCost {
					bestInProgressCost = c
				}
			}
			bc, _, _, _ := glrVersionStatus(b)
			if len(versions) == 0 || bc < bestInProgressCost || (!madeProgress && bc <= bestInProgressCost) {
				for i := len(b.Entries) - 1; i >= 1; i-- {
					if b.Entries[i].Node != nil && !b.Entries[i].Extra {
						return b.Entries[i].Node, st, true, nil
					}
				}
				return nil, st, true, nil
			}
		}
		if !madeProgress && len(spawned) == 0 && len(accepted) == 0 {
			allHalted := true
			for _, v := range versions {
				if v.Status != glrHalted {
					allHalted = false
					break
				}
			}
			if allHalted {
				break
			}
		}
	}
	if b := bestAccepted(accepted); b != nil {
		for i := len(b.Entries) - 1; i >= 1; i-- {
			if b.Entries[i].Node != nil && !b.Entries[i].Extra {
				return b.Entries[i].Node, st, true, nil
			}
		}
		return nil, st, true, nil
	}
	if n := recoverableRoot(versions); n != nil {
		return n, st, true, nil
	}
	if recoveredRoot != nil {
		return recoveredRoot, st, true, nil
	}
	// Tree-sitter still returns an ERROR-bearing tree when every recovery
	// version is exhausted at end-of-input. Preserve that contract at the
	// machine boundary, but retain a hard failure for a non-EOF machine error
	// so missing table data cannot be silently classified as a parse success.
	if st.LastSymbolID == 0 {
		return &ParseNode{SymbolID: -1, Start: 0, End: len(source), Fields: map[int][]int{}}, st, true, nil
	}
	return nil, st, false, fmt.Errorf("parse: all GLR versions exhausted state=%d lex_state=%d symbol=%d action_list=%d production=%d", st.LastParserState, st.LastLexState, st.LastSymbolID, st.LastActionListID, st.LastProductionID)
}

func externalSymbolNameMatch(meta SymbolMetadata, external string) bool {
	if meta.Name == external || meta.DisplayName == external {
		return true
	}
	norm := func(s string) string {
		s = strings.ToLower(strings.TrimSpace(s))
		s = strings.TrimPrefix(s, "sym__")
		s = strings.TrimPrefix(s, "anon_sym_")
		s = strings.ReplaceAll(s, "external", "")
		s = strings.ReplaceAll(s, "parenthesis", "paren")
		s = strings.ReplaceAll(s, "_", "")
		return s
	}
	return norm(meta.Name) == norm(external) || norm(meta.DisplayName) == norm(external)
}

func externalGrammarSymbol(t RealTables, index int) int {
	if t.ExternalScanners == nil || index < 0 || index >= len(t.ExternalScanners.ResultSymbols) {
		return index
	}
	name := t.ExternalScanners.ResultSymbols[index]
	// External scanner result symbols are grammar terminals.  Some grammars
	// expose a same-named nonterminal (for example R's close_bracket nodes),
	// so never resolve the result through map iteration order.
	for id, meta := range t.Symbols {
		if meta.Kind == "terminal" && externalSymbolNameMatch(meta, name) {
			return id
		}
	}
	for id, meta := range t.Symbols {
		if externalSymbolNameMatch(meta, name) {
			return id
		}
	}
	return index
}
