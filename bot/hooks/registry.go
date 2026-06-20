package hooks

import (
	"fmt"
	"strings"
	"sync"

	"nekocode/bot/debug"
)

type HookCounts struct {
	Evaluations  int64
	Hints        int64
	Stops        int64
	BlockTools   int64
	RequireTools int64
	BlockFinals  int64
}

func (c HookCounts) String() string {
	return fmt.Sprintf("hooks: %d eval, %d hints, %d stops, %d block_tool, %d require_tool, %d block_final",
		c.Evaluations, c.Hints, c.Stops, c.BlockTools, c.RequireTools, c.BlockFinals)
}

type Registry struct {
	mu      sync.Mutex
	hooks   []Hook
	store   map[string]int64
	strVals map[string]string
	counts  HookCounts
}

func NewRegistry() *Registry {
	return &Registry{
		store:   make(map[string]int64),
		strVals: make(map[string]string),
	}
}

func (r *Registry) Register(h Hook) {
	r.mu.Lock()
	r.hooks = append(r.hooks, h)
	r.mu.Unlock()
}

func (r *Registry) Evaluate(point HookPoint, tool string, toolError bool, toolArgs ...map[string]any) []Result {
	hooks, snap := r.evaluationSnapshot(tool, toolError, toolArgs...)
	results, evaluated := evaluateHooks(hooks, point, snap)
	r.recordEvaluation(results, evaluated)
	r.applyPatch(snap.patch)
	return results
}

func (r *Registry) evaluationSnapshot(tool string, toolError bool, toolArgs ...map[string]any) ([]Hook, *Snapshot) {
	r.mu.Lock()
	defer r.mu.Unlock()

	hooks := make([]Hook, len(r.hooks))
	copy(hooks, r.hooks)

	storeCopy := make(map[string]int64, len(r.store))
	for k, v := range r.store {
		storeCopy[k] = v
	}
	strCopy := make(map[string]string, len(r.strVals))
	for k, v := range r.strVals {
		strCopy[k] = v
	}

	snap := &Snapshot{Store: storeCopy, Tool: tool, Error: toolError, strVals: strCopy}
	if len(toolArgs) > 0 && toolArgs[0] != nil {
		snap.Args = toolArgs[0]
	}
	return hooks, snap
}

func evaluateHooks(hooks []Hook, point HookPoint, snap *Snapshot) ([]Result, int64) {
	var results []Result
	var evaluated int64

	for _, h := range hooks {
		if h.Point != point {
			continue
		}
		evaluated++
		result := h.On(snap)
		if result == nil {
			continue
		}
		applyResultPatch(snap, result.StatePatch)
		results = append(results, *result)
		logHookResult(h, point, result)
	}

	return results, evaluated
}

func applyResultPatch(snap *Snapshot, patch *StatePatch) {
	if patch == nil {
		return
	}
	for k, v := range patch.Ints {
		snap.set(k, v)
	}
	for k, v := range patch.Strings {
		snap.setStr(k, v)
	}
}

func logHookResult(h Hook, point HookPoint, result *Result) {
	switch {
	case result.Stop != nil:
		debug.Log("[HOOK] %s @%s → STOP(%s)", h.Name, point, result.Stop.String())
	case result.BlockTool != nil:
		debug.Log("[HOOK] %s @%s → BLOCK_TOOL(%s: %s)", h.Name, point, result.BlockTool.Tool, result.BlockTool.Reason)
	case result.RequireTool != nil:
		debug.Log("[HOOK] %s @%s → REQUIRE_TOOL(%s: %s)", h.Name, point, result.RequireTool.Tool, result.RequireTool.Reason)
	case result.BlockFinal != nil:
		debug.Log("[HOOK] %s @%s → BLOCK_FINAL(%s)", h.Name, point, result.BlockFinal.Reason)
	case result.Hint != nil:
		debug.Log("[HOOK] %s @%s → HINT(%s)", h.Name, point, result.Hint.Content)
	}
}

func (r *Registry) recordEvaluation(results []Result, evaluated int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.counts.Evaluations += evaluated
	for _, res := range results {
		if res.Hint != nil {
			r.counts.Hints++
		}
		if res.Stop != nil {
			r.counts.Stops++
		}
		if res.BlockTool != nil {
			r.counts.BlockTools++
		}
		if res.RequireTool != nil {
			r.counts.RequireTools++
		}
		if res.BlockFinal != nil {
			r.counts.BlockFinals++
		}
	}
}

func (r *Registry) applyPatch(patch StatePatch) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for k, v := range patch.Ints {
		r.store[k] = v
	}
	for k, v := range patch.Strings {
		r.strVals[k] = v
	}
}

func (r *Registry) ResetSession() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k := range r.store {
		delete(r.store, k)
	}
	for k := range r.strVals {
		delete(r.strVals, k)
	}
	r.counts = HookCounts{}
}

func (r *Registry) GovernanceStats() string {
	r.mu.Lock()
	c := r.counts
	r.counts = HookCounts{}
	r.mu.Unlock()
	return " | " + c.String()
}

func (r *Registry) HookCountsSnapshot() HookCounts {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts
}

func (r *Registry) ResetTurn() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k := range r.store {
		if strings.HasPrefix(k, "gauge:") || strings.HasPrefix(k, "value:") || strings.HasPrefix(k, "turn:") ||
			strings.HasPrefix(k, "flag:") {
			delete(r.store, k)
		}
	}
	for k := range r.strVals {
		if strings.HasPrefix(k, "value:") || strings.HasPrefix(k, "turn:") {
			delete(r.strVals, k)
		}
	}
}

func (r *Registry) Set(k string, v int64) {
	r.mu.Lock()
	r.store[k] = v
	r.mu.Unlock()
}

func (r *Registry) Inc(k string) {
	r.mu.Lock()
	r.store[k]++
	r.mu.Unlock()
}

func (r *Registry) Flag(k string, v bool) {
	var n int64
	if v {
		n = 1
	}
	r.Set(k, n)
}

func (r *Registry) SetStr(k, v string) {
	r.mu.Lock()
	r.strVals[k] = v
	r.mu.Unlock()
}

func (r *Registry) List() []Hook {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Hook, len(r.hooks))
	copy(out, r.hooks)
	return out
}

func (r *Registry) Unregister(name string) {
	r.UnregisterWhere(func(h Hook) bool { return h.Name == name })
}

func (r *Registry) UnregisterWhere(fn func(Hook) bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	filtered := make([]Hook, 0, len(r.hooks))
	for _, h := range r.hooks {
		if !fn(h) {
			filtered = append(filtered, h)
		}
	}
	r.hooks = filtered
}
