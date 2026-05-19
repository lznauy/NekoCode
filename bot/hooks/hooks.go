package hooks

import (
	"fmt"
	"strings"
)

// Hint represents a guardrail message to inject as a user message.
type Hint struct {
	Type     string
	Severity string // info, warning, critical
	Content  string
}

// StopReason signals the agent loop to stop.
type StopReason int

const (
	StopCompleted          StopReason = iota // normal completion
	StopInterrupted                          // user aborted
	StopFormatError                          // persistent garbled tool calls
)

func (s StopReason) String() string {
	switch s {
	case StopCompleted:
		return "completed"
	case StopInterrupted:
		return "interrupted"
	case StopFormatError:
		return "format_error"
	default:
		return "unknown"
	}
}

// State is the snapshot passed to hooks.
type State struct {
	NeedsVerification bool
	VerifyInjected    bool
	AllTasksDone      bool
	FilesModified     bool
	ActionIsChat      bool
	GarbledToolCall   bool
	GarbledCount      int
	TurnTokens        int64
	QuotaHard         bool
	QuotaReadsLeft    int
	ExplorationScore  int
	ExploreCascade    int
	StepInput         string
	OnFirstTurn       bool
	// Repeated tool call detection.
	RepeatedCallCount int
	RepeatedCallName  string
}

// InjectHook evaluates state and returns a hint to inject, or nil.
type InjectHook func(state *State) *Hint

// StopHook evaluates state and returns a stop reason, or false.
// The second return value is true if the agent should stop.
type StopHook func(state *State) (StopReason, bool)

type namedInject struct {
	name string
	fn   InjectHook
}

type namedStop struct {
	name string
	fn   StopHook
}

// Registry holds both inject and stop hooks.
type Registry struct {
	injectHooks []namedInject
	stopHooks   []namedStop
	Logf        func(string, ...any)
}

func NewRegistry() *Registry { return &Registry{} }

func (r *Registry) AddInject(name string, h InjectHook) {
	r.injectHooks = append(r.injectHooks, namedInject{name, h})
}

func (r *Registry) AddStop(name string, h StopHook) {
	r.stopHooks = append(r.stopHooks, namedStop{name, h})
}

// EvaluateInject returns non-empty hints from all inject hooks.
func (r *Registry) EvaluateInject(state *State) []Hint {
	var hints []Hint
	for _, h := range r.injectHooks {
		if hint := h.fn(state); hint != nil {
			if r.Logf != nil {
				r.Logf("hook inject %q triggered: type=%s severity=%s", h.name, hint.Type, hint.Severity)
			}
			hints = append(hints, *hint)
		}
	}
	return hints
}

// EvaluateStop returns the first stop reason found, or false.
func (r *Registry) EvaluateStop(state *State) (StopReason, bool) {
	for _, h := range r.stopHooks {
		if reason, stop := h.fn(state); stop {
			if r.Logf != nil {
				r.Logf("hook stop %q triggered: reason=%s garbledCount=%d explorationScore=%d", h.name, reason.String(), state.GarbledCount, state.ExplorationScore)
			}
			return reason, true
		}
	}
	return 0, false
}

// FormatHints packs hints into a tagged block for Layer 2 injection.
func FormatHints(hints []Hint) string {
	if len(hints) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<hints>\n")
	for _, h := range hints {
		fmt.Fprintf(&b, "  <hint type=%q severity=%q>%s</hint>\n", h.Type, h.Severity, h.Content)
	}
	b.WriteString("</hints>")
	return b.String()
}
