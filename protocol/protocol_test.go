package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestApprovalCloneDoesNotShareRememberedRules(t *testing.T) {
	original := &ApprovalContext{RememberRules: []ApprovalRememberRule{{Tool: "shell", Literal: "echo ok"}}}
	clone := original.Clone()
	clone.RememberRules[0].Literal = "changed"
	if original.RememberRules[0].Literal != "echo ok" {
		t.Fatal("approval preview shares mutable rules")
	}
}

func TestMetricsJSONUsesProtocolFieldNames(t *testing.T) {
	data, err := json.Marshal(Metrics{PromptTokens: 10, CompletionTokens: 5, TurnTotal: 105, TurnInput: 100, TurnCached: 80, TurnNew: 20, TurnOutput: 5, TurnReasoning: 2, TurnCacheReported: true, Duration: "1s"})
	if err != nil {
		t.Fatalf("marshal metrics: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, `"promptTokens"`) || !strings.Contains(got, `"completionTokens"`) {
		t.Fatalf("metrics json missing protocol names: %s", got)
	}
	if strings.Contains(got, `"PromptTokens"`) || strings.Contains(got, `"CompletionTokens"`) {
		t.Fatalf("metrics json leaked Go fields: %s", got)
	}
	for _, field := range []string{`"turnTotal":105`, `"turnInput":100`, `"turnCached":80`, `"turnNew":20`, `"turnOutput":5`, `"turnReasoning":2`, `"turnCacheReported":true`} {
		if !strings.Contains(got, field) {
			t.Fatalf("metrics missing %s: %s", field, got)
		}
	}
}

func TestMetricsJSONOmitsUnavailableReasoning(t *testing.T) {
	data, err := json.Marshal(Metrics{TurnTotal: 105, TurnInput: 100, TurnOutput: 5})
	if err != nil {
		t.Fatalf("marshal metrics: %v", err)
	}
	if strings.Contains(string(data), `"turnReasoning"`) {
		t.Fatalf("metrics exposed unavailable reasoning usage: %s", data)
	}
}
