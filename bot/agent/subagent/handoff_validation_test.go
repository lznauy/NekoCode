package subagent

import (
	"encoding/json"
	"strings"
	"testing"

	tools "nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/provider/types"
)

func TestMalformedHandoffRequiresWorkerCorrection(t *testing.T) {
	cases := []struct {
		name, field string
		invalid     func(map[string]any)
	}{
		{"missing unfinished", "unfinished", func(a map[string]any) { delete(a, "unfinished") }},
		{"missing risks", "risks", func(a map[string]any) { delete(a, "risks") }},
		{"risks text", "risks", func(a map[string]any) { a["risks"] = "confirmed data loss" }},
		{"mixed risks", "risks", func(a map[string]any) { a["risks"] = []any{"confirmed data loss", 7} }},
		{"missing next action", "next_action", func(a map[string]any) { a["outcome"] = "continue"; a["unfinished"] = []any{"remaining check"} }},
		{"empty unfinished", "unfinished", func(a map[string]any) { a["outcome"] = "blocked"; a["next_action"] = "supply prerequisite" }},
		{"missing check status", "status", func(a map[string]any) {
			c := verificationArgs("failed")
			delete(c, "status")
			a["verification_checks"] = []any{c}
		}},
		{"missing check limitations", "limitations", func(a map[string]any) {
			c := verificationArgs("failed")
			delete(c, "limitations")
			a["verification_checks"] = []any{c}
		}},
		{"invalid check status", "status", func(a map[string]any) { a["verification_checks"] = []any{verificationArgs("static")} }},
		{"invalid check method", "method", func(a map[string]any) {
			c := verificationArgs("failed")
			c["method"] = "not_run"
			a["verification_checks"] = []any{c}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			invalid := handoffArgs()
			invalid["summary"] = "original worker evidence"
			tc.invalid(invalid)
			raw, _ := json.Marshal(invalid)
			if _, submitted, err := submittedResult([]core.ToolCallItem{{Name: submitResultToolName, Args: invalid}}); !submitted || err == nil || !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("invalid %s was not rejected: %v", tc.field, err)
			}
			after, _ := json.Marshal(invalid)
			if string(after) != string(raw) {
				t.Fatal("validation rewrote worker facts")
			}
			corrected := handoffArgs()
			corrected["summary"] = "worker corrected report"
			corrected["risks"] = []any{"confirmed data loss"}
			corrected["verification_checks"] = []any{verificationArgs("failed")}
			fixed, _ := json.Marshal(corrected)
			llm := &scriptedLLM{scripts: [][]types.StreamToken{
				{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "rejected", Name: submitResultToolName, Arguments: string(raw)}}},
				{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "corrected", Name: submitResultToolName, Arguments: string(fixed)}}},
			}}
			result, err := New(Config{LLM: llm, Tools: tools.New()}).Run(t.Context(), RunConfig{Prompt: "review", Profile: Profile{Name: "executor"}})
			if err != nil || llm.calls != 2 || result.Handoff == nil || result.Handoff.Summary != "worker corrected report" {
				t.Fatalf("worker correction was not required: %+v calls=%d err=%v", result, llm.calls, err)
			}
			originalRetained := false
			for _, message := range llm.messages[1] {
				for _, call := range message.ToolCalls {
					if call.ID == "rejected" && call.Function.Arguments == string(raw) {
						originalRetained = true
					}
				}
			}
			if !originalRetained {
				t.Fatal("correction context lost the exact original report")
			}
			retry, _ := json.Marshal(llm.messages[1])
			for _, want := range []string{"original worker evidence", tc.field, "Sub-agent handoff correction"} {
				if !strings.Contains(string(retry), want) {
					t.Fatalf("retry lost %q: %s", want, retry)
				}
			}
			if !messagesContainToolResult(llm.messages[1], "rejected") {
				t.Fatal("rejected call has no paired error result")
			}
			if result.Handoff.Risks[0] != "confirmed data loss" || result.Handoff.Checks[0].Status != "failed" {
				t.Fatal("worker-corrected facts changed")
			}
		})
	}
}

func TestRejectedHandoffExhaustionRemainsPartial(t *testing.T) {
	args := handoffArgs()
	delete(args, "unfinished")
	raw, _ := json.Marshal(args)
	script := []types.StreamToken{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "invalid", Name: submitResultToolName, Arguments: string(raw)}}}
	llm := &scriptedLLM{}
	for range maxCompletionProtocolRetries + 1 {
		llm.scripts = append(llm.scripts, script)
	}
	result, err := New(Config{LLM: llm, Tools: tools.New()}).Run(t.Context(), RunConfig{Prompt: "review", Profile: Profile{Name: "executor"}})
	if err != nil || result.Status != StatusPartial || result.Handoff != nil || llm.calls != maxCompletionProtocolRetries+1 {
		t.Fatalf("invalid handoff became accepted facts: %+v calls=%d err=%v", result, llm.calls, err)
	}
}

func TestHandoffCorrectionBlocksOrdinaryToolsAndKeepsRetryLimit(t *testing.T) {
	args := handoffArgs()
	delete(args, "unfinished")
	raw, _ := json.Marshal(args)
	invalid := []types.StreamToken{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "invalid", Name: submitResultToolName, Arguments: string(raw)}}}
	ordinary := []types.StreamToken{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "ordinary", Name: "write", Arguments: "{}"}}}
	llm := &scriptedLLM{scripts: [][]types.StreamToken{invalid}}
	for range maxCompletionProtocolRetries + 1 {
		llm.scripts = append(llm.scripts, ordinary, invalid)
	}
	fixed, _ := json.Marshal(handoffArgs())
	llm.scripts = append(llm.scripts, []types.StreamToken{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "fixed", Name: submitResultToolName, Arguments: string(fixed)}}})
	write := &countingTool{name: "write"}
	registry := tools.New()
	registry.Register(write)
	result, err := New(Config{LLM: llm, Tools: registry}).Run(t.Context(), RunConfig{Prompt: "work", Profile: Profile{Name: "executor", Tools: []string{"write"}}})
	if err != nil || result.Status != StatusPartial || write.calls != 0 || llm.calls != maxCompletionProtocolRetries+1 {
		t.Fatalf("correction escaped its boundary: result=%+v tools=%d requests=%d err=%v", result, write.calls, llm.calls, err)
	}
	for _, definitions := range llm.tools[1:] {
		if len(definitions) != 1 || definitions[0].Function.Name != submitResultToolName {
			t.Fatal("correction exposed ordinary tools")
		}
	}
}
