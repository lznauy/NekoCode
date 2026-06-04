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
	r.mu.Lock()
	defer r.mu.Unlock()
	snap := &Snapshot{Store: r.store, Tool: tool, Error: toolError, strVals: r.strVals}
	var results []Result
	for _, h := range r.hooks {
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
		} else {
			debug.Log("[HOOK] %s @%s → HINT", h.Name, point)
		}
	}
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
		if strings.HasPrefix(k, "gauge:") || strings.HasPrefix(k, "value:") || strings.HasPrefix(k, "turn:") {
			delete(r.store, k)
		}
	}
	for k := range r.strVals {
		if strings.HasPrefix(k, "value:") {
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
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, h := range r.hooks {
		if h.Name == name {
			r.hooks = append(r.hooks[:i], r.hooks[i+1:]...)
			return
		}
	}
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
