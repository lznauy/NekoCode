package toolflow

import (
	"strings"
	"testing"

	"nekocode/bot/tools"
)

func TestMergeResultsPreservesOriginalCallOrder(t *testing.T) {
	calls := []tools.ToolCallItem{
		{ID: "1", Name: "read", Args: map[string]any{"path": "a.go"}},
		{ID: "2", Name: "write", Args: map[string]any{"path": "b.go"}},
		{ID: "3", Name: "bash", Args: map[string]any{"command": "go test ./..."}},
	}
	execResults := []tools.ToolCallResult{
		{ID: "1", Name: "read", Output: "read ok"},
		{ID: "3", Name: "bash", Output: "bash ok"},
	}

	results := MergeResults(calls, map[int]string{1: "blocked"}, execResults)
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3", len(results))
	}
	if results[0].Output != "read ok" || results[1].Error != "blocked" || results[2].Output != "bash ok" {
		t.Fatalf("unexpected result order: %+v", results)
	}
}

func TestEmitResultCallbacksUsesEffectiveOutput(t *testing.T) {
	var gotOutput string
	msgs := EmitResultCallbacks(
		[]tools.ToolCallItem{{ID: "1", Name: "read", Args: map[string]any{"path": "a.go"}}},
		[]tools.ToolCallResult{{ID: "1", Name: "read", Output: "ok"}},
		func(action, toolName, toolArgs, output string) {
			gotOutput = output
		},
	)

	if len(msgs) != 1 || msgs[0].ToolCallID != "1" || msgs[0].Content != "ok" {
		t.Fatalf("messages = %+v, want one tool result message", msgs)
	}
	if gotOutput != "ok" {
		t.Fatalf("callback output = %q, want ok", gotOutput)
	}
}

func TestEmitResultCallbacksMarksErrors(t *testing.T) {
	msgs := EmitResultCallbacks(
		[]tools.ToolCallItem{{ID: "1", Name: "bash", Args: map[string]any{"command": "false"}}},
		[]tools.ToolCallResult{{ID: "1", Name: "bash", Error: "command failed: exit status 1"}},
		nil,
	)

	if len(msgs) != 1 || msgs[0].Content != "command failed: exit status 1" || !msgs[0].IsError {
		t.Fatalf("messages = %+v, want failed tool result with IsError", msgs)
	}
}

func TestEmitStartCallbacksMarksBlockedCalls(t *testing.T) {
	var events []string
	EmitStartCallbacks([]tools.ToolCallItem{
		{Name: "read", Args: map[string]any{"path": "x.go"}},
		{Name: "write", Args: map[string]any{"path": "x.go"}},
	}, map[int]string{1: "blocked"}, func(action, toolName, toolArgs, output string) {
		events = append(events, action+":"+toolName)
	})

	if got, want := strings.Join(events, ","), "tool_start:read,tool_blocked:write"; got != want {
		t.Fatalf("events = %q, want %q", got, want)
	}
}
