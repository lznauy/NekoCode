package hooks

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"nekocode/bot/debug"
)

type HookPoint int

const (
	PointPreTurn  HookPoint = iota
	PointPostTool
	PointPostTurn
)

type Result struct {
	Hint *Hint
	Stop *StopReason
}

type Hook struct {
	Name       string
	Points     []HookPoint
	Priority   int
	Suppresses []string
	On         func(s *Snapshot) *Result
}

type Manager struct {
	store *Store
	hooks []Hook
}

func NewManager() *Manager {
	return &Manager{store: &Store{}}
}

func (m *Manager) Counter(k string)        { m.store.IncCounter(k) }
func (m *Manager) Flag(k string, v bool)   { m.store.SetFlag(k, v) }
func (m *Manager) Gauge(k string, v int64)  { m.store.SetGauge(k, v) }
func (m *Manager) Value(k string, v string) { m.store.SetValue(k, v) }
func (m *Manager) Turn(k string) { m.store.IncTurn(k) }
func (m *Manager) ResetTurn()    { m.store.ResetTurn() }

func (m *Manager) Register(h Hook) {
	if h.Points == nil {
		panic("hook " + h.Name + " has no Points")
	}
	m.hooks = append(m.hooks, h)
	sort.Slice(m.hooks, func(i, j int) bool {
		return m.hooks[i].Priority < m.hooks[j].Priority
	})
}

func (m *Manager) Evaluate(p HookPoint) []Result {
	snap := &Snapshot{store: m.store}
	suppressed := map[string]bool{}
	var results []Result

	for _, h := range m.hooks {
		if suppressed[h.Name] {
			continue
		}
		matches := false
		for _, pt := range h.Points {
			if pt == p {
				matches = true
				break
			}
		}
		if !matches {
			continue
		}
		result := h.On(snap)
		if result == nil {
			continue
		}
		results = append(results, *result)
		for _, s := range h.Suppresses {
			suppressed[s] = true
		}
		debug.Log("[HOOK] %s @%d | c:%s f:%s g:%s t:%s → %s",
			h.Name, p,
			fmtCounters(snap), fmtFlags(snap), fmtGauges(snap), fmtTurns(snap),
			summarizeResult(result))
	}
	return results
}

func (m *Manager) Dump() string {
	var sb strings.Builder
	sb.WriteString("--- Store ---\n")
	m.store.counters.Range(func(k, v any) bool { fmt.Fprintf(&sb, "  c:%s=%d\n", k, v.(*atomic.Int64).Load()); return true })
	m.store.flags.Range(func(k, v any) bool { fmt.Fprintf(&sb, "  f:%s=%v\n", k, v.(*atomic.Bool).Load()); return true })
	m.store.gauges.Range(func(k, v any) bool { fmt.Fprintf(&sb, "  g:%s=%d\n", k, v.(*atomic.Int64).Load()); return true })
	m.store.turns.Range(func(k, v any) bool { fmt.Fprintf(&sb, "  t:%s=%d\n", k, v.(*atomic.Int64).Load()); return true })
	sb.WriteString("\n--- Hooks ---\n")
	for _, h := range m.hooks {
		fmt.Fprintf(&sb, "  %s points=%v priority=%d\n", h.Name, h.Points, h.Priority)
	}
	return sb.String()
}

func (m *Manager) Reset() {
	m.store = &Store{}
	m.hooks = nil
}

func summarizeResult(r *Result) string {
	if r.Stop != nil {
		return fmt.Sprintf("STOP(%s)", r.Stop.String())
	}
	return "HINT"
}

func fmtCounters(s *Snapshot) string { return fmtMap(&s.store.counters, false) }
func fmtFlags(s *Snapshot) string    { return fmtMap(&s.store.flags, true) }
func fmtGauges(s *Snapshot) string   { return fmtMap(&s.store.gauges, false) }
func fmtTurns(s *Snapshot) string    { return fmtMap(&s.store.turns, false) }

func fmtMap(m *sync.Map, isBool bool) string {
	var parts []string
	m.Range(func(k, v any) bool {
		if isBool {
			parts = append(parts, fmt.Sprintf("%s=%v", k, v.(*atomic.Bool).Load()))
		} else {
			parts = append(parts, fmt.Sprintf("%s=%d", k, v.(*atomic.Int64).Load()))
		}
		return true
	})
	return strings.Join(parts, " ")
}
