package subagent

import (
	"strings"
	"testing"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/runtime/runner"
	"nekocode/bot/policy"
	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

func TestExecutionIdentitySeparatesAttemptsAndPreservesParent(t *testing.T) {
	model := &scriptedLLM{scripts: [][]types.StreamToken{completionScript(), completionScript(), completionScript()}}
	engine := New(Config{LLM: model, Tools: tools.New()})
	parent := protocol.ExecutionIdentity{RunID: "parent-run", TaskID: "parent-task", AttemptID: "parent-attempt"}
	ctx := protocol.WithExecutionIdentity(t.Context(), parent)
	cfg := RunConfig{SubagentID: "task", Prompt: "check", Profile: Profile{Name: "explore"}}
	for _, id := range []string{"first-attempt", "second-attempt"} {
		cfg.AttemptID = id
		if _, err := engine.Run(ctx, cfg); err != nil {
			t.Fatal(err)
		}
	}
	first := protocol.ExecutionFromContext(model.contexts[0])
	second := protocol.ExecutionFromContext(model.contexts[1])
	if first.RunID != parent.RunID || second.RunID != parent.RunID || first.TaskID != cfg.SubagentID || first.AttemptID == "" || second.AttemptID == "" || first.AttemptID == second.AttemptID {
		t.Fatalf("child attempts reused or lost identity: %+v / %+v", first, second)
	}
	if protocol.ExecutionFromContext(ctx) != parent {
		t.Fatal("child mutated its parent's execution scope")
	}
	cfg.AttemptID = ""
	if _, err := engine.Run(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	if got := protocol.ExecutionFromContext(model.contexts[2]); got != (protocol.ExecutionIdentity{RunID: parent.RunID, TaskID: cfg.SubagentID}) {
		t.Fatalf("engine invented or inherited a durable attempt: %+v", got)
	}
}

func TestSubagentPolicyDiagnosticUsesGuardNotParent(t *testing.T) {
	model := &scriptedLLM{scripts: [][]types.StreamToken{completionScript()}}
	engine := New(Config{LLM: model, Tools: tools.New()})
	parent := policy.New()
	parent.Register(policy.Hook{Name: "custom-parent", Point: policy.PreModel, On: func(policy.State) *policy.Result { return nil }})
	ctx := protocol.WithPolicyConfiguration(t.Context(), parent.Configuration())
	if _, err := engine.Run(ctx, RunConfig{Prompt: "check", Profile: Profile{Name: "explore"}, Policy: parent}); err != nil {
		t.Fatal(err)
	}
	got := protocol.PolicyConfigurationFromContext(model.contexts[0])
	expected := newRunGuard().Configuration()
	if got != expected || got.Hash == parent.Configuration().Hash {
		t.Fatalf("subagent diagnostic inherited parent policy: %+v", got)
	}
}

func TestSubagentBindsActualProfileAndSkillRequest(t *testing.T) {
	model := &scriptedLLM{scripts: [][]types.StreamToken{completionScript(), completionScript()}}
	engine := New(Config{LLM: model, Tools: tools.New()})
	cfg := RunConfig{Prompt: "check", Profile: Profile{Name: "explore", Skills: []string{"selected"}}, SkillContents: []string{"workflow-sentinel"}}
	parent := protocol.WithProfileConfiguration(t.Context(), "parent-profile")
	for _, limit := range []int{1, 2} {
		cfg.Profile.MaxSteps = limit
		if _, err := engine.Run(parent, cfg); err != nil {
			t.Fatal(err)
		}
		ctx := model.contexts[len(model.contexts)-1]
		if protocol.ProfileConfigurationFromContext(ctx) != cfg.Profile.ConfigurationHash() {
			t.Fatal("subagent lost actual profile declaration")
		}
		present := false
		for _, msg := range model.messages[len(model.messages)-1] {
			if strings.Contains(msg.Content, "workflow-sentinel") {
				present = true
				if msg.Role != "user" || msg.Source != types.MessageSourceSkillWorkflow {
					t.Fatal("workflow diagnostic provenance lost")
				}
			}
		}
		if !present {
			t.Fatal("workflow missing from actual model request")
		}
	}
	if protocol.ProfileConfigurationFromContext(model.contexts[0]) == protocol.ProfileConfigurationFromContext(model.contexts[1]) {
		t.Fatal("profile limit change invisible")
	}
	if protocol.ProfileConfigurationFromContext(parent) != "parent-profile" {
		t.Fatal("child mutated parent diagnostic")
	}
}

func TestSubagentSamplesItsOwnAccess(t *testing.T) {
	model := &scriptedLLM{scripts: [][]types.StreamToken{completionScript()}}
	engine := New(Config{LLM: model, Tools: tools.New()})
	parent := protocol.WithAccessObservation(t.Context(), protocol.AccessObservation{Schema: 99, Mode: "parent"})
	cfg := RunConfig{Prompt: "check", Profile: Profile{Name: "explore"}, ConfigurePermissions: func(executor *runner.Executor) { executor.SetFullAccess(true) }}
	if _, err := engine.Run(parent, cfg); err != nil {
		t.Fatal(err)
	}
	got := protocol.AccessObservationFromContext(model.contexts[0])
	if got.Schema != 1 || got.Mode != "full" || got.WorkspaceHash == "" {
		t.Fatalf("child access missing: %+v", got)
	}
	if protocol.AccessObservationFromContext(parent).Mode != "parent" {
		t.Fatal("child mutated parent observation")
	}
}

func TestDelegatedSkillAdmissionsAreRecordedOncePerPreparedAttempt(t *testing.T) {
	model := &scriptedLLM{scripts: [][]types.StreamToken{{{Content: "progress"}}, completionScript(), {{Content: "progress"}}, completionScript()}}
	engine := New(Config{LLM: model, Tools: tools.New()})
	var latest ctxmgr.ManagerSnapshot
	var admissions []types.SkillDelivery
	cfg := RunConfig{SubagentID: "task", Prompt: "inspect", Profile: Profile{Name: "explore"}, SkillNames: []string{"resolved-review"}, SkillContents: []string{"workflow body"}, SaveContext: func(snap ctxmgr.ManagerSnapshot, _, _ string, _ AttemptUsage) error {
		latest = snap
		for _, m := range snap.Transcript {
			if m.SkillDelivery.Schema != 0 {
				admissions = append(admissions, m.SkillDelivery)
				if m.Content != "" {
					t.Fatal("workflow body duplicated in admission event")
				}
			}
		}
		return nil
	}}
	for _, attempt := range []string{"one", "two"} {
		cfg.AttemptID = attempt
		admissions = nil
		if _, err := engine.Run(t.Context(), cfg); err != nil {
			t.Fatal(err)
		}
		if len(admissions) != 1 {
			t.Fatalf("duplicate/missing admission for %s: %d", attempt, len(admissions))
		}
		d := admissions[0]
		if d.Name != "resolved-review" || d.Source != "delegated" || d.ContentHash != types.SkillContentHash("workflow body") {
			t.Fatalf("invalid delegated admission: %+v", d)
		}
		for _, m := range latest.Messages {
			if m.SkillDelivery.Schema != 0 {
				t.Fatal("admission-only record entered model context")
			}
		}
	}
	model = &scriptedLLM{}
	engine = New(Config{LLM: model, Tools: tools.New()})
	admissions = nil
	cfg.ContextWindow = 4000
	cfg.SkillContents = []string{strings.Repeat("x", 50000)}
	if _, err := engine.Run(t.Context(), cfg); err == nil {
		t.Fatal("oversized workflow accepted")
	}
	if len(admissions) != 0 {
		t.Fatal("failed request fabricated workflow admission")
	}
	if model.calls != 0 {
		t.Fatal("oversized request reached provider")
	}
	cfg.SkillNames = []string{"one", "two"}
	if _, err := engine.Run(t.Context(), cfg); err == nil {
		t.Fatal("mismatched resolved names accepted")
	}
}

func TestSubagentSavesReceiptWithOwnAttemptIdentity(t *testing.T) {
	registry := tools.New(testProxyTool{name: "inspect"})
	model := &scriptedLLM{scripts: [][]types.StreamToken{
		{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "same-call", Name: "inspect", Arguments: `{}`}}}, completionScript(),
	}}
	var evidence, observed protocol.ToolEvidence
	cfg := RunConfig{SessionID: "parent", SubagentID: "child", AttemptID: "child-attempt", Profile: Profile{Name: "custom", Tools: []string{"inspect"}}, OnToolCall: func(event ToolCallEvent) {
		if event.Action == protocol.StepActionExecuteTool {
			observed = event.Evidence.Clone()
			event.Evidence.Executions[0].ID = "observer-mutated"
		}
	}, SaveContext: func(snapshot ctxmgr.ManagerSnapshot, _, _ string, _ AttemptUsage) error {
		for _, msg := range snapshot.Transcript {
			if msg.ToolCallID == "same-call" {
				evidence = msg.ToolEvidence
			}
		}
		return nil
	}}
	result, err := New(Config{LLM: model, Tools: registry}).Run(protocol.WithExecutionIdentity(t.Context(), protocol.ExecutionIdentity{RunID: "parent-run", AttemptID: "parent-attempt"}), cfg)
	if err != nil || result.Status != StatusCompleted {
		t.Fatalf("%+v %v", result, err)
	}
	if len(evidence.Executions) != 1 || evidence.Executions[0].Execution != (protocol.ExecutionIdentity{RunID: "parent-run", TaskID: "child", AttemptID: "child-attempt"}) {
		t.Fatalf("wrong child evidence: %+v", evidence)
	}
	if len(observed.Executions) != 1 || observed.Executions[0].ID != evidence.Executions[0].ID || observed.ReturnedHash != evidence.ReturnedHash || observed.Admission != "" || evidence.Admission == "" {
		t.Fatalf("delivery/admission boundary lost: observed=%+v transcript=%+v", observed, evidence)
	}
	for _, messages := range model.messages {
		for _, msg := range messages {
			if len(msg.ToolEvidence.Executions) > 0 {
				t.Fatal("evidence body entered child model request")
			}
		}
	}
}
