package subagent

import (
	"encoding/json"
	ctxmgr "nekocode/bot/contextmgr"
	tools "nekocode/bot/extension/tool"
	"nekocode/bot/provider/types"
	"strings"
	"testing"

	"nekocode/bot/extension/tool/runtime/core"
)

func verificationArgs(status string) map[string]any {
	return map[string]any{"name": "race probe", "status": status, "evidence": "go test -run Probe: exit 0", "entrypoint": "direct helper invocation", "scope": "one synthetic Manager; does not exercise separate task contexts", "limitations": []any{"production reachability not verified"}}
}

func TestHandoffVerificationPreservesOutcomesScopeAndLimitations(t *testing.T) {
	args := handoffArgs()
	checks := []any{}
	for _, status := range []string{"passed", "failed", "blocked", "not_run"} {
		checks = append(checks, verificationArgs(status))
	}
	args["verification_checks"] = checks
	handoff, _, err := submittedResult([]core.ToolCallItem{{Name: submitResultToolName, Args: args}})
	if err != nil {
		t.Fatal(err)
	}
	result := buildHandoffResult(handoff, runMeta{})
	for _, want := range []string{"passed=1; failed=1; blocked=1; not_run=1", "direct helper invocation", "one synthetic Manager", "production reachability not verified", "not proof of full validation"} {
		if !strings.Contains(result.Content, want) {
			t.Fatalf("missing %q in %s", want, result.Content)
		}
	}
	handoff.VerificationChecks[0].Limitations[0] = "changed"
	handoff.VerificationChecks[0].Scope = "changed"
	if result.Handoff.VerificationChecks[0].Limitations[0] == "changed" || result.Handoff.VerificationChecks[0].Scope == "changed" {
		t.Fatal("handoff shares mutable verification state")
	}
}

func TestHandoffVerificationRejectsIncompleteOrInvalidChecks(t *testing.T) {
	for _, field := range []string{"name", "status", "evidence", "entrypoint", "scope", "limitations"} {
		t.Run(field, func(t *testing.T) {
			check := verificationArgs("passed")
			delete(check, field)
			args := handoffArgs()
			args["verification_checks"] = []any{check}
			if _, _, err := submittedResult([]core.ToolCallItem{{Name: submitResultToolName, Args: args}}); err == nil {
				t.Fatal("accepted missing field")
			}
		})
	}
	for _, value := range []any{nil, "passed", []any{verificationArgs("confirmed")}, []any{"check"}, make([]any, maxVerificationChecks+1)} {
		args := handoffArgs()
		args["verification_checks"] = value
		if _, _, err := submittedResult([]core.ToolCallItem{{Name: submitResultToolName, Args: args}}); err == nil {
			t.Fatalf("accepted invalid checks: %v", value)
		}
	}
	check := verificationArgs("passed")
	check["evidence"] = strings.Repeat("x", 4097)
	args := handoffArgs()
	args["verification_checks"] = []any{check}
	if _, _, err := submittedResult([]core.ToolCallItem{{Name: submitResultToolName, Args: args}}); err == nil {
		t.Fatal("accepted unbounded evidence")
	}
}

func TestLegacyHandoffDoesNotImplyValidationPassed(t *testing.T) {
	handoff, _, err := submittedResult([]core.ToolCallItem{{Name: submitResultToolName, Args: handoffArgs()}})
	if err != nil {
		t.Fatal(err)
	}
	text := buildHandoffResult(handoff, runMeta{}).Content
	if !strings.Contains(text, "Not reported. Do not infer verification passed") {
		t.Fatal("legacy completion implies clean validation")
	}
}

func TestEnginePersistsVerificationGapsWithoutFailingHandoff(t *testing.T) {
	args := handoffArgs()
	args["verification_checks"] = []any{verificationArgs("blocked")}
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	model := &scriptedLLM{scripts: [][]types.StreamToken{{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "handoff", Name: submitResultToolName, Arguments: string(raw)}}}}}
	saved := ""
	result, err := New(Config{LLM: model, Tools: tools.New()}).Run(t.Context(), RunConfig{Prompt: "review", Profile: Profile{Name: "executor"}, SaveContext: func(_ ctxmgr.ManagerSnapshot, status, output string, _ AttemptUsage) error {
		if status == "completed" {
			saved = output
		}
		return nil
	}})
	if err != nil || result.Status != StatusCompleted {
		t.Fatalf("blocked verification confused with failed execution: %+v %v", result, err)
	}
	if saved != result.Content || !strings.Contains(saved, "blocked=1") || !strings.Contains(saved, "production reachability not verified") {
		t.Fatalf("durable handoff lost verification gaps: %s", saved)
	}
}
