package acp

import (
	"encoding/json"
	"strings"

	controlruntime "nekocode/runtime"
)

// ACP text chunks are append-only. Speculative deltas cannot be forwarded:
// retries may reset them, and ACP has no corresponding retraction operation.
// Canonical messages preserve tool preambles; the terminal result covers
// runners that return text without emitting canonical message events.
type turnUpdates struct {
	lastMessage string
}

func (t *turnUpdates) update(event controlruntime.Event) ([]map[string]any, bool) {
	switch event.Type {
	case controlruntime.EventAssistantDelta, controlruntime.EventAssistantReset:
		return nil, false
	case controlruntime.EventAssistantMessage:
		payload, ok := event.Payload.(controlruntime.MessagePayload)
		if !ok {
			return nil, false
		}
		t.lastMessage = payload.Content
		return assistantText(payload.Content)
	case controlruntime.EventRunDone:
		result, ok := event.Payload.(controlruntime.RunResult)
		if !ok || result.Output == t.lastMessage {
			return nil, false
		}
		return assistantText(result.Output)
	default:
		return eventUpdate(event)
	}
}

func assistantText(text string) ([]map[string]any, bool) {
	if text == "" {
		return nil, false
	}
	return []map[string]any{{"sessionUpdate": "agent_message_chunk",
		"content": map[string]any{"type": "text", "text": text}}}, true
}

func eventUpdate(event controlruntime.Event) ([]map[string]any, bool) {
	switch event.Type {
	case controlruntime.EventSystemMessage:
		payload, ok := event.Payload.(controlruntime.MessagePayload)
		if !ok || payload.Content == "" {
			return nil, false
		}
		return []map[string]any{{
			"sessionUpdate": "agent_message_chunk",
			"content":       map[string]any{"type": "text", "text": payload.Content},
		}}, true
	case controlruntime.EventReasoningDelta:
		payload, ok := event.Payload.(controlruntime.DeltaPayload)
		if !ok || payload.Delta == "" {
			return nil, false
		}
		return []map[string]any{{
			"sessionUpdate": "agent_thought_chunk",
			"content":       map[string]any{"type": "text", "text": payload.Delta},
		}}, true
	case controlruntime.EventToolStarted:
		payload, ok := event.Payload.(controlruntime.ToolPayload)
		if !ok {
			return nil, false
		}
		return []map[string]any{toolCall(event, payload)}, true
	case controlruntime.EventToolPreview:
		payload, ok := event.Payload.(controlruntime.ToolPayload)
		if !ok {
			return nil, false
		}
		return []map[string]any{toolUpdate(event, payload, "in_progress", payload.Preview)}, true
	case controlruntime.EventToolCompleted:
		payload, ok := event.Payload.(controlruntime.ToolPayload)
		if !ok {
			return nil, false
		}
		status := "completed"
		if payload.IsError {
			status = "failed"
		}
		return []map[string]any{toolUpdate(event, payload, status, payload.Output)}, true
	case controlruntime.EventToolBlocked:
		payload, ok := event.Payload.(controlruntime.ToolPayload)
		if !ok {
			return nil, false
		}
		// Blocked tools never emitted a tool_call event, so the update must
		// be preceded by the tool_call it refers to.
		return []map[string]any{toolCall(event, payload), toolUpdate(event, payload, "failed", payload.Output)}, true
	case controlruntime.EventTodosUpdated:
		items, ok := event.Payload.([]controlruntime.TodoItem)
		if !ok {
			return nil, false
		}
		entries := make([]map[string]any, 0, len(items))
		for _, item := range items {
			entries = append(entries, map[string]any{
				"content": item.Content, "priority": "medium", "status": planStatus(item.Status),
			})
		}
		return []map[string]any{{"sessionUpdate": "plan", "entries": entries}}, true
	case controlruntime.EventMetricsUpdated:
		metrics, ok := event.Payload.(controlruntime.MetricsSnapshot)
		if !ok {
			return nil, false
		}
		if update := usageUpdate(metrics); update != nil {
			return []map[string]any{update}, true
		}
		return nil, false
	default:
		return nil, false
	}
}

// usageUpdate renders the context occupancy as an ACP usage_update. Empty
// snapshots (no usage yet) yield nil so callers can skip them.
func usageUpdate(metrics controlruntime.MetricsSnapshot) map[string]any {
	if metrics.ContextTokens == 0 && metrics.ContextBudget == 0 {
		return nil
	}
	return map[string]any{
		"sessionUpdate": "usage_update",
		"used":          metrics.ContextTokens,
		"size":          metrics.ContextBudget,
	}
}

func toolCall(event controlruntime.Event, payload controlruntime.ToolPayload) map[string]any {
	return map[string]any{
		"sessionUpdate": "tool_call",
		"toolCallId":    toolCallID(event, payload),
		"title":         toolTitle(payload),
		"kind":          toolKind(payload.ToolName),
		"status":        "pending",
		"rawInput":      rawValue(payload.Args),
	}
}

func toolUpdate(event controlruntime.Event, payload controlruntime.ToolPayload, status, output string) map[string]any {
	return toolUpdateByID(toolCallID(event, payload), status, output)
}

func toolUpdateByID(callID, status, output string) map[string]any {
	update := map[string]any{
		"sessionUpdate": "tool_call_update",
		"toolCallId":    callID,
		"status":        status,
	}
	if output != "" {
		update["content"] = []any{map[string]any{
			"type": "content", "content": map[string]any{"type": "text", "text": output},
		}}
		update["rawOutput"] = output
	}
	return update
}

func toolCallID(event controlruntime.Event, payload controlruntime.ToolPayload) string {
	if payload.CallID != "" {
		return payload.CallID
	}
	return event.ID
}

func toolTitle(payload controlruntime.ToolPayload) string {
	if payload.ToolName == "" {
		return "Tool call"
	}
	return payload.ToolName
}

func toolKind(name string) string {
	name = strings.ToLower(name)
	switch {
	case strings.Contains(name, "read"), strings.Contains(name, "list"), strings.Contains(name, "glob"):
		return "read"
	case strings.Contains(name, "write"), strings.Contains(name, "edit"), strings.Contains(name, "patch"):
		return "edit"
	case strings.Contains(name, "delete"), strings.Contains(name, "remove"):
		return "delete"
	case strings.Contains(name, "search"), strings.Contains(name, "grep"):
		return "search"
	case strings.Contains(name, "fetch"), strings.Contains(name, "web"):
		return "fetch"
	case strings.Contains(name, "shell"), strings.Contains(name, "exec"), strings.Contains(name, "bash"):
		return "execute"
	case strings.Contains(name, "think"), strings.Contains(name, "plan"), strings.Contains(name, "todo"):
		return "think"
	default:
		return "other"
	}
}

func rawValue(value string) any {
	if value == "" {
		return nil
	}
	var decoded any
	if json.Unmarshal([]byte(value), &decoded) == nil {
		return decoded
	}
	return value
}

func planStatus(status string) string {
	switch status {
	case "completed":
		return "completed"
	case "in_progress":
		return "in_progress"
	default:
		return "pending"
	}
}
