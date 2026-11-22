package agent

import (
	"context"
	"encoding/json"
	"testing"

	tools "nekocode/bot/extension/tool"
	tasktool "nekocode/bot/extension/tool/builtin/task"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/bot/policy"
)

func TestBatchPostHooksReceiveSerializableTaskArguments(t *testing.T) {
	gov := policy.New()
	task := tasktool.NewTaskTool()
	a := New(t.Context(), Config{Tools: tools.New(task, &tasktool.BatchTool{}), Policy: gov})
	task.Wire(func(ctx context.Context, spec taskbridge.TaskSpec) (*taskbridge.TaskResult, error) {
		return &taskbridge.TaskResult{Content: "ok"}, nil
	})
	checkArgs := func(args map[string]any) {
		t.Helper()
		if _, err := json.Marshal(args); err != nil {
			t.Errorf("plugin cannot encode task arguments: %v", err)
		}
		if _, ok := args["prompt"]; !ok {
			t.Error("lost public prompt")
		}
	}
	var checked int
	for _, point := range []policy.HookPoint{policy.PostToolUse, policy.PostToolBatch} {
		err := gov.Register(policy.Hook{Name: string(point), Point: point, On: func(s policy.State) *policy.Result {
			facts := s.Facts()
			if facts.Tool.Name == "task" {
				checkArgs(facts.Tool.Args)
				checked++
			}
			for _, result := range facts.Batch {
				if result.Name == "task" {
					checkArgs(result.Args)
					checked++
				}
			}
			return nil
		}})
		if err != nil {
			t.Fatal(err)
		}
	}
	_, results, _ := a.toolRunner.dispatchToolCalls(t.Context(), []core.ToolCallItem{
		{ID: "batch", Name: "task_batch", Args: map[string]any{"profile": "explore", "prompt": "inspect", "items": []taskbridge.BatchItem{{ID: "a", Input: "a"}}}},
		{ID: "direct", Name: "task", Args: map[string]any{"profile": "explore", "prompt": "read"}},
	}, nil)
	if len(results) != 2 || results[0].Error != "" || results[1].Error != "" || checked != 4 {
		t.Fatalf("results=%+v checks=%d", results, checked)
	}
}

func TestBatchPermissionDenialIsBlockedInPostHooks(t *testing.T) {
	gov := policy.New()
	task := tasktool.NewTaskTool()
	a := New(t.Context(), Config{Tools: tools.New(task, &tasktool.BatchTool{}), Policy: gov})
	a.deps.toolExecutor.SetPermissionPolicy(permission.PermissionsDecl{Deny: []string{"task"}}, t.TempDir(), t.TempDir())
	var after, blocked int
	for _, point := range []policy.HookPoint{policy.PostToolUse, policy.PostToolBatch} {
		if err := gov.Register(policy.Hook{Name: string(point), Point: point, On: func(s policy.State) *policy.Result {
			facts := s.Facts()
			if facts.Tool.Name == "task" {
				after++
			}
			for _, r := range facts.Batch {
				if r.Name == "task" && r.Blocked && r.BlockReason != "" {
					blocked++
				}
			}
			return nil
		}}); err != nil {
			t.Fatal(err)
		}
	}
	_, results, _ := a.toolRunner.dispatchToolCalls(t.Context(), []core.ToolCallItem{{ID: "batch", Name: "task_batch", Args: map[string]any{"profile": "explore", "prompt": "inspect", "items": []taskbridge.BatchItem{{ID: "a", Input: "doc"}}}}}, nil)
	if after != 0 || blocked != 1 || results[0].Error != "" {
		t.Fatalf("post_tool_use=%d blocked=%d result=%+v", after, blocked, results[0])
	}
}
