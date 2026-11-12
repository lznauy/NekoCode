package subagent

import (
	"encoding/json"
	"strings"
	"testing"

	tools "nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/builtin/shell"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/workspace"
	"nekocode/bot/provider/types"
)

func TestEngineWaitsForManagedProcessWithoutInferencePolling(t *testing.T) {
	command := &shell.ShellTool{}
	command.SetSessionID("worker")
	t.Cleanup(func() {
		if err := command.Shutdown(); err != nil {
			t.Error(err)
		}
	})
	// Start one real process before the worker decides to await its outcome.
	// The command outlives shell's initial observation window.
	initial, err := command.ExecuteWithPermission(t.Context(), map[string]any{"command": "sleep 2.2; printf 'final evidence'", "name": "work", "sandbox_mode": "host"}, core.PermissionRequest{Capabilities: []string{core.CapProcessHost}})
	if err != nil {
		t.Fatal(err)
	}
	if initial.Process.State != "running" {
		t.Fatalf("fixture did not leave a managed process: %+v", initial.Process)
	}
	args, _ := json.Marshal(map[string]any{"action": "wait", "task": "work"})
	model := &scriptedLLM{scripts: [][]types.StreamToken{
		{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "await", Name: "process", Arguments: string(args)}}}, completionScript(),
	}}
	registry := tools.New()
	registry.SetWorkspace(workspace.New(t.TempDir(), nil))
	registry.Register(shell.NewProcessTool(command))
	result, err := New(Config{LLM: model, Tools: registry}).Run(t.Context(), RunConfig{SubagentID: "worker", Prompt: "Wait for final evidence.", Profile: Profile{Name: "executor", Tools: []string{"process"}}})
	if err != nil || result.Status != StatusCompleted || model.calls != 2 {
		t.Fatalf("worker made extra inference calls while waiting: calls=%d result=%+v err=%v", model.calls, result, err)
	}
	found := false
	for _, msg := range model.messages[1] {
		if msg.Role == "tool" && msg.ToolCallID == "await" {
			found = strings.Contains(msg.Content, "final evidence") && !strings.Contains(msg.Content, "wait_timeout")
		}
	}
	if !found {
		t.Fatal("worker resumed without terminal evidence")
	}
}
