package runner

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/protocol"
)

func TestCancelledSequentialCallNeverInvokesTool(t *testing.T) {
	e := newTestExecutor(fakeRegistry{"plain": fakeTool{name: "plain", mode: core.ModeSequential}})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result := e.ExecuteBatch(ctx, []core.ToolCallItem{{ID: "call", Name: "plain"}})[0]
	if len(result.Evidence.Executions) != 0 || result.Error == "" {
		t.Fatalf("cancelled sequential call was dispatched: %+v", result)
	}
	if result.NonExecution.Stage != "dispatch" || result.NonExecution.Outcome != "cancelled" {
		t.Fatalf("decision=%+v", result.NonExecution)
	}
}

func TestNonExecutionScopeAndPhysicalCallsRemainDistinct(t *testing.T) {
	e := newPermTestExecutor(t)
	identity := protocol.ExecutionIdentity{RunID: "run", TaskID: "child", AttemptID: "attempt"}
	ctx := protocol.WithExecutionIdentity(t.Context(), identity)
	seen := map[string]bool{}
	for range 2 {
		result := e.ExecuteBatch(ctx, []core.ToolCallItem{{ID: "same-provider-id", Name: "shell", Args: map[string]any{"command": "sudo secret"}}})[0]
		d := result.NonExecution
		if _, err := uuid.Parse(d.ID); err != nil || seen[d.ID] {
			t.Fatalf("nonunique decision: %+v", d)
		}
		seen[d.ID] = true
		if d.Schema != 1 || d.Execution != identity || d.CallID != "same-provider-id" || d.Tool != "shell" || d.Stage != "permission" || d.Outcome != "blocked" || d.ReasonHash != core.EvidenceHash([]byte(result.Error)) || d.RecordedAt == 0 || result.Evidence.Schema != 0 {
			t.Fatalf("result=%+v", result)
		}
	}
	physical := newTestExecutor(fakeRegistry{"panic": panicReceiptTool{fakeTool{name: "panic"}}}).ExecuteBatch(ctx, []core.ToolCallItem{{ID: "same-provider-id", Name: "panic"}})[0]
	if physical.NonExecution.Schema != 0 || len(physical.Evidence.Executions) != 1 {
		t.Fatalf("physical failure became non-execution: %+v", physical)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	queued := newTestExecutor(fakeRegistry{"plain": fakeTool{name: "plain"}}).ExecuteBatch(cancelled, []core.ToolCallItem{{Name: "plain", EffectiveName: "effective-target"}})[0]
	if queued.NonExecution.Execution != identity || queued.NonExecution.Tool != "effective-target" || queued.NonExecution.Outcome != "cancelled" || queued.Evidence.Schema != 0 {
		t.Fatalf("queued=%+v", queued)
	}
}

func TestCancellationDuringApprovalDoesNotDispatchTool(t *testing.T) {
	e := newPermTestExecutor(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply { cancel(); return protocol.AllowOnce() })
	result := e.ExecuteBatch(ctx, []core.ToolCallItem{{ID: "call", Name: "shell", Args: map[string]any{"command": "git add ."}}})[0]
	if result.NonExecution.Outcome != "cancelled" || result.NonExecution.Stage != "dispatch" || len(result.Evidence.Executions) != 0 {
		t.Fatalf("result=%+v", result)
	}
}

type cancelQueuedTool struct {
	fakeTool
	cancel context.CancelFunc
	calls  int
}

func (t *cancelQueuedTool) Execute(context.Context, map[string]any) (core.ToolOutput, error) {
	t.calls++
	t.cancel()
	return core.ToolOutput{Text: "first executed"}, nil
}

func TestQueueCancellationRecordsOnlyUnstartedCall(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	tool := &cancelQueuedTool{fakeTool: fakeTool{name: "task"}, cancel: cancel}
	e := newTestExecutor(fakeRegistry{"task": tool})
	e.SetMaxConcurrentTasks(1)
	results := e.ExecuteBatch(ctx, []core.ToolCallItem{{ID: "first", Name: "task"}, {ID: "second", Name: "task"}})
	if tool.calls != 1 || len(results[0].Evidence.Executions) != 1 || results[0].NonExecution.Schema != 0 || results[1].NonExecution.Outcome != "cancelled" || results[1].NonExecution.CallID != "second" || results[1].Evidence.Schema != 0 {
		t.Fatalf("calls=%d results=%+v", tool.calls, results)
	}
}
