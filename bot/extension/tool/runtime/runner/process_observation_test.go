package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/runtime/core"
)

type processFactTool struct {
	fakeTool
	fail       bool
	privileged bool
}

func (t processFactTool) Execute(context.Context, map[string]any) (core.ToolOutput, error) {
	if t.privileged {
		return core.ToolOutput{}, core.RequiredPermissionError{Request: core.PermissionRequest{Capabilities: []string{core.CapProcessHost}}}
	}
	return t.result()
}
func (t processFactTool) ExecuteWithPermission(context.Context, map[string]any, core.PermissionRequest) (core.ToolOutput, error) {
	return t.result()
}
func (t processFactTool) result() (core.ToolOutput, error) {
	out := core.ToolOutput{Text: "\x1b[31mresult\x1b[0m", Process: core.ProcessObservation{State: "exited", ExitCode: 7, ExitCodeKnown: true, OutputComplete: true}}
	if t.output != "" {
		out.Text = t.output
	}
	if t.fail {
		return out, errors.New("tool delivery failed")
	}
	return out, nil
}

func TestExecutorPreservesProcessFactAcrossErrorAndPrivilegedRetry(t *testing.T) {
	for _, tc := range []struct {
		name             string
		fail, privileged bool
	}{{"returned", false, false}, {"error", true, false}, {"retry", false, true}, {"retry error", true, true}} {
		t.Run(tc.name, func(t *testing.T) {
			tool := processFactTool{fakeTool: fakeTool{name: "fact"}, fail: tc.fail, privileged: tc.privileged}
			registry := tools.New()
			registry.RegisterWithOptions(tool, tools.RegistrationOptions{Privileged: tool.ExecuteWithPermission})
			executor := NewExecutor(registry)
			executor.SetFullAccess(true)
			result := executor.ExecuteBatch(t.Context(), []core.ToolCallItem{{ID: "call", Name: "fact"}})[0]
			if result.Process != (core.ProcessObservation{State: "exited", ExitCode: 7, ExitCodeKnown: true, OutputComplete: true}) || result.TextOutput().Process != result.Process {
				t.Fatalf("lost fact: %+v", result)
			}
			if strings.Contains(result.Output, "\x1b") || strings.Contains(result.Error, "\x1b") || (result.Error != "") != tc.fail {
				t.Fatalf("delivery changed: %+v", result)
			}
		})
	}
}

func TestToolsWithoutProcessFactsRemainUnknown(t *testing.T) {
	e := newTestExecutor(fakeRegistry{"plain": fakeTool{name: "plain"}})
	result := e.ExecuteBatch(t.Context(), []core.ToolCallItem{{Name: "plain"}})[0]
	if result.Process != (core.ProcessObservation{}) {
		t.Fatal("nil tool error invented process success")
	}
}

func TestExecutorPreservesExitFactWhenOutputIsClippedOrCannotBeSaved(t *testing.T) {
	for _, failSave := range []bool{false, true} {
		tool := processFactTool{fakeTool: fakeTool{name: "fact", output: strings.Repeat("output", core.MaxOutputBytes)}}
		executor := newTestExecutor(fakeRegistry{"fact": tool})
		if failSave {
			path := filepath.Join(t.TempDir(), "not-directory")
			if err := os.WriteFile(path, []byte("file"), 0600); err != nil {
				t.Fatal(err)
			}
			executor.ExecutionState().OutputStore = core.NewOutputStore(path)
		}
		result := executor.ExecuteBatch(t.Context(), []core.ToolCallItem{{Name: "fact"}})[0]
		if result.Process.OutputComplete || result.Process.State != "exited" || !result.Process.ExitCodeKnown || result.Process.ExitCode != 7 {
			t.Fatalf("lost process fact: %+v", result.Process)
		}
		if (result.Error != "") != failSave {
			t.Fatalf("unexpected save outcome: %+v", result)
		}
	}
}
