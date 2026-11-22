package subagent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

// The process ID changes on every relaunch in the Relay trace.
func TestForegroundProcessWaitReviewAcrossHandles(t *testing.T) {
	var w processWaitHistory
	launch := func(task, commandHash string) {
		w.observe("shell", nil, core.ToolCallResult{Evidence: protocol.ToolEvidence{Executions: []protocol.ExecutionReceipt{
			{Tool: "shell", ArgumentsHash: commandHash, Process: protocol.ProcessObservation{Task: task, State: "running"}},
		}}})
	}
	launch("first", "A")
	args := map[string]any{"action": "wait", "task": "first"}
	receipt := protocol.ExecutionReceipt{Tool: "process", StartedAt: 1, EndedAt: 1 + int64(61*time.Second), Process: protocol.ProcessObservation{Task: "first", State: "exited", Reason: "exit", SampleHash: "same"}}
	result := core.ToolCallResult{Evidence: protocol.ToolEvidence{Executions: []protocol.ExecutionReceipt{receipt}}}
	w.observe("process", args, result)
	if w.deferWait("process", args, nil) {
		t.Fatal("single long wait caused handoff")
	}
	launch("second", "B")
	result.Evidence.Executions[0].Process.Task = "second"
	w.observe("process", args, result)
	if w.deferWait("read", nil, nil) || w.deferWait("process", map[string]any{"action": "stop", "task": "second"}, nil) {
		t.Fatal("productive or cleanup tools blocked")
	}
	launch("third", "B")
	args["task"] = "third"
	if w.deferWait("process", args, nil) {
		t.Fatal("different commands with identical output were combined")
	}
	result.Evidence.Executions[0].Process.Task = "third"
	w.observe("process", args, result)
	if w.deferWait("process", map[string]any{"action": "wait", "task": "unrelated"}, nil) {
		t.Fatal("unrelated wait blocked")
	}
	launch("fourth", "B")
	args["task"] = "fourth"
	if !w.deferWait("process", args, nil) || !strings.Contains(w.description(), "fourth") {
		t.Fatal(w)
	}
	w.observe("edit", nil, core.ToolCallResult{Error: "denied"})
	if !w.deferWait("process", args, nil) {
		t.Fatal("failed edit cleared observation history")
	}
	w.observe("edit", nil, core.ToolCallResult{Evidence: protocol.ToolEvidence{Executions: []protocol.ExecutionReceipt{{Tool: "edit", Status: "success"}}}})
	if w.reviewTask != "fourth" {
		t.Fatal("sibling edit erased a promised handoff")
	}
	w.reviewTask = ""
	w.observe("edit", nil, core.ToolCallResult{Evidence: protocol.ToolEvidence{Executions: []protocol.ExecutionReceipt{{Tool: "edit", Status: "success"}}}})
	if w.deferWait("process", args, nil) {
		t.Fatal("successful edit did not start a new observation period")
	}
}

func TestForegroundDistinctResultsAndLiveOutput(t *testing.T) {
	for _, state := range []string{"exited", "running"} {
		var w processWaitHistory
		args := map[string]any{"action": "watch", "task": "worker"}
		for _, hash := range []string{"one", "two", "three"} {
			obs := protocol.ProcessObservation{Task: "worker", State: state, Reason: "output", SampleHash: hash, NewOutputBytes: 512}
			receipt := protocol.ExecutionReceipt{Tool: "process", StartedAt: 1, EndedAt: 1 + int64(65*time.Second), Process: obs}
			w.observe("process", args, core.ToolCallResult{Evidence: protocol.ToolEvidence{Executions: []protocol.ExecutionReceipt{receipt}}})
			if w.deferWait("process", args, nil) {
				t.Fatalf("fresh %s result triggered review: %+v", state, w)
			}
		}
	}
}

func TestForegroundSilentWaitOnAnotherTask(t *testing.T) {
	w := processWaitHistory{observations: 2, elapsed: 2 * time.Minute, last: protocol.ProcessObservation{State: "running", Task: "one", Reason: "wait_timeout"}}
	if w.deferWait("process", map[string]any{"action": "wait", "task": "two"}, nil) {
		t.Fatal("unrelated running task was blocked")
	}
	if !w.deferWait("process", map[string]any{"action": " WAIT ", "task": " one "}, nil) {
		t.Fatal("repeated silent wait was allowed")
	}
}

type waitingModel struct {
	scriptedLLM
	entered chan struct{}
}

func (m *waitingModel) ChatStream(ctx context.Context, _ []types.Message, _ []types.ToolDef) (<-chan types.StreamToken, <-chan error) {
	tokens := make(chan types.StreamToken)
	errs := make(chan error, 1)
	close(m.entered)
	go func() { <-ctx.Done(); errs <- ctx.Err(); close(errs); close(tokens) }()
	return tokens, errs
}

func TestForegroundIntervalPersistsResumableAttempt(t *testing.T) {
	for _, cancelParent := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		model := &waitingModel{entered: make(chan struct{})}
		worker := New(Config{LLM: model, Tools: tools.New()})
		var saved string
		var snapshot ctxmgr.ManagerSnapshot
		cfg := RunConfig{IsolatedWorkspace: true, SubagentID: "worker", Prompt: "check entry point", ForegroundInterval: 50 * time.Millisecond, SaveContext: func(s ctxmgr.ManagerSnapshot, status, _ string, _ AttemptUsage) error {
			saved = status
			snapshot = s
			return nil
		}}
		if cancelParent {
			cfg.ForegroundInterval = time.Minute
			go func() { <-model.entered; cancel() }()
		}
		result, err := worker.Run(ctx, cfg)
		cancel()
		if cancelParent {
			if !errors.Is(err, context.Canceled) || saved != "interrupted" || result.Summary.StopReason != "cancelled" {
				t.Fatalf("parent cancel: %+v %v %s", result, err, saved)
			}
			continue
		}
		if err != nil || result.Status != StatusPartial || result.Summary.StopReason != "foreground_interval" || saved != "partial" {
			t.Fatalf("yield: %+v %v %s", result, err, saved)
		}
		if !strings.Contains(result.Content, "Previous process handles") || len(snapshot.Messages) == 0 || !strings.Contains(snapshot.Messages[len(snapshot.Messages)-1].Content, "Previous process handles") {
			t.Fatal("missing durable worktree process disposition")
		}
		cfg.ResumeContext = &snapshot
		cfg.ForegroundInterval = time.Minute
		cfg.Prompt = "use existing work"
		result, err = New(Config{LLM: &scriptedLLM{scripts: [][]types.StreamToken{completionScript()}}, Tools: tools.New()}).Run(t.Context(), cfg)
		if err != nil || result.Status != StatusCompleted || saved != "completed" {
			t.Fatalf("resume: %+v %v %s", result, err, saved)
		}
	}
}

func TestRepeatedWaitYieldsAtToolBoundaryAndFinishesHandoff(t *testing.T) {
	process := &countingTool{name: "process"}
	engine := New(Config{LLM: &scriptedLLM{scripts: [][]types.StreamToken{stageHandoff()}}, Tools: tools.New(process)})
	cfg := RunConfig{Profile: Profile{Name: "coder", Tools: []string{"process"}}, ForegroundInterval: time.Minute, guard: newRunGuard()}
	exec, closeExec := engine.newExecutor(cfg)
	defer closeExec()
	run := &engineRun{engine: engine, ctx: t.Context(), cfg: cfg, ctxMgr: engine.newContextManager(cfg), executor: exec, state: newRunState(), log: func(string, ...any) {}, phase: func(string) {}}
	run.state.processWait = processWaitHistory{observations: 2, elapsed: 2 * time.Minute, last: protocol.ProcessObservation{Task: "new", State: "running"}}
	call := core.ToolCallItem{ID: "wait", Name: "process", Args: map[string]any{"action": "wait", "task": "new"}}
	if err := engine.executeToolBatch(run.ctx, cfg, run.ctxMgr, exec, []core.ToolCallItem{call}, run.state, run.phase, run.log); err != nil {
		t.Fatal(err)
	}
	if process.calls != 0 || run.stepLimitReached() || run.handoffPhase != handoffFinalizing {
		t.Fatal("worker did not yield before another wait")
	}
	if !run.stepOnce() {
		t.Fatal("handoff did not settle")
	}
	result, err := run.finish()
	if err != nil || result.Status != StatusPartial || result.Summary.StopReason != "process_wait_review" || !strings.Contains(result.Content, "new") {
		t.Fatalf("%+v %v", result, err)
	}
}

type silentWaitTool struct {
	testProxyTool
	calls int
}

func (*silentWaitTool) ExecutionMode(map[string]any) core.ExecutionMode { return core.ModeSequential }

func (t *silentWaitTool) Execute(_ context.Context, args map[string]any) (core.ToolOutput, error) {
	t.calls++
	time.Sleep(65 * time.Second)
	return core.ToolOutput{Process: protocol.ProcessObservation{Task: args["task"].(string), State: "running", Reason: "wait_timeout"}}, nil
}

func TestForegroundWaitsWithinOneBatch(t *testing.T) {
	for _, isolated := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			process := &silentWaitTool{testProxyTool: testProxyTool{name: "process"}}
			edit := &countingTool{name: "edit"}
			engine := New(Config{Tools: tools.New(process, edit)})
			cfg := RunConfig{IsolatedWorkspace: isolated, Profile: Profile{Name: "coder", Tools: []string{"process", "edit"}}, ForegroundInterval: time.Hour, guard: newRunGuard()}
			executor, closeExec := engine.newExecutor(cfg)
			defer closeExec()
			state := newRunState()
			var calls []core.ToolCallItem
			for _, id := range []string{"a", "b", "c"} {
				calls = append(calls, core.ToolCallItem{ID: id, Name: "process", Args: map[string]any{"task": "worker", "action": "wait"}})
			}
			calls = append(calls, core.ToolCallItem{ID: "edit", Name: "edit"})
			err := engine.executeToolBatch(t.Context(), cfg, engine.newContextManager(cfg), executor, calls, state, func(string) {}, func(string, ...any) {})
			wantWaits, wantTask := 2, "worker"
			if isolated {
				wantWaits, wantTask = 3, ""
			}
			if err != nil || process.calls != wantWaits || edit.calls != 1 || state.processWait.reviewTask != wantTask {
				t.Fatalf("err=%v waits=%d edits=%d review=%q", err, process.calls, edit.calls, state.processWait.reviewTask)
			}
		})
	}
}

type blockedForegroundTool struct {
	testProxyTool
	unwound bool
}

func (t *blockedForegroundTool) Execute(ctx context.Context, _ map[string]any) (core.ToolOutput, error) {
	<-ctx.Done()
	t.unwound = true
	return core.ToolOutput{}, ctx.Err()
}
func TestForegroundIntervalDrainsToolBeforeFinalCheckpoint(t *testing.T) {
	blocked := &blockedForegroundTool{testProxyTool: testProxyTool{name: "inspect"}}
	model := &scriptedLLM{scripts: [][]types.StreamToken{{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "inspect-1", Name: "inspect", Arguments: "{}"}}}}}
	engine := New(Config{LLM: model, Tools: tools.New(blocked)})
	result, err := engine.Run(t.Context(), RunConfig{ForegroundInterval: 200 * time.Millisecond, Profile: Profile{Name: "coder", Tools: []string{"inspect"}}, SaveContext: func(_ ctxmgr.ManagerSnapshot, status, _ string, _ AttemptUsage) error {
		if status != "running" && (!blocked.unwound || status != "partial") {
			t.Errorf("terminal checkpoint before worker drained: status=%s unwound=%v", status, blocked.unwound)
		}
		return nil
	}})
	if err != nil || !blocked.unwound || result.Summary.StopReason != "foreground_interval" {
		t.Fatalf("result=%+v err=%v unwound=%v", result, err, blocked.unwound)
	}
}

type readyForegroundProcess struct{ testProxyTool }

func (readyForegroundProcess) WaitReady(map[string]any) bool { return true }
func (readyForegroundProcess) Execute(context.Context, map[string]any) (core.ToolOutput, error) {
	return core.ToolOutput{Text: "tests finished", Process: protocol.ProcessObservation{Task: "build", State: "exited", ExitCodeKnown: true}}, nil
}

func TestForegroundWorkerCollectsExitAfterTwoSilentWaits(t *testing.T) {
	engine := New(Config{Tools: tools.New(readyForegroundProcess{testProxyTool{name: "process"}})})
	var completed ToolCallEvent
	cfg := RunConfig{Profile: Profile{Name: "coder", Tools: []string{"process"}}, ForegroundInterval: time.Minute, guard: newRunGuard(), OnToolCall: func(event ToolCallEvent) {
		if event.Action == protocol.StepActionExecuteTool {
			completed = event
		}
	}}
	executor, cleanup := engine.newExecutor(cfg)
	defer cleanup()
	state := newRunState()
	state.processWait = processWaitHistory{observations: 2, elapsed: 2 * time.Minute, last: protocol.ProcessObservation{Task: "build", State: "running", Reason: "wait_timeout"}}
	call := core.ToolCallItem{ID: "collect", Name: "process", Args: map[string]any{"action": "wait", "task": "build"}}
	err := engine.executeToolBatch(t.Context(), cfg, engine.newContextManager(cfg), executor, []core.ToolCallItem{call}, state, func(string) {}, func(string, ...any) {})
	if err != nil || state.processWait.reviewTask != "" || completed.Output != "tests finished" || len(completed.Evidence.Executions) != 1 || !completed.Evidence.Executions[0].Process.ExitCodeKnown {
		t.Fatalf("review=%q result=%+v err=%v", state.processWait.reviewTask, completed, err)
	}
}
