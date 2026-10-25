package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"nekocode/bot/session"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	tools "nekocode/bot/extension/tool"
	tasktool "nekocode/bot/extension/tool/builtin/task"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/protocol"
)

func TestSubagentParallelCallbacksKeepAttemptIdentity(t *testing.T) {
	r := newToolRunner(&Agent{deps: agentDeps{subSlotMgr: newSlotManager()}})
	cleanup, _, calls := r.prepareSubagentCallbacks([]core.ToolCallItem{{Name: "task", Args: map[string]any{}}}, []int{0}, func(ev protocol.StepEvent) {
		if ev.Action == protocol.StepActionToolPreview && (ev.SubAgentAttemptID != "attempt" || ev.SubAgentAttemptSeq != 1) {
			t.Error("lost attempt identity")
		}
	})
	defer cleanup()
	cb := calls[0].Args["_sub_callback"].(taskbridge.TaskCallback)
	cb.Callback(protocol.StepEvent{Action: protocol.StepActionSubAgentStart, SubAgentAttemptID: "attempt", SubAgentAttemptSeq: 1})
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			for range 100 {
				cb.Callback(protocol.StepEvent{Action: protocol.StepActionToolPreview, SubAgentAttemptID: "attempt", SubAgentAttemptSeq: 1})
			}
		})
	}
	workers.Wait()
}

func TestTaskQueueAdmissionAndCallbacksIntegrate(t *testing.T) {
	for _, limit := range []int{0, 1, 12} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			tool := tasktool.NewTaskTool()
			a := New(t.Context(), Config{Tools: tools.New(tool), MaxConcurrentSubagents: limit})
			r, slots := a.toolRunner, a.deps.subSlotMgr
			var completed, started atomic.Int32
			tool.Wire(func(ctx context.Context, spec taskbridge.TaskSpec) (*taskbridge.TaskResult, error) {
				cb, _ := taskbridge.TaskCallbackFromCtx(ctx)
				cb(protocol.StepEvent{Action: protocol.StepActionSubAgentStart})
				return &taskbridge.TaskResult{Content: "done"}, nil
			})
			var calls []core.ToolCallItem
			var indexes []int
			for i := 0; i < 100; i++ {
				calls = append(calls, core.ToolCallItem{ID: fmt.Sprint(i), Name: "task", Args: map[string]any{"profile": "explore", "prompt": "inspect"}})
				indexes = append(indexes, i)
			}
			cleanup, denied, kept := r.prepareSubagentCallbacks(calls, indexes, func(ev protocol.StepEvent) {
				if ev.SubAgentStatus == "completed" {
					completed.Add(1)
				}
				if ev.Action == protocol.StepActionSubAgentStart && ev.SubAgentStatus == "running" {
					started.Add(1)
				}
			})
			if len(denied) != 0 {
				t.Fatal(denied)
			}
			results := a.deps.toolExecutor.ExecuteBatch(t.Context(), kept)
			cleanup()
			for _, result := range results {
				if result.Error != "" {
					t.Fatalf("admitted task failed: %+v", result)
				}
			}
			if completed.Load() != 100 || started.Load() != 100 || slots.active != 0 || len(slots.reserved) != 0 {
				t.Fatalf("incomplete queue cleanup: started=%d completed=%d slots=%d", started.Load(), completed.Load(), slots.active)
			}
		})
	}
}

func TestHundredQueuedTasksReserveNoExecutionSlots(t *testing.T) {
	slots := newSlotManager()
	r := newToolRunner(&Agent{deps: agentDeps{subSlotMgr: slots}})
	var calls []core.ToolCallItem
	var indexes []int
	var queued, interrupted int
	for i := 0; i < 100; i++ {
		calls = append(calls, core.ToolCallItem{ID: fmt.Sprint(i), Name: "task", Args: map[string]any{"profile": "explore"}})
		indexes = append(indexes, i)
	}
	cleanup, denied, kept := r.prepareSubagentCallbacks(calls, indexes, func(ev protocol.StepEvent) {
		if ev.SubAgentStatus == "queued" {
			queued++
		}
		if ev.Action == protocol.StepActionSubAgentEnd && ev.SubAgentStatus == "interrupted" {
			interrupted++
		}
	})
	if len(denied) != 0 || len(kept) != 100 || slots.active != 0 || queued != 100 {
		t.Fatal("queue rejected tasks or occupied execution slots")
	}
	cleanup()
	cleanup()
	if interrupted != 100 || len(slots.reserved) != 0 {
		t.Fatal("queue cancellation leaked identities or duplicated terminal events")
	}
}

func TestResumeCallbackRejectsDuplicateAndPreservesStateOnFailure(t *testing.T) {
	for _, started := range []bool{false, true} {
		slots := newSlotManager()
		r := newToolRunner(&Agent{deps: agentDeps{subSlotMgr: slots}})
		var events []protocol.StepEvent
		cleanup, denied, calls := r.prepareSubagentCallbacks([]core.ToolCallItem{
			{Name: "task", Args: map[string]any{"resume_id": "saved", "profile": "explore"}},
			{Name: "task", Args: map[string]any{"resume_id": "saved", "profile": "explore"}},
		}, []int{0, 1}, func(ev protocol.StepEvent) { events = append(events, ev) })
		if len(calls) != 1 || denied[1] == "" || slots.active != 0 || len(events) != 1 || events[0].SubAgentStatus != "queued" {
			t.Fatal("duplicate resume reserved twice")
		}
		cb := calls[0].Args["_sub_callback"].(taskbridge.TaskCallback)
		events = nil
		if cb.ID != "saved" {
			t.Fatal("resume identity changed")
		}
		if started {
			if err := cb.Begin(t.Context()); err != nil {
				t.Fatal(err)
			}
			cb.Callback(protocol.StepEvent{Action: protocol.StepActionSubAgentStart})
		}
		cb.Callback(protocol.StepEvent{Action: protocol.StepActionSubAgentEnd, SubAgentStatus: "failed"})
		cleanup()
		if slots.active != 0 {
			t.Fatal("resume leaked slot")
		}
		want := 1
		if started {
			want = 2
		}
		if len(events) != want {
			t.Fatalf("started=%v events=%+v", started, events)
		}
		if !started && events[0].SubAgentStatus != "not_started" {
			t.Fatal("rejected resume changed saved status")
		}
	}
}

func TestSubagentCallbackLifecycle(t *testing.T) {
	for _, mode := range []string{"resolved", "legacy", "validation_error"} {
		t.Run(mode, func(t *testing.T) {
			r := newToolRunner(&Agent{deps: agentDeps{subSlotMgr: newSlotManager()}})
			var events []protocol.StepEvent
			cleanup, denied, calls := r.prepareSubagentCallbacks([]core.ToolCallItem{{
				Name: "task", Args: map[string]any{"profile": "reviewer"},
			}}, []int{0}, func(ev protocol.StepEvent) { events = append(events, ev) })
			if len(denied) != 0 || len(calls) != 1 || len(events) != 1 || events[0].SubAgentStatus != "queued" {
				t.Fatal("expected a reserved slot without premature start", denied, events)
			}
			delegated := calls[0].Args["_sub_callback"].(taskbridge.TaskCallback)
			events = nil
			if err := delegated.Begin(t.Context()); err != nil {
				t.Fatal(err)
			}
			cb := delegated.Callback
			wantProfile := "reviewer"
			if mode == "resolved" {
				wantProfile = "demo/reviewer"
				start := protocol.StepEvent{Action: protocol.StepActionSubAgentStart, SubAgentProfile: wantProfile, SubAgentAttemptID: "attempt-1", SubAgentAttemptSeq: 3}
				cb(start)
				cb(start) // Duplicate starts must not replace the running record.
			}
			if mode != "validation_error" {
				cb(protocol.StepEvent{Action: protocol.StepActionExecuteTool, ToolName: "read"})
			}
			cleanup()
			wantCount := 3
			if mode == "validation_error" {
				wantCount = 2
			}
			if len(events) != wantCount || events[0].Action != protocol.StepActionSubAgentStart || events[len(events)-1].Action != protocol.StepActionSubAgentEnd {
				t.Fatalf("unpaired lifecycle: %+v", events)
			}
			if events[0].SubAgentProfile != wantProfile || events[0].SubAgentID == "" {
				t.Fatalf("incorrect start metadata: %+v", events[0])
			}
			if delegated.ID != events[0].SubAgentID {
				t.Fatal("delegated interaction identity differs from event identity")
			}
			for _, ev := range events {
				if ev.SubAgentID != events[0].SubAgentID {
					t.Fatal("subagent ID changed")
				}
				if mode == "resolved" && (ev.SubAgentAttemptID != "attempt-1" || ev.SubAgentAttemptSeq != 3) {
					t.Fatalf("attempt identity changed across callbacks: %+v", ev)
				}
			}
		})
	}
}

func TestSubagentEndReleasesSlotBeforeBatchCleanup(t *testing.T) {
	slots := newSlotManager()
	r := newToolRunner(&Agent{deps: agentDeps{subSlotMgr: slots}})
	var events []protocol.StepEvent
	cleanup, _, calls := r.prepareSubagentCallbacks([]core.ToolCallItem{
		{Name: "task", Args: map[string]any{"profile": "explore", "prompt": "first"}},
		{Name: "task", Args: map[string]any{"profile": "coder", "prompt": "second"}},
	}, []int{0, 1}, func(ev protocol.StepEvent) { events = append(events, ev) })
	first := calls[0].Args["_sub_callback"].(taskbridge.TaskCallback)
	for _, call := range calls {
		if err := call.Args["_sub_callback"].(taskbridge.TaskCallback).Begin(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	events = nil
	first.Callback(protocol.StepEvent{Action: protocol.StepActionSubAgentEnd, SubAgentStatus: "completed"})
	if slots.active != 1 {
		t.Fatalf("finished task held its slot: %d", slots.active)
	}
	if len(events) != 2 || events[1].SubAgentStatus != "completed" || events[0].SubAgentPrompt != "first" {
		t.Fatalf("bad lifecycle: %+v", events)
	}
	cleanup()
	if slots.active != 0 || len(events) != 4 {
		t.Fatalf("cleanup duplicated lifecycle: active=%d events=%+v", slots.active, events)
	}
}

func TestInvalidTaskDoesNotLeakCallbackIntoSession(t *testing.T) {
	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{"missing profile", map[string]any{"prompt": "inspect"}},
		{"missing prompt", map[string]any{"profile": "explore"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestAgent()
			a.deps.subSlotMgr = newSlotManager()
			calls := []core.ToolCallItem{{ID: "invalid", Name: "task", Args: tc.args}}
			cleanup, _, prepared := a.toolRunner.prepareSubagentCallbacks(calls, []int{0}, nil)
			defer cleanup()
			tool := tasktool.NewTaskTool()
			tool.Wire(func(context.Context, taskbridge.TaskSpec) (*taskbridge.TaskResult, error) {
				t.Fatal("invalid task reached runner")
				return nil, nil
			})
			_, err := tool.Execute(t.Context(), prepared[0].Args)
			if err == nil || !strings.Contains(err.Error(), strings.TrimPrefix(tc.name, "missing ")) {
				t.Fatalf("validation error = %v", err)
			}
			a.toolRunner.recordToolResults(calls, nil, []core.ToolCallResult{{ID: "invalid", Name: "task", Error: err.Error()}})
			if _, err := json.Marshal(&session.Snapshot{Ledger: a.deps.gov.Snapshot()}); err != nil {
				t.Fatalf("failed task made session unpersistable: %v", err)
			}
			if _, exists := calls[0].Args["_sub_callback"]; exists {
				t.Fatal("runtime callback leaked into original arguments")
			}
		})
	}
}
