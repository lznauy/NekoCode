package runner

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/protocol"
)

func TestReceiptsIdentifyPhysicalRetriesAndPreserveRawFacts(t *testing.T) {
	tool := processFactTool{fakeTool: fakeTool{name: "fact"}, privileged: true}
	executor := newTestExecutor(fakeRegistry{"fact": tool})
	executor.SetFullAccess(true)
	identity := protocol.ExecutionIdentity{RunID: "run", TaskID: "task", AttemptID: "attempt"}
	ctx := protocol.WithExecutionIdentity(t.Context(), identity)
	call := core.ToolCallItem{ID: "reused-provider-id", Name: "fact", Args: map[string]any{"token": "argument-secret"}, EffectiveName: "mcp__demo__lookup", EffectiveArgs: map[string]any{"query": "effective input"}}
	seen := map[string]bool{}
	for range 2 {
		result := executor.ExecuteBatch(ctx, []core.ToolCallItem{call})[0]
		receipts := result.Evidence.Executions
		if len(receipts) != 2 || receipts[0].Status != "blocked" || receipts[1].Status != "error" {
			t.Fatalf("retry facts=%+v", receipts)
		}
		for _, r := range receipts {
			if _, err := uuid.Parse(r.ID); err != nil || seen[r.ID] {
				t.Fatal("nonunique receipt identity", r.ID)
			}
			seen[r.ID] = true
			if r.Execution != identity || r.Tool != call.EffectiveName || r.CallID != call.ID || r.ArgumentsHash != core.EvidenceHash(core.JSONArgs(call.EffectiveArgs)) {
				t.Fatalf("wrong execution scope: %+v", r)
			}
			if r.EndedAt < r.StartedAt || r.StartedAt == 0 {
				t.Fatal("invalid observation times")
			}
		}
		raw, _ := tool.result()
		if receipts[1].OutputHash != core.OutputHash(raw) || !receipts[1].RawComplete || !result.Evidence.Normalized || result.Evidence.ReturnedHash != core.OutputHash(result.TextOutput()) {
			t.Fatalf("raw/delivery values mixed: %+v", result.Evidence)
		}
		encoded, _ := json.Marshal(result.Evidence)
		if strings.Contains(string(encoded), "argument-secret") || strings.Contains(string(encoded), "effective input") {
			t.Fatal("receipt stored argument plaintext")
		}
	}
}

type panicReceiptTool struct{ fakeTool }

func (panicReceiptTool) Execute(context.Context, map[string]any) (core.ToolOutput, error) {
	panic("physical call panic")
}

func TestReceiptsDoNotInventExecutionBeforeDispatchOrAfterPanic(t *testing.T) {
	e := newTestExecutor(fakeRegistry{"panic": panicReceiptTool{fakeTool{name: "panic"}}, "plain": fakeTool{name: "plain"}})
	e.SetPlanMode(true)
	blocked := e.ExecuteBatch(t.Context(), []core.ToolCallItem{{Name: "plain"}})[0]
	if len(blocked.Evidence.Executions) != 0 || blocked.Error == "" {
		t.Fatal("pre-dispatch denial invented execution")
	}
	e.SetPlanMode(false)
	result := e.ExecuteBatch(t.Context(), []core.ToolCallItem{{Name: "panic"}})[0]
	if len(result.Evidence.Executions) != 1 || result.Evidence.Executions[0].Status != "unknown" || result.Evidence.Executions[0].ErrorHash == "" {
		t.Fatalf("panic claimed success: %+v", result)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	queued := e.ExecuteBatch(canceled, []core.ToolCallItem{{Name: "plain"}})[0]
	if len(queued.Evidence.Executions) != 0 {
		t.Fatal("unstarted queued call invented execution")
	}
}

func TestReceiptStatusDistinguishesToolReturnAndProcessOutcome(t *testing.T) {
	for _, tc := range []struct {
		process core.ProcessObservation
		want    string
	}{
		{core.ProcessObservation{}, "success"},
		{core.ProcessObservation{State: "running"}, "unknown"},
		{core.ProcessObservation{State: "exited"}, "unknown"},
		{core.ProcessObservation{State: "exited", ExitCodeKnown: true}, "success"},
		{core.ProcessObservation{State: "exited", ExitCodeKnown: true, ExitCode: 9}, "error"},
		{core.ProcessObservation{State: "timeout"}, "cancelled"},
	} {
		_, _, receipt := observeExecution(t.Context(), core.ToolCallItem{Name: "custom"}, func() (core.ToolOutput, error) {
			return core.ToolOutput{Text: "all tests passed", Process: tc.process}, nil
		})
		if receipt.Status != tc.want || receipt.RawComplete {
			t.Fatalf("invented process result: %+v", receipt)
		}
	}
	_, _, receipt := observeExecution(t.Context(), core.ToolCallItem{Name: "custom"}, func() (core.ToolOutput, error) { return core.ToolOutput{}, context.Canceled })
	if receipt.Status != "cancelled" {
		t.Fatal("cancelled invocation marked successful")
	}
}
