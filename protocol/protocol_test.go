package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestApprovalCloneDoesNotShareRememberedRules(t *testing.T) {
	original := &ApprovalContext{RememberRules: []ApprovalRememberRule{{Tool: "shell", Literal: "echo ok"}}, ApprovedRules: []ApprovalRememberRule{{Tool: "shell", Specifier: "curl *"}}}
	clone := original.Clone()
	clone.RememberRules[0].Literal = "changed"
	clone.ApprovedRules[0].Specifier = "changed"
	if original.RememberRules[0].Literal != "echo ok" {
		t.Fatal("approval preview shares mutable rules")
	}
	if original.ApprovedRules[0].Specifier != "curl *" {
		t.Fatal("approval shares mutable approved rules")
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

func TestApprovalOptionsDescribeAuthority(t *testing.T) {
	for _, tc := range []struct {
		name, tool, scope string
		rules             []ApprovalRememberRule
	}{
		{"session", "shell", "session", []ApprovalRememberRule{{Tool: "shell", Literal: "echo $x", Lifetime: "session"}}},
		{"mixed", "shell", "mixed", []ApprovalRememberRule{{Tool: "shell", Specifier: "go *"}, {Tool: "shell", Literal: "echo $x", Lifetime: "session"}}},
		{"workspace", "workspace", "project", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := &ApprovalContext{Scope: ApprovalScopeProject, RememberRules: tc.rules}
			req := NewApprovalRequest(tc.tool, nil, ConfirmKindPermission, original)
			options := req.Approval.Options
			if len(options) != 3 || options[1].Scope != tc.scope || !options[1].Remember {
				t.Fatalf("options=%+v", options)
			}
			if tc.tool == "workspace" && (options[0].Label != "本次会话允许" || options[0].Remember || options[0].Scope != "session") {
				t.Fatalf("workspace=%+v", options)
			}
			clone := req.Approval.Clone()
			clone.Options[0].Label = "changed"
			if req.Approval.Options[0].Label == "changed" || original.Options != nil {
				t.Fatal("shared options")
			}
		})
	}
	req := NewApprovalRequest("shell", nil, ConfirmKindPermission, &ApprovalContext{Scope: ApprovalScopeOnce})
	if len(req.Approval.Options) != 2 {
		t.Fatal("unexpected remember option")
	}
}
