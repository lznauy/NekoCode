package agent

import (
	"context"
	"testing"

	"nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool"
	"nekocode/bot/policy"
	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

type executionLLM struct {
	contexts []context.Context
	onCall   func()
}

func (*executionLLM) Chat(context.Context, []types.Message, []types.ToolDef) (*types.Response, error) {
	panic("unexpected non-streaming call")
}

func (m *executionLLM) ChatStream(ctx context.Context, _ []types.Message, _ []types.ToolDef) (<-chan types.StreamToken, <-chan error) {
	m.contexts = append(m.contexts, ctx)
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
