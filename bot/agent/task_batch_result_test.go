package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"nekocode/util/secrets"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tools "nekocode/bot/extension/tool"
	tasktool "nekocode/bot/extension/tool/builtin/task"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/protocol"
)

func TestBatchLargeManifestProvidesEveryOutcome(t *testing.T) {
	task := tasktool.NewTaskTool()
	r := New(t.Context(), Config{Tools: tools.New(task, &tasktool.BatchTool{})}).toolRunner
	r.agent.deps.toolExecutor.ExecutionState().OutputStore = core.NewOutputStore(t.TempDir())
	task.Wire(func(context.Context, taskbridge.TaskSpec) (*taskbridge.TaskResult, error) {
		return nil, fmt.Errorf("invalid profile: %s", strings.Repeat("x", 310))
	})
	items := make([]taskbridge.BatchItem, 100)
	for i := range items {
		items[i] = taskbridge.BatchItem{ID: fmt.Sprintf("%s-%d", strings.Repeat("站", 70), i), Input: "document"}
	}
	_, results, _ := r.dispatchToolCalls(t.Context(), []core.ToolCallItem{{Name: "task_batch", ID: "batch", Args: map[string]any{"profile": "invalid", "prompt": "review", "items": items}}}, nil)
	var preview []struct {
		Index  int    `json:"index"`
		Status string `json:"status"`
	}
	result := results[0]
	if err := json.Unmarshal([]byte(result.Output), &preview); err != nil || len(preview) != len(items) {
		t.Fatalf("invalid manifest preview: %v; %d of %d rows", err, len(preview), len(items))
	}
	for i, row := range preview {
		if row.Index != i+1 || row.Status != "failed" {
			t.Fatalf("row %d: %+v", i, row)
		}
	}
	data, err := os.ReadFile(result.Artifact.Path)
	if err != nil {
		t.Fatal(err)
	}
	var full []batchItemResult
	if err := json.Unmarshal(data, &full); err != nil || len(full) != len(items) {
		t.Fatalf("full manifest: %v (%d rows)", err, len(full))
	}
	for i, row := range full {
		if row.ID != items[i].ID || row.Summary == "" {
			t.Fatalf("incomplete row %d: %+v", i, row)
		}
	}
}

func TestBatchDoesNotReportSuccessfulDeliveryAfterPreservationFailure(t *testing.T) {
	task := tasktool.NewTaskTool()
	r := New(t.Context(), Config{Tools: tools.New(task, &tasktool.BatchTool{})}).toolRunner
	path := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(path, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	r.agent.deps.toolExecutor.ExecutionState().OutputStore = core.NewOutputStore(path)
	task.Wire(func(context.Context, taskbridge.TaskSpec) (*taskbridge.TaskResult, error) {
		return &taskbridge.TaskResult{Content: strings.Repeat("A", 2*core.MaxOutputBytes)}, nil
	})
	_, results, _ := r.dispatchToolCalls(t.Context(), []core.ToolCallItem{{Name: "task_batch", ID: "batch", Args: map[string]any{"prompt": "review", "profile": "explore", "items": []taskbridge.BatchItem{{ID: "one", Input: "doc"}}}}}, nil)
	var rows []struct {
		DeliveryError string `json:"delivery_error"`
		Status        string `json:"status"`
	}
	if err := json.Unmarshal([]byte(results[0].Output), &rows); err != nil || len(rows) != 1 || rows[0].DeliveryError == "" || rows[0].Status != "completed" {
		t.Fatalf("delivery error missing: %+v, %v", results[0], err)
	}
}

func TestBatchDoesNotOfferUnsavedTaskForResume(t *testing.T) {
	task := tasktool.NewTaskTool()
	r := New(t.Context(), Config{Tools: tools.New(task, &tasktool.BatchTool{})}).toolRunner
	task.Wire(func(context.Context, taskbridge.TaskSpec) (*taskbridge.TaskResult, error) {
		// The production runner resolves the profile before creating a session.
		return nil, fmt.Errorf("unknown profile")
	})
	_, results, _ := r.dispatchToolCalls(t.Context(), []core.ToolCallItem{{Name: "task_batch", ID: "batch", Args: map[string]any{"prompt": "review", "profile": "unknown", "items": []taskbridge.BatchItem{{ID: "one", Input: "doc"}}}}}, nil)
	var rows []batchItemResult
	if err := json.Unmarshal([]byte(results[0].Output), &rows); err != nil || len(rows) != 1 || rows[0].TaskID != "" || rows[0].Status != "failed" {
		t.Fatalf("unsaved task exposed for resume: %+v (%v)", rows, err)
	}
}

func TestBatchManifestWriteFailureStillReportsSettledItems(t *testing.T) {
	task := tasktool.NewTaskTool()
	r := New(t.Context(), Config{Tools: tools.New(task, &tasktool.BatchTool{})}).toolRunner
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	r.agent.deps.toolExecutor.ExecutionState().OutputStore = core.NewOutputStore(path)
	task.Wire(func(ctx context.Context, spec taskbridge.TaskSpec) (*taskbridge.TaskResult, error) {
		cb, _ := taskbridge.TaskCallbackFromCtx(ctx)
		cb(protocol.StepEvent{Action: protocol.StepActionSubAgentStart, SubAgentAttemptID: fmt.Sprintf("attempt-%d", len(spec.Title)), SubAgentAttemptSeq: 1})
		return &taskbridge.TaskResult{ID: taskbridge.TaskIDFromCtx(ctx), Content: strings.Repeat("A", 300)}, nil
	})
	items := make([]taskbridge.BatchItem, 100)
	for i := range items {
		items[i] = taskbridge.BatchItem{ID: fmt.Sprintf("%s-%d", strings.Repeat("站", 60), i), Input: "doc"}
	}
	_, results, _ := r.dispatchToolCalls(t.Context(), []core.ToolCallItem{{Name: "task_batch", ID: "batch", Args: map[string]any{"profile": "explore", "prompt": "review", "items": items}}}, nil)
	var manifest struct {
		Error string `json:"manifest_error"`
		Items []struct {
			Index  int    `json:"index"`
			TaskID string `json:"task_id"`
			Status string `json:"status"`
		} `json:"items"`
	}
	result := results[0]
	if result.Error != "" || result.Artifact.Path != "" || json.Unmarshal([]byte(result.Output), &manifest) != nil || manifest.Error == "" || len(manifest.Items) != len(items) {
		t.Fatalf("lost settled batch after manifest write failure: error=%q, rows=%d, manifest_error=%q", result.Error, len(manifest.Items), manifest.Error)
	}
	for i, row := range manifest.Items {
		if row.Index != i+1 || row.Status != "completed" || row.TaskID == "" {
			t.Fatalf("row %d: %+v", i, row)
		}
	}
}

func TestBatchManifestBudgetIncludesCredentialRedaction(t *testing.T) {
	const marker = `N&ko"`
	secrets.Register(marker)
	items := make([]taskbridge.BatchItem, 100)
	for i := range items {
		items[i] = taskbridge.BatchItem{ID: fmt.Sprintf("%s-%d", strings.Repeat(marker, 9), i), Input: "doc"}
	}
	task := tasktool.NewTaskTool()
	task.Wire(func(context.Context, taskbridge.TaskSpec) (*taskbridge.TaskResult, error) {
		return &taskbridge.TaskResult{Content: "done"}, nil
	})
	r := New(t.Context(), Config{Tools: tools.New(task, &tasktool.BatchTool{})}).toolRunner
	r.agent.deps.toolExecutor.ExecutionState().OutputStore = core.NewOutputStore(t.TempDir())
	for _, count := range []int{1, len(items)} {
		_, results, _ := r.dispatchToolCalls(t.Context(), []core.ToolCallItem{{Name: "task_batch", ID: "batch", Args: map[string]any{"profile": "explore", "prompt": "inspect", "items": items[:count]}}}, nil)
		result := results[0]
		var rows []batchItemResult
		if err := json.Unmarshal([]byte(result.Output), &rows); err != nil || len(rows) != count {
			t.Fatalf("invalid batch after credential filtering: %v (%d rows)", err, len(rows))
		}
		if count == 1 && result.Artifact.Path != "" {
			t.Fatal("unexpected artifact for a single short item")
		}
		if count > 1 {
			data, err := os.ReadFile(result.Artifact.Path)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &rows); err != nil || len(rows) != count {
				t.Fatalf("invalid full manifest: %v, rows=%d", err, len(rows))
			}
		}
		for i, row := range rows {
			want := strings.ReplaceAll(items[i].ID, marker, secrets.Replacement)
			if row.ID != want || row.Status != "completed" {
				t.Fatalf("wrong redacted manifest row %d", i)
			}
		}
	}
}
