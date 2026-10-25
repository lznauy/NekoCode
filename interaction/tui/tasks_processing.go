package tui

import (
	"encoding/json"

	"nekocode/interaction/tui/components/processing"
	controlruntime "nekocode/runtime"
)

func taskProcessingText(records []controlruntime.SubagentRecord, live *taskLiveMessage) (output, thinking string) {
	for _, record := range records {
		if record.ContextOnly || record.Role != "assistant" {
			continue
		}
		output = processing.DisplayText(record.Content)
		if text := processing.DisplayText(record.Reasoning); text != "" {
			thinking = text
		}
		// The main page moves intermediate assistant text out of output when
		// a tool starts. Keep it in the explicit process history instead.
		if len(record.ToolCalls) > 0 {
			output = ""
		}
	}
	if live != nil {
		output = processing.DisplayText(live.text.String())
		if text := processing.DisplayText(live.reasoning.String()); text != "" {
			thinking = text
		}
	}
	return
}

func taskProcessingTodos(records []controlruntime.SubagentRecord, results map[string]controlruntime.SubagentRecord) string {
	var text string
	for _, record := range records {
		for _, call := range record.ToolCalls {
			result, done := results[call.ID]
			if call.Name != "todo_write" || !done || result.IsError {
				continue
			}
			var args struct {
				Todos []controlruntime.TodoItem `json:"todos"`
			}
			if json.Unmarshal([]byte(call.Arguments), &args) == nil {
				text = todoItemsText(args.Todos)
			}
		}
	}
	return text
}

// processingToolCalls supplies the same ordered calls to the status card and
// its inspector. Persisted arguments take precedence over live previews.
func (d *taskDetail) processingToolCalls(records []controlruntime.SubagentRecord) []controlruntime.SubagentToolCall {
	var calls []controlruntime.SubagentToolCall
	positions := make(map[string]int)
	appendCall := func(call controlruntime.SubagentToolCall) {
		if call.ID == "" {
			return
		}
		if i, ok := positions[call.ID]; ok {
			calls[i] = call
			return
		}
		positions[call.ID] = len(calls)
		calls = append(calls, call)
	}
	for _, record := range records {
		for _, call := range record.ToolCalls {
			appendCall(call)
		}
	}
	for _, id := range d.liveOrder {
		if _, saved := positions[id]; saved {
			continue
		}
		if live, ok := d.tools[id]; ok {
			appendCall(controlruntime.SubagentToolCall{ID: id, Name: live.payload.ToolName, Arguments: live.payload.Args})
		}
	}
	return calls
}
