package agent

import (
	"context"
	"strings"
	"testing"

	"nekocode/bot/agent/internal/kernel"
	"nekocode/bot/checkpoint"
	"nekocode/bot/contextmgr"
	"nekocode/bot/policy"
	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

func messagesContain(msgs []types.Message, substr string) bool {
	for _, msg := range msgs {
		if strings.Contains(msg.Content, substr) {
			return true
		}
	}
	return false
}

func TestRestoreSessionUsageClearsPreviousRunMetrics(t *testing.T) {
	a := newTestAgent()
	a.deps.ctxMgr.Add("user", "previous task")
	a.life.Start()
	a.AddCompletionTokens(50)
	a.RecordLLMUsage(types.StreamUsage{PromptTokens: 20, CompletionTokens: 50})
	a.RestoreSessionUsage(17)
	got := a.Metrics()
	if got.Duration != "" || got.CompletionTokens != 17 || got.TurnCompletion != 0 || got.TurnPrompt != 0 || got.TurnTotal != 0 || got.TurnOutput != 0 {
		t.Fatalf("previous run metrics leaked into restored session: %+v", got)
	}
	a.AddCompletionTokens(3)
	a.RecordLLMUsage(types.StreamUsage{PromptTokens: 8, CompletionTokens: 3})
	got = a.Metrics()
	if got.CompletionTokens != 20 || got.TurnCompletion != 3 || got.TurnTotal != 11 {
		t.Fatalf("new usage was not counted from the restored baseline: %+v", got)
	}
}

func TestDrainSteeringRotatesCheckpointMessage(t *testing.T) {
	cp := checkpoint.New(t.TempDir())
	cp.Activate("session", nil, 0)
	if _, err := cp.BeginMessage("session", "Initial request"); err != nil {
		t.Fatal(err)
	}
	ctxManager := contextmgr.New(contextmgr.Config{})
	a := &Agent{
		life: kernel.NewLifecycle(context.Background(), steeringChBuffer),
		deps: agentDeps{ctxMgr: ctxManager, checkpoints: cp},
	}
	if err := a.TrySteer("Updated direction"); err != nil {
		t.Fatal(err)
	}
	if err := a.drainSteering(); err != nil {
		t.Fatal(err)
	}
	if err := cp.Finish("session"); err != nil {
		t.Fatal(err)
	}
	history, err := cp.History("session")
	if err != nil || len(history) != 2 || history[0].UserMessage != "Updated direction" || history[1].UserMessage != "Initial request" {
		t.Fatalf("steering checkpoint history = %+v err=%v", history, err)
	}
}

func TestSteerRejectsFullQueueWithoutCancelingCurrentContext(t *testing.T) {
	a := &Agent{life: kernel.NewLifecycle(context.Background(), steeringChBuffer)}
	for n := 0; n < steeringChBuffer; n++ {
		if err := a.TrySteer("queued"); err != nil {
			t.Fatalf("steer %d: %v", n, err)
		}
	}
	current := a.getCtx()
	if err := a.TrySteer("overflow"); err == nil {
		t.Fatal("full steering queue was accepted")
	}
	if err := current.Err(); err != nil {
		t.Fatalf("rejected steering canceled the current context: %v", err)
	}
}

func TestSteerKeepsLegacyMethodSignature(t *testing.T) {
	a := &Agent{life: kernel.NewLifecycle(context.Background(), steeringChBuffer)}
	var steer func(string) = a.Steer
	steer("legacy caller")
	select {
	case got := <-a.life.Steering():
		if got != "legacy caller" {
			t.Fatalf("steering message = %q", got)
		}
	default:
		t.Fatal("legacy Steer wrapper did not enqueue the message")
	}
}

func TestBindInteractionRestoresPreviousCallbacks(t *testing.T) {
	originalCalls := 0
	activeCalls := 0
	a := newTestAgent()
	a.interaction = Interaction{
		Confirm: func(protocol.ConfirmRequest) protocol.ConfirmReply {
			originalCalls++
			return protocol.AllowOnce()
		},
	}

	release := a.BindInteraction(Interaction{
		Confirm: func(protocol.ConfirmRequest) protocol.ConfirmReply {
			activeCalls++
			return protocol.Deny()
		},
	})
	if reply := a.confirm(protocol.ConfirmRequest{}); reply.Allowed {
		t.Fatal("bound interaction did not replace the original callback")
	}
	release()
	if reply := a.confirm(protocol.ConfirmRequest{}); !reply.Allowed {
		t.Fatal("release did not restore the original callback")
	}
	if activeCalls != 1 || originalCalls != 1 {
		t.Fatalf("callback counts = active %d, original %d", activeCalls, originalCalls)
	}
}

func TestBindOutputRestoresPreviousCallbacks(t *testing.T) {
	a := newTestAgent()
	originalCalls := 0
	activeCalls := 0
	a.setOutput(Output{Text: func(string) { originalCalls++ }})

	release := a.BindOutput(Output{Text: func(string) { activeCalls++ }})
	a.stream.emitText("active")
	release()
	a.stream.emitText("original")
	if activeCalls != 1 || originalCalls != 1 {
		t.Fatalf("callback counts = active %d, original %d", activeCalls, originalCalls)
	}
}

func TestInjectHintAppendsTaggedUserContext(t *testing.T) {
	a := newTestAgent()
	before := a.deps.ctxMgr.Len()

	a.injectHint(&policy.Hint{Type: "final_check", Severity: "critical", Content: "run verification"})
	if got := a.deps.ctxMgr.Len(); got != before {
		t.Fatalf("hint changed history length: got %d, want %d", got, before)
	}

	a.applyTurnHints(nil)
	msgs := a.deps.ctxMgr.BuildRequest(contextmgr.ModelRequest{})
	if !messagesContain(msgs, `type="final_check"`) || !messagesContain(msgs, "run verification") {
		t.Fatalf("expected tagged final_check hint in build messages, got %+v", msgs)
	}
	if tail := msgs[len(msgs)-1]; tail.Role != "user" || tail.Source != types.MessageSourceHint {
		t.Fatalf("hint must be an internal user context message: %+v", tail)
	}

	a.deps.ctxMgr.SetHints("")
	a.deps.ctxMgr.BuildRequest(contextmgr.ModelRequest{})
	if got := a.deps.ctxMgr.Len(); got != before+1 {
		t.Fatalf("clearing hint appended context: history length = %d, want %d", got, before+1)
	}
}
