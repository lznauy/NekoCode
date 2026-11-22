package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"nekocode/bot/contextmgr"
	tools "nekocode/bot/extension/tool"
	tasktool "nekocode/bot/extension/tool/builtin/task"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

func TestTaskBatchUsesConfiguredPoolAndPreservesEachOutcome(t *testing.T) {
	task := tasktool.NewTaskTool()
	reg := tools.New(task, &tasktool.BatchTool{})
	a := New(t.Context(), Config{Tools: reg, MaxConcurrentSubagents: 4})
	var active, peak, count atomic.Int32
	started := make(chan struct{})
	var ready sync.Once
	task.Wire(func(ctx context.Context, spec taskbridge.TaskSpec) (*taskbridge.TaskResult, error) {
		count.Add(1)
		n := active.Add(1)
		defer active.Add(-1)
		for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
		}
		if spec.Title == "ordinary" {
			return &taskbridge.TaskResult{}, nil
		}
		if n == 4 {
			ready.Do(func() { close(started) })
		}
		select {
		case <-started:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if !strings.Contains(spec.Prompt, "shared criteria") || !strings.Contains(spec.Prompt, "repo-"+spec.Title) {
			return nil, fmt.Errorf("wrong prompt")
		}
		// Real foreground workers publish the persisted attempt when they start.
		cb, _ := taskbridge.TaskCallbackFromCtx(ctx)
		cb(protocol.StepEvent{Action: protocol.StepActionSubAgentStart, SubAgentAttemptID: "attempt-" + spec.Title, SubAgentAttemptSeq: 1})
		res := &taskbridge.TaskResult{ID: taskbridge.TaskIDFromCtx(ctx), Content: "finished"}
		switch spec.Title {
		case "3":
			return nil, fmt.Errorf("unavailable repository")
		case "4":
			res.Status = taskbridge.TaskStatusPartial
		}
		return res, nil
	})
	var items []map[string]string
	for i := range 50 {
		id := fmt.Sprint(i)
		items = append(items, map[string]string{"id": id, "input": "repo-" + id})
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, results, _ := a.toolRunner.dispatchToolCalls(ctx, []core.ToolCallItem{
		{ID: "batch", Name: "task_batch", Args: map[string]any{"items": items, "profile": "explore", "prompt": "shared criteria"}},
		{ID: "ordinary", Name: "task", Args: map[string]any{"title": "ordinary", "prompt": "read", "profile": "explore"}},
	}, nil)
	var rows []batchItemResult
	if len(results) != 2 || results[0].ID != "batch" || results[0].Error != "" || results[1].Error != "" {
		t.Fatalf("results=%+v", results)
	}
	if err := json.Unmarshal([]byte(results[0].Output), &rows); err != nil {
		t.Fatal(err)
	}
	if count.Load() != 51 || peak.Load() != 4 || len(rows) != 50 {
		t.Fatalf("count=%d peak=%d rows=%d", count.Load(), peak.Load(), len(rows))
	}
	seen := map[string]bool{}
	for i, row := range rows {
		want := "completed"
		switch i {
		case 3:
			want = "failed"
		case 4:
			want = "partial"
		}
		if row.ID != fmt.Sprint(i) || row.Status != want || row.TaskID == "" || seen[row.TaskID] {
			t.Fatalf("row %d: %+v", i, row)
		}
		seen[row.TaskID] = true
	}
}

func TestTaskBatchCancelQueuedItems(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	task := tasktool.NewTaskTool()
	a := New(ctx, Config{Tools: tools.New(task, &tasktool.BatchTool{}), MaxConcurrentSubagents: 1})
	var started atomic.Int32
	task.Wire(func(context.Context, taskbridge.TaskSpec) (*taskbridge.TaskResult, error) {
		started.Add(1)
		cancel()
		return nil, context.Canceled
	})
	var items []map[string]string
	for i := range 5 {
		items = append(items, map[string]string{"id": fmt.Sprint(i), "input": "url"})
	}
	_, results, _ := a.toolRunner.dispatchToolCalls(ctx, []core.ToolCallItem{{Name: "task_batch", ID: "batch", Args: map[string]any{"profile": "explore", "prompt": "review", "items": items}}}, nil)
	var rows []batchItemResult
	if err := json.Unmarshal([]byte(results[0].Output), &rows); err != nil {
		t.Fatalf("%v, %+v", err, results[0])
	}
	if len(rows) != 5 || started.Load() != 1 {
		t.Fatalf("%+v started=%d", rows, started.Load())
	}
	for i, row := range rows {
		want := "not_started"
		if i == 0 {
			want = "interrupted"
		}
		if row.Status != want {
			t.Fatalf("%+v", rows)
		}
	}
}

func TestTaskBatchRespectsTaskDenyAndReturnsOneProviderReply(t *testing.T) {
	task := tasktool.NewTaskTool()
	task.Wire(func(context.Context, taskbridge.TaskSpec) (*taskbridge.TaskResult, error) {
		t.Fatal("denied task ran")
		return nil, nil
	})
	model := &evidenceSequenceLLM{scripts: [][]types.StreamToken{
		{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "batch-call", Name: "task_batch", Arguments: `{"profile":"explore","prompt":"review","items":[{"id":"a","input":"one"},{"id":"b","input":"two"}]}`}}},
		{{Content: "done"}},
	}}
	mgr := contextmgr.New(contextmgr.Config{ContextWindow: 128000})
	a := New(t.Context(), Config{Model: model, Context: mgr, Tools: tools.New(task, &tasktool.BatchTool{})})
	a.deps.toolExecutor.SetPermissionPolicy(permission.PermissionsDecl{Deny: []string{"task"}}, t.TempDir(), t.TempDir())
	got := a.Run("process items", nil, nil)
	if got.Error != nil || got.FinalOutput != "done" {
		t.Fatalf("%+v", got)
	}
	replies := 0
	for _, msg := range mgr.Snapshot().Transcript {
		if msg.Role != "tool" {
			continue
		}
		replies++
		var rows []batchItemResult
		if msg.ToolCallID != "batch-call" || json.Unmarshal([]byte(msg.Content), &rows) != nil || len(rows) != 2 {
			t.Fatalf("unexpected tool reply: %+v", msg)
		}
		for _, row := range rows {
			if row.Status != "denied" || row.Summary == "" || row.TaskID != "" || row.AttemptID != "" {
				t.Fatalf("%+v", row)
			}
		}
	}
	if replies != 1 {
		t.Fatalf("tool replies=%d", replies)
	}
}

func TestDeniedTaskBatchKeepsStatusInOversizeIndex(t *testing.T) {
	task := tasktool.NewTaskTool()
	a := New(t.Context(), Config{Tools: tools.New(task, &tasktool.BatchTool{})})
	a.deps.toolExecutor.SetPermissionPolicy(permission.PermissionsDecl{Deny: []string{"task"}}, t.TempDir(), t.TempDir())
	a.deps.toolExecutor.ExecutionState().OutputStore = core.NewOutputStore(t.TempDir())
	items := make([]taskbridge.BatchItem, 100)
	for i := range items {
		items[i] = taskbridge.BatchItem{ID: fmt.Sprintf("%s%d", strings.Repeat("文", 75), i), Input: "document"}
	}
	_, results, _ := a.toolRunner.dispatchToolCalls(t.Context(), []core.ToolCallItem{{Name: "task_batch", ID: "batch", Args: map[string]any{"profile": "explore", "prompt": "review", "items": items}}}, nil)
	var index []indexedBatchItem
	if err := json.Unmarshal([]byte(results[0].Output), &index); err != nil || len(index) != len(items) {
		t.Fatalf("invalid compact index: rows=%d err=%v", len(index), err)
	}
	for i, item := range index {
		if item.Index != i+1 || item.Status != "denied" || item.TaskID != "" {
			t.Fatalf("row %d: %+v", i, item)
		}
	}
}
