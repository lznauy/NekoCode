package agent

import (
	"context"
	"strings"
	"testing"

	"nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/refbridge"
	"nekocode/bot/policy"
	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

type executionLLM struct {
	contexts []context.Context
	messages [][]types.Message
	onCall   func()
}

func (*executionLLM) Chat(context.Context, []types.Message, []types.ToolDef) (*types.Response, error) {
	panic("unexpected non-streaming call")
}

func (m *executionLLM) ChatStream(ctx context.Context, messages []types.Message, _ []types.ToolDef) (<-chan types.StreamToken, <-chan error) {
	m.contexts = append(m.contexts, ctx)
	m.messages = append(m.messages, append([]types.Message(nil), messages...))
	if m.onCall != nil {
		m.onCall()
	}
	tokens := make(chan types.StreamToken, 1)
	tokens <- types.StreamToken{Content: "answer"}
	close(tokens)
	errs := make(chan error)
	close(errs)
	return tokens, errs
}

func TestExecutionIdentitySurvivesSteeringAndDoesNotLeakToNextRun(t *testing.T) {
	model := &executionLLM{}
	a := New(t.Context(), Config{Context: contextmgr.New(contextmgr.Config{}), Model: model, Tools: tools.New()})
	identity := protocol.ExecutionIdentity{RunID: "runtime-run"}
	model.onCall = func() {
		model.onCall = nil
		if err := a.TrySteer("continue with this correction"); err != nil {
			t.Fatal(err)
		}
		if protocol.ExecutionFromContext(a.getCtx()) != identity {
			t.Fatal("steering lost the active execution identity")
		}
	}
	if result := a.RunContext(protocol.WithExecutionIdentity(t.Context(), identity), "question", nil, nil); result.Error != nil {
		t.Fatal(result.Error)
	}
	before := len(model.contexts)
	for range 2 {
		if result := a.Run("next question", nil, nil); result.Error != nil {
			t.Fatal(result.Error)
		}
	}
	first := protocol.ExecutionFromContext(model.contexts[before])
	last := protocol.ExecutionFromContext(model.contexts[len(model.contexts)-1])
	if first.RunID == "" || last.RunID == "" || first.RunID == identity.RunID || first.RunID == last.RunID {
		t.Fatalf("direct runs reused execution identity: %+v / %+v", first, last)
	}
	for _, ctx := range model.contexts[:before] {
		if protocol.ExecutionFromContext(ctx) != identity {
			t.Fatal("model call diverged from runtime execution identity")
		}
	}
}

func TestModelObservesCurrentPolicyConfiguration(t *testing.T) {
	gov := policy.New()
	hook := policy.Hook{Name: "configured", Point: policy.PreModel, BehaviorVersion: "1", On: func(policy.State) *policy.Result { return nil }}
	gov.Register(hook)
	model := &executionLLM{}
	a := New(t.Context(), Config{Context: contextmgr.New(contextmgr.Config{}), Model: model, Tools: tools.New(), Policy: gov})
	for _, version := range []string{"1", "2"} {
		hook.BehaviorVersion = version
		gov.Register(hook)
		model.onCall = func() {
			got := protocol.PolicyConfigurationFromContext(model.contexts[len(model.contexts)-1])
			if got != gov.Configuration() {
				t.Fatalf("model saw stale policy: %+v", got)
			}
		}
		if result := a.Run("question", nil, nil); result.Error != nil {
			t.Fatal(result.Error)
		}
	}
	if protocol.PolicyConfigurationFromContext(model.contexts[0]).Hash == protocol.PolicyConfigurationFromContext(model.contexts[len(model.contexts)-1]).Hash {
		t.Fatal("policy change was invisible across runs")
	}
}

func TestMainAgentDoesNotInheritParentProfileDiagnostic(t *testing.T) {
	parent := protocol.WithProfileConfiguration(t.Context(), "parent-profile-hash")
	model := &executionLLM{}
	a := New(parent, Config{Context: contextmgr.New(contextmgr.Config{}), Model: model, Tools: tools.New()})
	if result := a.Run("independent main request", nil, nil); result.Error != nil {
		t.Fatal(result.Error)
	}
	if got := protocol.ProfileConfigurationFromContext(model.contexts[0]); got != "" {
		t.Fatalf("main model inherited unrelated profile: %q", got)
	}
	if _, err := a.modelRunner.streamSynthesize(parent); err != nil {
		t.Fatal(err)
	}
	if got := protocol.ProfileConfigurationFromContext(model.contexts[len(model.contexts)-1]); got != "" {
		t.Fatalf("main synthesis inherited unrelated profile: %q", got)
	}
	if protocol.ProfileConfigurationFromContext(parent) != "parent-profile-hash" {
		t.Fatal("main request mutated parent context")
	}
}

func TestModelSamplesCurrentAccessInsteadOfInheritedObservation(t *testing.T) {
	model := &executionLLM{}
	parent := protocol.WithAccessObservation(t.Context(), protocol.AccessObservation{Schema: 99, Mode: "parent"})
	a := New(parent, Config{Context: contextmgr.New(contextmgr.Config{}), Model: model, Tools: tools.New()})
	var samples []protocol.AccessObservation
	for _, full := range []bool{false, true} {
		a.Executor().SetFullAccess(full)
		if result := a.Run("question", nil, nil); result.Error != nil {
			t.Fatal(result.Error)
		}
		got := protocol.AccessObservationFromContext(model.contexts[len(model.contexts)-1])
		if got != a.Executor().AccessObservation() {
			t.Fatalf("model saw stale access: %+v", got)
		}
		samples = append(samples, got)
	}
	if samples[0].Mode != "manual" || samples[1].Mode != "full" {
		t.Fatalf("mode changes missing: %+v", samples)
	}
	if protocol.AccessObservationFromContext(model.contexts[0]) != samples[0] {
		t.Fatal("later request changed earlier observation")
	}
	if protocol.AccessObservationFromContext(parent).Mode != "parent" {
		t.Fatal("parent context changed")
	}
}

func TestModelRefreshesRequestOnlyTaskReferences(t *testing.T) {
	model := &executionLLM{}
	manager := contextmgr.New(contextmgr.Config{})
	current := "first-attempt"
	calls := 0
	a := New(t.Context(), Config{Context: manager, Model: model, Tools: tools.New(), SessionID: func() string { return "parent" }, TaskReferences: func(ctx context.Context, sessionID string) contextmgr.TaskReferences {
		calls++
		if sessionID != "parent" || ctx.Err() != nil {
			t.Fatalf("wrong reference scope: %q", sessionID)
		}
		return contextmgr.TaskReferences{SessionID: sessionID, Total: 1, Items: []contextmgr.TaskReference{{ID: "task-ref", AttemptID: current, Status: "running"}}}
	}})
	for _, attempt := range []string{"first-attempt", "resumed-attempt"} {
		current = attempt
		if result := a.Run("continue", nil, nil); result.Error != nil {
			t.Fatal(result.Error)
		}
		found := false
		for _, message := range model.messages[len(model.messages)-1] {
			if strings.Contains(message.Content, "task_references") {
				found = true
				if !strings.Contains(message.Content, attempt) || message.Role != "user" || message.IsConversationUser() {
					t.Fatalf("incorrect task reference: %+v", message)
				}
			}
		}
		if !found {
			t.Fatal("actual model request omitted task refs")
		}
	}
	if calls != 2 || len(model.messages) != 2 {
		t.Fatalf("extra sampling/model calls: %d/%d", calls, len(model.messages))
	}
	if _, err := a.modelRunner.streamSynthesize(t.Context()); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("synthesis repeated the same metadata scan")
	}
	a.toolRunner.executeAllowedTools([]core.ToolCallItem{{Name: "task_list", ID: "query"}}, nil)
	if _, err := a.modelRunner.streamSynthesize(t.Context()); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatal("task query did not invalidate request projection")
	}
	for _, message := range manager.Snapshot().Transcript {
		if strings.Contains(message.Content, "task_references") {
			t.Fatal("request refs persisted in transcript")
		}
	}
}

func TestModelAndSynthesisReceiveFreshContextReferences(t *testing.T) {
	model := &executionLLM{}
	current := "first-reference"
	a := New(t.Context(), Config{Context: contextmgr.New(contextmgr.Config{}), Model: model, Tools: tools.New(), SessionID: func() string { return "parent" }, ContextReferences: func(ctx context.Context, scope string) refbridge.Page {
		if scope != "parent" || ctx.Err() != nil {
			t.Fatal("wrong reference scope")
		}
		return refbridge.Page{SessionID: scope, Items: []refbridge.Reference{{ID: current, Sequence: 2}}}
	}})
	if result := a.Run("question", nil, nil); result.Error != nil {
		t.Fatal(result.Error)
	}
	current = "new-reference"
	if _, err := a.modelRunner.streamSynthesize(t.Context()); err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"first-reference", "new-reference"} {
		found := false
		for _, msg := range model.messages[i] {
			if strings.Contains(msg.Content, id) {
				found = true
				if msg.Role != "user" || msg.IsConversationUser() {
					t.Fatal("reference acquired authority")
				}
			}
		}
		if !found {
			t.Fatalf("model request %d omitted current references", i)
		}
	}
}
