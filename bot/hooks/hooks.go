package hooks

import (
	"fmt"
	"strings"
	"sync"

	"nekocode/bot/debug"
)

// ---------------------------------------------------------------------------
// types
// ---------------------------------------------------------------------------

type HookPoint string

const (
	PreTurn     HookPoint = "pre_turn"
	PreToolUse  HookPoint = "pre_tool_use"
	PostToolUse HookPoint = "post_tool_use" // per-tool (declarative hooks)
	PostTool    HookPoint = "post_tool"     // batch (builtin hooks)
	PostTurn    HookPoint = "post_turn"
	UserSubmit  HookPoint = "user_submit"
	Stop        HookPoint = "stop"
)

type Hint struct {
	Type     string
	Severity string
	Content  string
}

type StopReason string

const (
	StopFormatError StopReason = "format_error"
	StopInterrupted StopReason = "interrupted"
	StopCompleted   StopReason = "completed"
)

func (s StopReason) String() string { return string(s) }

type Result struct {
	Hint *Hint
	Stop *StopReason
}

type Snapshot struct {
	Store   map[string]int64
	Tool    string
	Error   bool
	strVals map[string]string
}

func (s *Snapshot) get(k string) int64     { return s.Store[k] }
func (s *Snapshot) set(k string, v int64)   { s.Store[k] = v }
func (s *Snapshot) flag(k string) bool      { return s.Store[k] == 1 }
func (s *Snapshot) getStr(k string) string  { return s.strVals[k] }

type Hook struct {
	Name  string
	Point HookPoint
	On    func(s *Snapshot) *Result
}

// ---------------------------------------------------------------------------
// ---------------------------------------------------------------------------
// registry
// ---------------------------------------------------------------------------

type Registry struct {
	mu      sync.Mutex
	hooks   []Hook
	store   map[string]int64
	strVals map[string]string
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

func (r *Registry) Evaluate(point HookPoint, tool string, toolError bool) []Result {
	// Copy hooks and snapshot data under lock, then execute outside lock.
	// Plugin hooks may run shell commands (seconds of I/O); holding the
	// mutex during execution would block all Set/Inc/Flag calls.
	r.mu.Lock()
	hooks := make([]Hook, len(r.hooks))
	copy(hooks, r.hooks)
	// Copy maps so hook callbacks can mutate the snapshot without racing
	// against concurrent Set/Inc/Flag calls that acquire r.mu.
	storeCopy := make(map[string]int64, len(r.store))
	for k, v := range r.store {
		storeCopy[k] = v
	}
	strCopy := make(map[string]string, len(r.strVals))
	for k, v := range r.strVals {
		strCopy[k] = v
	}
	snap := &Snapshot{Store: storeCopy, Tool: tool, Error: toolError, strVals: strCopy}

	// Snapshot flag/counter values before hooks mutate storeCopy,
	// so the write-back can compute deltas safely (see below).
	origSnap := make(map[string]int64, len(storeCopy))
	for k, v := range storeCopy {
		if strings.HasPrefix(k, "flag:") || strings.HasPrefix(k, "counter:") {
			origSnap[k] = v
		}
	}
	r.mu.Unlock()

	var results []Result
	for _, h := range hooks {
		if h.Point != point {
			continue
		}
		result := h.On(snap)
		if result == nil {
			continue
		}
		results = append(results, *result)
		if result.Stop != nil {
			debug.Log("[HOOK] %s @%s → STOP(%s)", h.Name, point, result.Stop.String())
		} else if result.Hint != nil {
			debug.Log("[HOOK] %s @%s → HINT(%s)", h.Name, point, result.Hint.Content)
		}
	}

	// Write back flag/counter mutations as deltas so concurrent
	// Set/Inc/Flag calls from the agent goroutine are not overwritten.
	// We snapshot the pre-evaluation values and compute the delta for
	// each key that actually changed — only that delta is applied to
	// the current store, preserving any concurrent mutations.
	r.mu.Lock()
	for k, newV := range storeCopy {
		if !strings.HasPrefix(k, "flag:") && !strings.HasPrefix(k, "counter:") {
			continue
		}
		// origSnap holds the snapshot taken before hook callbacks ran.
		// Compute delta and apply it to the current store value, which
		// may have been mutated concurrently by Inc/Set/Flag.
		if oldV, existed := origSnap[k]; existed {
			if newV != oldV {
				r.store[k] += newV - oldV
			}
		} else {
			// New key created by a hook callback — set it directly.
			r.store[k] = newV
		}
	}
	r.mu.Unlock()

	return results
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
}

func (r *Registry) ResetTurn() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k := range r.store {
		// Clear per-turn and per-gauge keys. flag: keys are per-turn guards
		// (reset each turn so hooks can re-fire). counter: keys accumulate
		// across turns (e.g., idle call detection) and are NOT cleared here.
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

// UnregisterByPrefix removes all hooks whose name starts with the given prefix.
func (r *Registry) UnregisterByPrefix(prefix string) {
	r.UnregisterWhere(func(h Hook) bool { return strings.HasPrefix(h.Name, prefix) })
}

// UnregisterWhere removes all hooks for which fn returns true.
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

// ---------------------------------------------------------------------------
// FormatHints
// ---------------------------------------------------------------------------

func FormatHints(hints []Hint) string {
	if len(hints) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<hints>\n")
	for _, h := range hints {
		sev := h.Severity
		if sev == "" {
			sev = "info"
		}
		fmt.Fprintf(&b, "  <hint type=%q severity=%q>\n    %s\n  </hint>\n", h.Type, sev, h.Content)
	}
	b.WriteString("</hints>")
	return b.String()
}
