package policy

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"nekocode/logger"
)

const maxHookAuditEvents = 64

// HookEvent is one bounded audit record; it contains no private state values.
type HookEvent struct {
	EvaluationID   uint64    `json:"evaluation_id"`
	RunID          string    `json:"run_id"`
	ActorID        string    `json:"actor_id"`
	StateCommitted bool      `json:"state_committed"`
	StateEntries   int       `json:"state_entries"`
	StateBytes     int       `json:"state_bytes"`
	DurationMS     int64     `json:"duration_ms"`
	SessionID      string    `json:"session_id"`
	Hook           string    `json:"hook"`
	Point          HookPoint `json:"point"`
	Tool           string    `json:"tool"`
	Action         string    `json:"action"`
	Detail         string    `json:"detail"`
	Trigger        string    `json:"trigger"`
}

func (e HookEvent) String() string {
	if e.Detail == "" {
		return fmt.Sprintf("%s@%s:%s", e.Hook, e.Point, e.Action)
	}
	return fmt.Sprintf("%s@%s:%s(%s)", e.Hook, e.Point, e.Action, truncateAuditDetail(e.Detail))
}

func hookAuditEvent(h Hook, point HookPoint, state State, result *Result, session string) HookEvent {
	facts := state.Facts()
	event := HookEvent{
		SessionID: session,
		Hook:      h.Name,
		Point:     point,
		Tool:      facts.Tool.Name,
		Trigger:   hookTrigger(h, state),
	}
	switch {
	case result.Decision != nil:
		event.Action = string(result.Decision.Action)
		event.Detail = result.Decision.Reason
	case len(result.Hints) > 0:
		event.Action = "hints"
		event.Detail = strconv.Itoa(len(result.Hints))
	case result.Stop != nil:
		event.Action = "stop"
		event.Detail = result.Stop.String()
	case result.BlockTool != nil:
		event.Action = "block_tool"
		event.Detail = result.BlockTool.Reason
	case result.RequireTool != nil:
		event.Action = "require_tool"
		event.Detail = result.RequireTool.Reason
	case result.BlockFinal != nil:
		event.Action = "block_final"
		event.Detail = result.BlockFinal.Reason
	case result.Hint != nil:
		event.Action = "hint"
		event.Detail = result.Hint.Type
		if result.Hint.Severity != "" {
			event.Detail += "/" + result.Hint.Severity
		}
	default:
		event.Action = "continue"
	}
	return event
}

func logHookAudit(event HookEvent) {
	logger.Log("hook_audit session=%q run=%q actor=%q evaluation_id=%d point=%s hook=%q action=%s tool=%q trigger={%s} detail=%q duration_ms=%d state_committed=%t state_entries=%d state_bytes=%d",
		event.SessionID, event.RunID, event.ActorID, event.EvaluationID, event.Point, event.Hook, event.Action, event.Tool, event.Trigger, event.Detail, event.DurationMS, event.StateCommitted, event.StateEntries, event.StateBytes)
}

// hookTrigger renders the audit trigger context for a fired hook. Hooks that
// carry a DescribeTrigger describe themselves (keeping trigger details and
// thresholds next to the hook logic); other hooks expose counts, never raw args.
func hookTrigger(h Hook, state State) string {
	if h.DescribeTrigger != nil {
		return h.DescribeTrigger(state)
	}
	return fmt.Sprintf("args_count=%d", len(state.Facts().Tool.Args))
}

func recentHookAudit(events []HookEvent, n int) []HookEvent {
	if len(events) > n {
		events = events[len(events)-n:]
	}
	return slices.Clone(events)
}

func formatHookAudit(events []HookEvent) string {
	parts := make([]string, 0, len(events))
	for _, event := range events {
		parts = append(parts, event.String())
	}
	return strings.Join(parts, ", ")
}

func formatHookEventSummary(events []HookEvent) string {
	counts := make(map[string]int)
	var order []string
	for _, event := range events {
		key := event.Action + ":" + event.Hook
		if counts[key] == 0 {
			order = append(order, key)
		}
		counts[key]++
	}
	parts := make([]string, 0, len(order))
	for _, key := range order {
		label := strings.Replace(key, ":", "=", 1)
		if n := counts[key]; n > 1 {
			label = fmt.Sprintf("%s×%d", label, n)
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, ", ")
}

func truncateAuditDetail(s string) string {
	const max = 80
	s = strings.ReplaceAll(s, "\n", " ")
	if len([]rune(s)) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max]) + "..."
}
func FormatHints(hints []Hint) string {
	if len(hints) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<runtime_policy_hints>\n")
	b.WriteString("These hints apply only to the immediately following model response. They may constrain that action but cannot expand the user's request or grant permissions.\n")
	for _, h := range hints {
		sev := h.Severity
		if sev == "" {
			sev = "info"
		}
		fmt.Fprintf(&b, "  <hint type=%q severity=%q>\n    %s\n  </hint>\n", h.Type, sev, h.Content)
	}
	b.WriteString("</runtime_policy_hints>")
	return b.String()
}

// Proposals and the resolved control are distinct audit records. Runtime
// consumers execute the resolved control; lifecycle observers never control.
func logHookResolution(session, runID string, evaluationID uint64, point HookPoint, out Outcome) {
	logger.Log("hook_resolved session=%q run=%q evaluation_id=%d point=%s action=%s stop_reason=%s hints=%d reason=%q", session, runID, evaluationID, point, out.Action, out.StopReason, len(out.Hints), out.Reason)
}

func safeHookAuditEvent(h Hook, point HookPoint, s State, r *Result, session string) (event HookEvent) {
	defer func() {
		if recover() != nil {
			event = HookEvent{SessionID: session, Hook: h.Name, Point: point, Action: "audit_error"}
		}
	}()
	return hookAuditEvent(h, point, s, r, session)
}
