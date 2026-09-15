// Copyright (c) 2026 Tarek Wasfy

package backend

// This file contains bounded, derived compilation planning.  It deliberately
// does not introduce a semantic IR: units and summaries only hold stable UAST
// node IDs and hashes, while lowering continues to use the canonical graph.

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type StreamingFunctionSummary struct {
	ID                int      `json:"id"`
	Name              string   `json:"name"`
	SemanticHash      string   `json:"semantic_hash"`
	CalledSymbols     []string `json:"called_symbols,omitempty"`
	ReferencedGlobals []int    `json:"referenced_globals,omitempty"`
	EstimatedOps      int      `json:"estimated_ops"`
}

type GlobalSemanticIndex struct {
	Functions []StreamingFunctionSummary `json:"functions"`
	Order     []int                      `json:"order"`
}

type CompilationUnit struct {
	ID                 int                 `json:"id"`
	FunctionIDs        []int               `json:"function_ids"`
	RequiredPrimitives []string            `json:"required_primitives,omitempty"`
	EstimatedOps       int                 `json:"estimated_ops"`
	Regions            []CompilationRegion `json:"regions,omitempty"`
}

// CompilationRegion is a conservative, derived boundary. It is only
// populated when a safe single-entry region is known; otherwise the complete
// function remains the unit to preserve control-flow and lifetime semantics.
type CompilationRegion struct {
	ID       int   `json:"id"`
	Entry    int   `json:"entry"`
	Exits    []int `json:"exits,omitempty"`
	LiveIns  []int `json:"live_ins,omitempty"`
	LiveOuts []int `json:"live_outs,omitempty"`
}

type EncodedRegion struct {
	Function    string `json:"function"`
	Start       int    `json:"start"`
	End         int    `json:"end"`
	SingleEntry bool   `json:"single_entry"`
}

// MachineFragment is already-target output plus link metadata.  It is not a
// semantic representation and therefore can be discarded after linking.
type MachineFragment struct {
	UnitID       int                  `json:"unit_id"`
	SemanticHash string               `json:"semantic_hash"`
	Text         []byte               `json:"text"`
	Symbols      map[string]uint32    `json:"symbols,omitempty"`
	Relocations  []FragmentRelocation `json:"relocations,omitempty"`
	Alignment    int                  `json:"alignment,omitempty"`
}

type FragmentRelocation struct {
	Offset uint32 `json:"offset"`
	Target string `json:"target"`
	Kind   string `json:"kind"`
	Addend int64  `json:"addend,omitempty"`
}

// Verified fixed stencils contain no holes or relocations.  They are used only
// for byte-exact terminal instructions; all operand-bearing operations retain
// the normal encoder and its validation.
var verifiedX64Stencils = map[string][]byte{
	"ret": {0xc3},
	"cqo": {0x48, 0x99},
	"ud2": {0x0f, 0x0b},
	"nop": {0x90},
}

func lookupVerifiedX64Stencil(op string, a, b x64Operand) ([]byte, bool) {
	if a.Kind != 0 || b.Kind != 0 {
		return nil, false
	}
	bts, ok := verifiedX64Stencils[op]
	if !ok {
		return nil, false
	}
	return bts, true
}

type StreamingPlan struct {
	Index     GlobalSemanticIndex `json:"index"`
	Units     []CompilationUnit   `json:"units"`
	Workers   int                 `json:"workers"`
	QueueSize int                 `json:"queue_size"`
}

// StreamingMetrics records bounded-planning timings without retaining traces.
type StreamingMetrics struct {
	SummaryMicros     int64 `json:"summary_us"`
	PreparationMicros int64 `json:"preparation_us"`
	LegalityMicros    int64 `json:"legality_us"`
	SelectionMicros   int64 `json:"selection_us"`
	EncodingMicros    int64 `json:"encoding_us"`
	LinkMicros        int64 `json:"link_us"`
	Units             int   `json:"units"`
	Functions         int   `json:"functions"`
	Fragments         int   `json:"fragments"`
	CacheHits         int   `json:"cache_hits"`
	CacheMisses       int   `json:"cache_misses"`
	StencilHits       int   `json:"stencil_hits"`
	StencilMisses     int   `json:"stencil_misses"`
	PipelineWorkers   int   `json:"pipeline_workers,omitempty"`
	PipelineQueueSize int   `json:"pipeline_queue_size,omitempty"`
	PipelineMicros    int64 `json:"pipeline_us,omitempty"`
	LoadQueuePeak     int   `json:"load_queue_peak,omitempty"`
	PrepareQueuePeak  int   `json:"prepare_queue_peak,omitempty"`
	FragmentQueuePeak int   `json:"fragment_queue_peak,omitempty"`
}

type nativeLoweringCacheRecord struct {
	Schema     string               `json:"schema"`
	Code       []byte               `json:"code"`
	Labels     map[string]int       `json:"labels"`
	Functions  []x64Function        `json:"functions"`
	Unresolved []FragmentRelocation `json:"unresolved,omitempty"`
	Imports    []pe64ImportSpec     `json:"imports,omitempty"`
}

const nativeLoweringBackendVersion = "native-lowering-v4-semantic-root-project-context"

func nativeLoweringCacheKey(u *UniversalASTDocument, opts CompileOptions) string {
	semanticDigest := opts.projectUnitSemanticRoot
	if semanticDigest == "" && u != nil {
		semanticDigest = u.SemanticDocumentSHA256
	}
	// Canonical imports already carry a digest of the compatibility Semantic
	// document. Reuse it rather than JSON-marshaling a potentially multi-million
	// node UAST just to discover that its lowering cache is reusable. Fall back
	// to the full canonical payload for documents without a verified digest.
	if len(semanticDigest) != 64 {
		semanticDigest = ""
		if u != nil {
			if b, err := json.Marshal(u); err == nil {
				semanticDigest = stableBytesHash(b)
			}
		}
	}
	b, _ := json.Marshal(struct {
		Version              string
		SemanticDigest       string
		Arch, OS, ABI, Entry string
		ProjectMode          bool
		ProjectUnit, Index   string
	}{nativeLoweringBackendVersion, semanticDigest, opts.TargetArch, opts.TargetOS, opts.ABI, opts.EntryPoint, opts.ProjectMode, opts.ProjectUnitID, opts.projectIndexFingerprint})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func loadNativeLoweringCache(dir, key string) (*nativeLoweringCacheRecord, bool) {
	if dir == "" {
		return nil, false
	}
	b, err := os.ReadFile(filepath.Join(dir, key+".json"))
	if err != nil {
		return nil, false
	}
	var r nativeLoweringCacheRecord
	if json.Unmarshal(b, &r) != nil || r.Schema != nativeLoweringBackendVersion || len(r.Code) == 0 {
		return nil, false
	}
	for _, off := range r.Labels {
		if off < 0 || off > len(r.Code) {
			return nil, false
		}
	}
	for _, fn := range r.Functions {
		start, okStart := r.Labels[fn.Label]
		end, okEnd := r.Labels[fn.End]
		if !okStart || !okEnd || start < 0 || end < start || end > len(r.Code) {
			return nil, false
		}
	}
	return &r, true
}

func saveNativeLoweringCache(dir, key string, r nativeLoweringCacheRecord) {
	if dir == "" {
		return
	}
	if os.MkdirAll(dir, 0755) != nil {
		return
	}
	b, err := json.Marshal(r)
	if err != nil {
		return
	}
	tmp := filepath.Join(dir, key+".tmp")
	if os.WriteFile(tmp, b, 0644) == nil {
		_ = os.Rename(tmp, filepath.Join(dir, key+".json"))
	}
}

// resolveFragmentRelocations validates the late-link contract.  Current x64
// encoding resolves local branches before fragment serialization, so most
// fragments have no records; imported/global references can use this same
// routine once emitted as relocations.
func resolveFragmentRelocations(fragments map[string]MachineFragment) error {
	symbols := map[string]uint32{}
	for _, f := range fragments {
		for name, off := range f.Symbols {
			if _, exists := symbols[name]; exists {
				return fmt.Errorf("duplicate fragment symbol %q", name)
			}
			symbols[name] = off
		}
	}
	for _, f := range fragments {
		for _, r := range f.Relocations {
			if _, ok := symbols[r.Target]; !ok {
				return fmt.Errorf("undefined fragment relocation target %q", r.Target)
			}
			if int(r.Offset) >= len(f.Text) {
				return fmt.Errorf("fragment relocation offset %d out of range", r.Offset)
			}
		}
	}
	return nil
}

func linkMachineFragments(fragments map[string]MachineFragment) ([]byte, error) {
	return linkMachineFragmentsPolicy(fragments, false)
}

// linkMachineFragmentsRelocatable reconstructs a unit's encoded byte stream
// while deliberately retaining references to symbols defined by other units.
// Known local relocations are applied immediately; unknown targets remain zero
// and are carried by MachineFragment.Relocations to the project linker.
func linkMachineFragmentsRelocatable(fragments map[string]MachineFragment) ([]byte, error) {
	return linkMachineFragmentsPolicy(fragments, true)
}

func linkMachineFragmentsPolicy(fragments map[string]MachineFragment, allowUnresolved bool) ([]byte, error) {
	if len(fragments) == 0 {
		return nil, fmt.Errorf("no machine fragments")
	}
	maxEnd := 0
	for _, f := range fragments {
		if f.UnitID < 0 {
			return nil, fmt.Errorf("negative fragment offset")
		}
		if e := f.UnitID + len(f.Text); e > maxEnd {
			maxEnd = e
		}
	}
	out := make([]byte, maxEnd)
	used := make([]bool, maxEnd)
	for _, f := range fragments {
		for i, b := range f.Text {
			at := f.UnitID + i
			if used[at] {
				return nil, fmt.Errorf("overlapping machine fragments at %d", at)
			}
			used[at] = true
			out[at] = b
		}
	}
	globalSymbols := map[string]uint32{}
	for _, f := range fragments {
		for name, off := range f.Symbols {
			if _, exists := globalSymbols[name]; exists {
				return nil, fmt.Errorf("duplicate fragment symbol %q", name)
			}
			globalSymbols[name] = uint32(f.UnitID) + off
		}
	}
	missingTargets := map[string]struct{}{}
	for _, f := range fragments {
		for _, r := range f.Relocations {
			if r.Kind != "relative_call" && r.Kind != "relative_branch" && r.Kind != "relative_data" {
				return nil, fmt.Errorf("unsupported fragment relocation kind %q", r.Kind)
			}
			target, ok := globalSymbols[r.Target]
			if !ok {
				if allowUnresolved {
					continue
				}
				missingTargets[r.Target] = struct{}{}
				continue
			}
			at := f.UnitID + int(r.Offset)
			if at < 0 || at+4 > len(out) {
				return nil, fmt.Errorf("fragment relocation offset out of range")
			}
			disp := int64(target) + r.Addend - int64(at+4)
			if disp < -2147483648 || disp > 2147483647 {
				return nil, fmt.Errorf("relative relocation overflow for %q", r.Target)
			}
			binary.LittleEndian.PutUint32(out[at:at+4], uint32(int32(disp)))
		}
	}
	if len(missingTargets) > 0 {
		labels := make([]string, 0, len(missingTargets))
		for label := range missingTargets {
			labels = append(labels, label)
		}
		sort.Strings(labels)
		return nil, fmt.Errorf("undefined fragment relocation targets (%d): %s", len(labels), strings.Join(labels, ", "))
	}
	return out, nil
}

func deriveX64Relocations(code []byte, labels map[string]int) []FragmentRelocation {
	byOffset := make(map[int]string, len(labels))
	for name, off := range labels {
		byOffset[off] = name
	}
	out := make([]FragmentRelocation, 0)
	for i := 0; i+4 < len(code); i++ {
		if code[i] != 0xe8 && code[i] != 0xe9 {
			continue
		}
		d := int32(binary.LittleEndian.Uint32(code[i+1 : i+5]))
		target := i + 5 + int(d)
		name, ok := byOffset[target]
		if !ok {
			continue
		}
		kind := "relative_call"
		if code[i] == 0xe9 {
			kind = "relative_branch"
		}
		out = append(out, FragmentRelocation{Offset: uint32(i + 1), Target: name, Kind: kind})
	}
	return out
}

func deriveEncodedRegions(p x64Program, labels map[string]int) []EncodedRegion {
	if len(p.Functions) == 0 {
		return nil
	}
	incoming := map[string]bool{}
	for _, in := range p.Instructions {
		switch in.Op {
		case "jmp", "call", "je", "jne", "jl", "jle", "jg", "jge", "jb", "jae":
			if in.A.Label != "" {
				incoming[in.A.Label] = true
			}
		}
	}
	out := make([]EncodedRegion, 0, len(p.Functions))
	for _, fn := range p.Functions {
		start, ok1 := labels[fn.Label]
		end, ok2 := labels[fn.End]
		if !ok1 || !ok2 || end < start {
			continue
		}
		// Internal labels without incoming control edges are safe linear
		// boundaries. Branch targets remain attached to the containing region.
		bounds := []int{start}
		for name, off := range labels {
			if off > start && off < end && !incoming[name] {
				bounds = append(bounds, off)
			}
		}
		sort.Ints(bounds)
		bounds = append(bounds, end)
		for i := 0; i+1 < len(bounds); i++ {
			if bounds[i] < bounds[i+1] {
				out = append(out, EncodedRegion{Function: fn.Label, Start: bounds[i], End: bounds[i+1], SingleEntry: true})
			}
		}
	}
	return out
}

func elapsedMicros(start time.Time) int64 { return time.Since(start).Microseconds() }

func stableNodeHash(g *uastExecutionGraph, id int) string {
	n := g.nodes[id]
	if n == nil {
		return ""
	}
	// Hash only stable summary facts.  Marshaling Fields/Attributes here would
	// duplicate potentially large semantic payloads for every function and
	// defeat bounded planning memory.
	b, _ := json.Marshal(struct {
		ID     int
		Kind   string
		Facets []string
	}{n.ID, n.StructuralKind, n.SemanticFacets})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func stableBytesHash(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func buildGlobalSemanticIndex(g *uastExecutionGraph) GlobalSemanticIndex {
	idx := GlobalSemanticIndex{}
	ids := make([]int, 0)
	for id, c := range g.common {
		if c.Kind == "function" {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	for _, id := range ids {
		c := g.common[id]
		s := StreamingFunctionSummary{ID: id, Name: c.Name, SemanticHash: stableNodeHash(g, id), EstimatedOps: 1}
		// Count only direct children.  This keeps the summary O(V+E) and avoids
		// repeatedly walking the merged graph during planning.
		for _, children := range g.children[id] {
			s.EstimatedOps += len(children)
			for _, child := range children {
				cc := g.common[child.ID]
				if cc.Kind == "function" || cc.Kind == "call" {
					if cc.Name != "" {
						s.CalledSymbols = append(s.CalledSymbols, cc.Name)
					}
				}
				if cc.Kind == "identifier" && cc.Name != "" {
					s.CalledSymbols = append(s.CalledSymbols, cc.Name)
				}
			}
		}
		sort.Strings(s.CalledSymbols)
		s.CalledSymbols = uniqueStrings(s.CalledSymbols)
		idx.Functions = append(idx.Functions, s)
		idx.Order = append(idx.Order, id)
	}
	return idx
}

func uniqueStrings(in []string) []string {
	if len(in) < 2 {
		return in
	}
	out := in[:1]
	for _, s := range in[1:] {
		if s != out[len(out)-1] {
			out = append(out, s)
		}
	}
	return out
}

func streamingWorkerBudget(functions int) (int, int) {
	if functions < 1 {
		return 1, 1
	}
	// A unit is intentionally small; the budget is conservative because the
	// selector still owns target-specific scratch.  It prevents CPU count from
	// becoming an implicit RAM multiplier.
	workers := runtime.NumCPU() * 80 / 100
	if workers < 32 {
		workers = 32
	}
	if workers > functions {
		workers = functions
	}
	queue := workers * 2
	return workers, queue
}

func partitionSemanticFunctions(idx GlobalSemanticIndex, maxOps, maxFunctions int) []CompilationUnit {
	if maxOps < 1 {
		maxOps = 256
	}
	if maxFunctions < 1 {
		maxFunctions = 8
	}
	units := make([]CompilationUnit, 0)
	cur := CompilationUnit{ID: 0}
	for _, s := range idx.Functions {
		if len(cur.FunctionIDs) > 0 && (cur.EstimatedOps+s.EstimatedOps > maxOps || len(cur.FunctionIDs) >= maxFunctions) {
			cur.ID = len(units)
			units = append(units, cur)
			cur = CompilationUnit{}
		}
		cur.FunctionIDs = append(cur.FunctionIDs, s.ID)
		cur.EstimatedOps += s.EstimatedOps
		cur.Regions = append(cur.Regions, regionizeFunction(s, maxOps)...)
	}
	if len(cur.FunctionIDs) > 0 {
		cur.ID = len(units)
		units = append(units, cur)
	}
	return units
}

func regionizeFunction(summary StreamingFunctionSummary, maxOps int) []CompilationRegion {
	if maxOps < 1 || summary.EstimatedOps <= maxOps {
		return nil
	}
	// No explicit CFG boundary is present in the summary yet. Returning one
	// whole-function region is the safe fallback and makes that decision
	// inspectable rather than silently pretending arbitrary node cuts are safe.
	return []CompilationRegion{{ID: 0, Entry: summary.ID}}
}

// planStreamingLowering is a cheap planning pass used by CompileMachine.  It
// intentionally performs no semantic rewrite and therefore cannot lose UAST
// facts.  The resulting plan is also deterministic for any worker count.
func planStreamingLowering(g *uastExecutionGraph) StreamingPlan {
	idx := buildGlobalSemanticIndex(g)
	w, q := streamingWorkerBudget(len(idx.Functions))
	return StreamingPlan{Index: idx, Units: partitionSemanticFunctions(idx, 256, 8), Workers: w, QueueSize: q}
}

// runBoundedUnits is used for local, discardable preparation.  Bounded queues,
// cancellation, and ordered results make the scheduler safe to reuse for
// fragment writers and cache lookups without making completion order semantic.
func runBoundedUnits(ctx context.Context, units []CompilationUnit, workers int, fn func(CompilationUnit) error) error {
	if workers < 1 {
		workers = 1
	}
	if workers > len(units) && len(units) > 0 {
		workers = len(units)
	}
	jobs := make(chan CompilationUnit, workers*2)
	errs := make(chan error, 1)
	var wg sync.WaitGroup
	worker := func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case u, ok := <-jobs:
				if !ok {
					return
				}
				if err := fn(u); err != nil {
					select {
					case errs <- err:
					default:
					}
					return
				}
			}
		}
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go worker()
	}
submit:
	for _, u := range units {
		select {
		case <-ctx.Done():
			break submit
		case jobs <- u:
		}
	}
	close(jobs)
	wg.Wait()
	select {
	case err := <-errs:
		return err
	default:
		return ctx.Err()
	}
}

func (p StreamingPlan) validate() error {
	if len(p.Index.Functions) != len(p.Index.Order) {
		return fmt.Errorf("streaming index order mismatch")
	}
	seen := map[int]bool{}
	for _, u := range p.Units {
		for _, id := range u.FunctionIDs {
			if seen[id] {
				return fmt.Errorf("function %d assigned to multiple units", id)
			}
			seen[id] = true
		}
	}
	if len(seen) != len(p.Index.Functions) {
		return fmt.Errorf("streaming plan omits functions: have %d want %d", len(seen), len(p.Index.Functions))
	}
	return nil
}
