package taskwire

import (
	"context"
	"testing"

	"nekocode/bot/agent/subagent"
	"nekocode/bot/tools"
)

func TestBuildRunConfigUnknownAgent(t *testing.T) {
	if _, ok := BuildRunConfig(RunConfigInput{AgentTypeName: "missing"}); ok {
		t.Fatal("expected missing agent")
	}
}

func TestBuildRunConfigWiresCallbacks(t *testing.T) {
	subagent.RegisterPlugin(subagent.AgentType{Name: "tester"})
	defer subagent.UnregisterPlugin("tester")

	var eventAction string
	ctx := tools.WithTaskCallback(context.Background(), func(action, toolName, toolArgs, output string) {
		eventAction = action + ":" + toolName + ":" + toolArgs + ":" + output
	})
	var phase string
	cfg, ok := BuildRunConfig(RunConfigInput{
		Context:       ctx,
		AgentTypeName: "tester",
		Prompt:        "do it",
		PhaseFn:       func(p string) { phase = p },
	})
	if !ok {
		t.Fatal("expected config")
	}
	if cfg.Prompt != "do it" || !cfg.DisableThinking {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	cfg.OnToolCall(subagent.ToolCallEvent{Action: "start", ToolName: "read", ToolArgs: "path=a", Output: "ok"})
	if eventAction != "sub_start:read:path=a:ok" {
		t.Fatalf("callback = %q", eventAction)
	}
	cfg.OnPhase("phase")
	if phase != "tester · phase" {
		t.Fatalf("phase = %q", phase)
	}
}
