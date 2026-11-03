package runner

import (
	"context"
	tools "nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/protocol"
	"sync/atomic"
	"testing"
)

func TestPreparePreviewsDetachSharedArgs(t *testing.T) {
	registry := tools.New()
	for _, name := range []string{"first", "second"} {
		registry.RegisterWithOptions(fakeTool{name: name}, tools.RegistrationOptions{
			Preview: func(context.Context, map[string]any) string { return name + " preview" },
		})
	}
	shared := map[string]any{"value": "original"}
	calls := []core.ToolCallItem{{Name: "first", Args: shared}, {Name: "second", Args: shared}}
	NewExecutor(registry).PreparePreviewsContext(t.Context(), calls)
	if calls[0].Args["_preview"] != "first preview" || calls[1].Args["_preview"] != "second preview" {
		t.Fatalf("previews collided: first=%v second=%v", calls[0].Args["_preview"], calls[1].Args["_preview"])
	}
	if shared["_preview"] != nil {
		t.Fatal("preview preparation modified caller-owned arguments")
	}
}

func TestParallelPreviewsDetachSharedArgs(t *testing.T) {
	var entered atomic.Int32
	release := make(chan struct{})
	e := previewRuleExecutor(t, t.TempDir(), fakeTool{name: "shell", mode: core.ModeParallel}, func(context.Context, map[string]any) string {
		if entered.Add(1) == 2 {
			close(release)
		}
		<-release
		return "preview"
	})
	e.SetPermissionMode(permission.ModeAuto)
	e.SetBashAutoJudge(func(context.Context, string) (bool, bool) { return false, true })
	e.SetPreviewDecisionFn(func(string, string, map[string]any, string, protocol.ToolDecision) {})
	shared := map[string]any{"command": "unknown-tool", "_preview": "old preview"}
	results := e.ExecuteBatch(t.Context(), []core.ToolCallItem{{ID: "a", Name: "shell", Args: shared}, {ID: "b", Name: "shell", Args: shared}})
	if shared["_preview"] != "old preview" {
		t.Fatal("workers mutated the caller preview cache")
	}
	for _, result := range results {
		if result.Error != "" {
			t.Fatalf("execution: %+v", result)
		}
	}
}

type mutatingBatchTool struct {
	fakeTool
	mutate func(map[string]any)
}

func (t mutatingBatchTool) Execute(_ context.Context, args map[string]any) (core.ToolOutput, error) {
	t.mutate(args)
	return core.ToolOutput{Text: "ok"}, nil
}

func TestParallelToolsOwnNestedArgumentsAndCallbacks(t *testing.T) {
	var entered, callbacks atomic.Int32
	release := make(chan struct{})
	callback := taskbridge.TaskCallback{ID: "typed", Callback: func(protocol.StepEvent) { callbacks.Add(1) }}
	tool := mutatingBatchTool{fakeTool: fakeTool{name: "mutate", mode: core.ModeParallel}, mutate: func(args map[string]any) {
		if entered.Add(1) == 2 {
			close(release)
		}
		<-release
		args["top"] = "changed"
		args["nested"].(map[string]any)["value"] = "changed"
		args["list"].([]any)[0].(map[string]any)["value"] = "changed"
		args["strings"].([]string)[0] = "changed"
		args["_sub_callback"].(taskbridge.TaskCallback).Callback(protocol.StepEvent{})
	}}
	e := newTestExecutor(fakeRegistry{"mutate": tool})
	e.SetPermissionMode(permission.ModeFull)
	nested := map[string]any{"value": "original"}
	shared := map[string]any{"top": "original", "nested": nested, "list": []any{map[string]any{"value": "original"}}, "strings": []string{"original"}, "_sub_callback": callback}
	results := e.ExecuteBatch(t.Context(), []core.ToolCallItem{{ID: "a", Name: "mutate", Args: shared}, {ID: "b", Name: "mutate", Args: shared}})
	for _, result := range results {
		if result.Error != "" {
			t.Fatalf("execution=%+v", result)
		}
	}
	if shared["top"] != "original" || nested["value"] != "original" || shared["list"].([]any)[0].(map[string]any)["value"] != "original" || shared["strings"].([]string)[0] != "original" {
		t.Fatal("worker mutations escaped into caller containers")
	}
	if callbacks.Load() != 2 {
		t.Fatal("typed callbacks were lost during argument cloning")
	}
}

func TestBatchDetachedEffectiveArgsPreservesNilAndEmptyLists(t *testing.T) {
	e := newTestExecutor(fakeRegistry{"mutate": fakeTool{name: "mutate", mode: core.ModeParallel}})
	effective := map[string]any{"nested": map[string]any{"value": "original"}, "empty": []string{}, "nil": []string(nil)}
	args := map[string]any{"target": effective}
	calls := []core.ToolCallItem{{Name: "mutate", Args: args, EffectiveArgs: effective}, {Name: "mutate", Args: args, EffectiveArgs: effective}}
	parallel, _ := e.partitionCalls(calls)
	parallel[0].call.EffectiveArgs["nested"].(map[string]any)["value"] = "changed"
	parallel[0].call.Args["target"].(map[string]any)["nested"].(map[string]any)["value"] = "changed separately"
	if effective["nested"].(map[string]any)["value"] != "original" || parallel[1].call.EffectiveArgs["nested"].(map[string]any)["value"] != "original" {
		t.Fatal("effective arguments share mutable containers between calls")
	}
	if parallel[0].call.EffectiveArgs["empty"].([]string) == nil || parallel[0].call.EffectiveArgs["nil"].([]string) != nil {
		t.Fatal("cloning changed absent versus empty list semantics")
	}
}
