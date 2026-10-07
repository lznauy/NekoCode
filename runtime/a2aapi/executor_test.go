package a2aapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

	controlruntime "nekocode/runtime"
)

type fakeRuntime struct {
	mu sync.Mutex

	runID           controlruntime.RunID
	sessionID       string
	nextSession     int
	eventBatches    [][]controlruntime.Event
	replayCalls     int
	startedInputs   []controlruntime.Input
	startCtx        context.Context
	startErr        error
	cancelled       []controlruntime.RunID
	approvalID      string
	approval        controlruntime.ApprovalDecision
	questionID      string
	question        controlruntime.QuestionReply
	waited          []controlruntime.RunID
	resumeSessions  []string
	liveEvents      chan controlruntime.Event
	replayStarted   chan struct{}
	replayOnce      sync.Once
	cancelCalled    chan struct{}
	cancelOnce      sync.Once
	startEntered    chan struct{}
	startRelease    chan struct{}
	deleteErr       error
	deletedSessions []string
}

func (f *fakeRuntime) StartRunInSession(ctx context.Context, sessionID string, input controlruntime.Input) (controlruntime.RunID, string, error) {
	if f.startEntered != nil {
		f.startEntered <- struct{}{}
	}
	if f.startRelease != nil {
		<-f.startRelease
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.startErr != nil {
		return "", sessionID, f.startErr
	}
	f.startedInputs = append(f.startedInputs, input)
	f.startCtx = ctx
	if sessionID == "" {
		f.nextSession++
		sessionID = "session_" + string(rune('0'+f.nextSession))
	}
	f.sessionID = sessionID
	if f.runID == "" {
		f.runID = "run_1"
	}
	return f.runID, sessionID, nil
}

func (f *fakeRuntime) CancelRun(_ context.Context, id controlruntime.RunID) error {
	f.mu.Lock()
	f.cancelled = append(f.cancelled, id)
	liveEvents := f.liveEvents
	if f.cancelCalled != nil {
		f.cancelOnce.Do(func() { close(f.cancelCalled) })
	}
	f.mu.Unlock()
	if liveEvents != nil {
		liveEvents <- controlruntime.Event{Sequence: 1, Type: controlruntime.EventRunCancelled}
		close(liveEvents)
	}
	return nil
}

func (f *fakeRuntime) DecideApproval(_ context.Context, id string, decision controlruntime.ApprovalDecision) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.approvalID, f.approval = id, decision
	return nil
}

func (f *fakeRuntime) AnswerQuestion(_ context.Context, id string, reply controlruntime.QuestionReply) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.questionID, f.question = id, reply
	return nil
}

func (f *fakeRuntime) ReplayEvents(_ context.Context, _ controlruntime.EventFilter) (<-chan controlruntime.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.liveEvents != nil {
		if f.replayStarted != nil {
			f.replayOnce.Do(func() { close(f.replayStarted) })
		}
		return f.liveEvents, nil
	}
	if f.replayCalls >= len(f.eventBatches) {
		return nil, errors.New("missing event batch")
	}
	events := f.eventBatches[f.replayCalls]
	f.replayCalls++
	ch := make(chan controlruntime.Event, len(events))
	for _, event := range events {
		ch <- event
	}
	close(ch)
	return ch, nil
}

func (f *fakeRuntime) WaitRun(_ context.Context, id controlruntime.RunID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.waited = append(f.waited, id)
	return nil
}

func (f *fakeRuntime) DeleteInactiveSession(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletedSessions = append(f.deletedSessions, id)
	return f.deleteErr
}

func (f *fakeRuntime) CurrentSessionID() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sessionID
}

func executeEvents(t *testing.T, executor *executor, execCtx *a2asrv.ExecutorContext) []a2a.Event {
	t.Helper()
	var events []a2a.Event
	for event, err := range executor.Execute(context.Background(), execCtx) {
		if err != nil {
			t.Fatalf("Execute error: %v", err)
		}
		events = append(events, event)
	}
	return events
}

func TestExecutorStreamsRuntimeOutput(t *testing.T) {
	rt := &fakeRuntime{eventBatches: [][]controlruntime.Event{{
		{Sequence: 1, Type: controlruntime.EventAssistantDelta, Payload: controlruntime.DeltaPayload{Delta: "hel"}},
		{Sequence: 2, Type: controlruntime.EventAssistantDelta, Payload: controlruntime.DeltaPayload{Delta: "lo"}},
		{Sequence: 3, Type: controlruntime.EventRunDone, Payload: controlruntime.RunResult{Output: "hello"}},
	}}}
	execCtx := &a2asrv.ExecutorContext{
		TaskID:    "task_1",
		ContextID: "context_1",
		Message:   a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("say hello")),
	}

	events := executeEvents(t, newExecutor(rt), execCtx)
	if len(events) != 5 {
		t.Fatalf("event count = %d, want 5", len(events))
	}
	if task, ok := events[0].(*a2a.Task); !ok || task.Status.State != a2a.TaskStateSubmitted {
		t.Fatalf("first event = %#v, want submitted task", events[0])
	}
	if status := events[1].(*a2a.TaskStatusUpdateEvent); status.Status.State != a2a.TaskStateWorking {
		t.Fatalf("working status = %s", status.Status.State)
	}
	firstArtifact := events[2].(*a2a.TaskArtifactUpdateEvent)
	secondArtifact := events[3].(*a2a.TaskArtifactUpdateEvent)
	if firstArtifact.Artifact.Parts[0].Text() != "hel" || secondArtifact.Artifact.Parts[0].Text() != "lo" || !secondArtifact.Append {
		t.Fatalf("artifact stream = %#v then %#v", firstArtifact, secondArtifact)
	}
	if status := events[4].(*a2a.TaskStatusUpdateEvent); status.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("terminal status = %s", status.Status.State)
	}
	if len(rt.startedInputs) != 1 || rt.startedInputs[0].Text != "say hello" || rt.startedInputs[0].Source.Kind != "a2a" {
		t.Fatalf("started input = %#v", rt.startedInputs)
	}
	if len(rt.waited) != 1 || rt.waited[0] != "run_1" {
		t.Fatalf("waited runs = %#v", rt.waited)
	}
}

func TestExecutorApprovalRoundTrip(t *testing.T) {
	approval := controlruntime.ApprovalView{ID: "approval_1", ToolName: "bash", Args: map[string]any{"command": "go test ./..."}}
	rt := &fakeRuntime{eventBatches: [][]controlruntime.Event{
		{{Sequence: 1, Type: controlruntime.EventApprovalRequested, Payload: approval}},
		{{Sequence: 2, Type: controlruntime.EventRunDone, Payload: controlruntime.RunResult{Output: "ok"}}},
	}}
	executor := newExecutor(rt)
	first := &a2asrv.ExecutorContext{
		TaskID: "task_approval", ContextID: "context_approval",
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("run tests")),
	}
	events := executeEvents(t, executor, first)
	status := events[len(events)-1].(*a2a.TaskStatusUpdateEvent)
	if status.Status.State != a2a.TaskStateInputRequired {
		t.Fatalf("status = %s, want input required", status.Status.State)
	}

	second := &a2asrv.ExecutorContext{
		TaskID: "task_approval", ContextID: "context_approval",
		StoredTask: &a2a.Task{ID: "task_approval", ContextID: "context_approval", Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired}},
		Message:    a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("allow")),
	}
	events = executeEvents(t, executor, second)
	if rt.approvalID != "approval_1" || !rt.approval.Allowed {
		t.Fatalf("approval = %q %#v", rt.approvalID, rt.approval)
	}
	if status := events[len(events)-1].(*a2a.TaskStatusUpdateEvent); status.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("terminal status = %s", status.Status.State)
	}
}

func TestExecutorAbandonsResumedRunWhenWorkingStatusCannotBeDelivered(t *testing.T) {
	rt := &fakeRuntime{
		eventBatches: [][]controlruntime.Event{{{
			Sequence: 1,
			Type:     controlruntime.EventApprovalRequested,
			Payload:  controlruntime.ApprovalView{ID: "approval_1", ToolName: "bash"},
		}}},
		cancelCalled: make(chan struct{}),
	}
	executor := newExecutor(rt)
	executeEvents(t, executor, &a2asrv.ExecutorContext{
		TaskID: "task_approval", ContextID: "context_approval",
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("run command")),
	})

	resumed := &a2asrv.ExecutorContext{
		TaskID: "task_approval", ContextID: "context_approval",
		StoredTask: &a2a.Task{ID: "task_approval", ContextID: "context_approval", Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired}},
		Message:    a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("allow")),
	}
	for range executor.Execute(context.Background(), resumed) {
		break
	}
	select {
	case <-rt.cancelCalled:
	case <-time.After(time.Second):
		t.Fatal("resumed runtime run was not canceled after working status delivery stopped")
	}
}

func TestInputRequiredDoesNotCancelRuntimeRun(t *testing.T) {
	rt := &fakeRuntime{eventBatches: [][]controlruntime.Event{{
		{
			Sequence: 1,
			Type:     controlruntime.EventApprovalRequested,
			Payload:  controlruntime.ApprovalView{ID: "approval_1", ToolName: "bash"},
		},
	}}}
	executor := newExecutor(rt)
	ctx, cancel := context.WithCancel(context.Background())
	execCtx := &a2asrv.ExecutorContext{
		TaskID: "task_1", ContextID: "context_1",
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("run command")),
	}
	for _, err := range executor.Execute(ctx, execCtx) {
		if err != nil {
			t.Fatal(err)
		}
	}
	cancel()
	select {
	case <-rt.startCtx.Done():
		t.Fatal("runtime run inherited the short-lived A2A execution cancellation")
	default:
	}
}

func TestExecutorAbandonsRunWhenEventConsumerStops(t *testing.T) {
	rt := &fakeRuntime{
		eventBatches: [][]controlruntime.Event{{
			{Sequence: 1, Type: controlruntime.EventAssistantDelta, Payload: controlruntime.DeltaPayload{Delta: "output"}},
		}},
		cancelCalled: make(chan struct{}),
	}
	executor := newExecutor(rt)
	execCtx := &a2asrv.ExecutorContext{
		TaskID: "task_1", ContextID: "context_1",
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("work")),
	}
	for event, err := range executor.Execute(context.Background(), execCtx) {
		if err != nil {
			t.Fatal(err)
		}
		if status, ok := event.(*a2a.TaskStatusUpdateEvent); ok && status.Status.State == a2a.TaskStateWorking {
			break
		}
	}
	select {
	case <-rt.cancelCalled:
	case <-time.After(time.Second):
		t.Fatal("runtime run was not canceled after the event consumer stopped")
	}
}

func TestExecutorRemovesBindingWhenConsumerStopsAtSubmission(t *testing.T) {
	executor := newExecutor(&fakeRuntime{})
	execCtx := &a2asrv.ExecutorContext{
		TaskID: "task_disconnect", ContextID: "context_disconnect",
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("work")),
	}
	for range executor.Execute(context.Background(), execCtx) {
		break
	}
	if executor.lookup(execCtx.TaskID) != nil {
		t.Fatal("aborted submission retained an executor binding")
	}
}

func TestExecutorRejectsNonTextContent(t *testing.T) {
	executor := newExecutor(&fakeRuntime{})
	execCtx := &a2asrv.ExecutorContext{
		TaskID: "task_1", ContextID: "context_1",
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewRawPart([]byte("data"))),
	}
	var got error
	for _, err := range executor.Execute(context.Background(), execCtx) {
		got = err
	}
	if !errors.Is(got, a2a.ErrUnsupportedContentType) {
		t.Fatalf("error = %v, want unsupported content type", got)
	}
}

func TestExecutorRejectsDataOnlyNewTask(t *testing.T) {
	executor := newExecutor(&fakeRuntime{})
	execCtx := &a2asrv.ExecutorContext{
		TaskID: "task_1", ContextID: "context_1",
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewDataPart(map[string]any{"allowed": true})),
	}
	var got error
	for _, err := range executor.Execute(context.Background(), execCtx) {
		got = err
	}
	if !errors.Is(got, a2a.ErrInvalidParams) {
		t.Fatalf("error = %v, want invalid params", got)
	}
	if executor.lookup("task_1") != nil {
		t.Fatal("invalid task retained an executor binding")
	}
}

func TestExecutorRejectsOversizedProtocolFields(t *testing.T) {
	executor := newExecutor(&fakeRuntime{})
	execCtx := &a2asrv.ExecutorContext{
		TaskID:    a2a.TaskID(strings.Repeat("x", maxProtocolIDBytes+1)),
		ContextID: "context_1",
		Message:   a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hello")),
	}
	var got error
	for _, err := range executor.Execute(context.Background(), execCtx) {
		got = err
	}
	if !errors.Is(got, a2a.ErrInvalidParams) {
		t.Fatalf("error = %v, want invalid params", got)
	}
}

func TestMessageTextRejectsOversizedText(t *testing.T) {
	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(strings.Repeat("x", maxMessagePartBytes+1)))
	if _, err := messageText(message); !errors.Is(err, a2a.ErrInvalidParams) {
		t.Fatalf("error = %v, want invalid params", err)
	}
}

func TestExecutorCancel(t *testing.T) {
	rt := &fakeRuntime{}
	executor := newExecutor(rt)
	executor.tasks["task_1"] = &taskBinding{runID: "run_1"}
	execCtx := &a2asrv.ExecutorContext{TaskID: "task_1", ContextID: "context_1"}

	var events []a2a.Event
	for event, err := range executor.Cancel(context.Background(), execCtx) {
		if err != nil {
			t.Fatalf("Cancel error: %v", err)
		}
		events = append(events, event)
	}
	if len(rt.cancelled) != 1 || rt.cancelled[0] != "run_1" {
		t.Fatalf("cancelled runs = %#v", rt.cancelled)
	}
	if len(events) != 1 || events[0].(*a2a.TaskStatusUpdateEvent).Status.State != a2a.TaskStateCanceled {
		t.Fatalf("cancel events = %#v", events)
	}
}

func TestQuestionReplyUsesStructuredAnswers(t *testing.T) {
	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewDataPart(map[string]any{
		"answers": [][]string{{"one"}, {"two"}},
	}))
	reply := questionReply(message, "", 2)
	if len(reply.Answers) != 2 || reply.Answers[1][0] != "two" {
		t.Fatalf("reply = %#v", reply)
	}
}

func TestExecutorContextSessionIsReused(t *testing.T) {
	rt := &fakeRuntime{eventBatches: [][]controlruntime.Event{
		{{Sequence: 1, Type: controlruntime.EventRunDone, Time: time.Now()}},
		{{Sequence: 1, Type: controlruntime.EventRunDone, Time: time.Now()}},
	}}
	executor := newExecutor(rt)
	for _, taskID := range []a2a.TaskID{"task_1", "task_2"} {
		execCtx := &a2asrv.ExecutorContext{
			TaskID: taskID, ContextID: "shared_context",
			Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hello")),
		}
		executeEvents(t, executor, execCtx)
		rt.runID = ""
	}
	if rt.nextSession != 1 {
		t.Fatalf("created sessions = %d, want 1", rt.nextSession)
	}
}

func TestExecutorContextRetentionEvictsOldestIdleContext(t *testing.T) {
	executor := newExecutor(&fakeRuntime{})
	for i := range maxContexts {
		executor.contextClock++
		id := fmt.Sprintf("context_%03d", i)
		executor.contexts[id] = &contextBinding{sessionID: fmt.Sprintf("session_%03d", i), lastUsed: executor.contextClock}
	}
	reservation, err := executor.reserveContext("context_new", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if reservation.evictedID != "context_000" || reservation.evicted == nil || reservation.evicted.sessionID != "session_000" {
		t.Fatalf("evicted = %q %#v", reservation.evictedID, reservation.evicted)
	}
}

func TestExecutorContextRetentionRotatesOversizedContext(t *testing.T) {
	executor := newExecutor(&fakeRuntime{})
	executor.contexts["context_1"] = &contextBinding{sessionID: "session_old", turns: maxContextTurns, lastUsed: 1}
	reservation, err := executor.reserveContext("context_1", "main_session", 1)
	if err != nil {
		t.Fatal(err)
	}
	if reservation.sessionID != "" || reservation.evictedID != "context_1" || reservation.evicted == nil || reservation.evicted.sessionID != "session_old" {
		t.Fatalf("rotation = session %q, evicted %q %#v", reservation.sessionID, reservation.evictedID, reservation.evicted)
	}
}

func TestExecutorRejectsNewSessionWhenCleanupBacklogIsFull(t *testing.T) {
	executor := newExecutor(&fakeRuntime{})
	for i := range maxContexts {
		executor.pendingSessionDeletes[fmt.Sprintf("session_%03d", i)] = struct{}{}
	}
	if _, err := executor.reserveContext("context_new", "", 1); err == nil {
		t.Fatal("new context succeeded with a full session cleanup backlog")
	}
	if executor.contexts["context_new"] != nil {
		t.Fatal("rejected context was retained")
	}
}

func TestExecutorDoesNotEvictContextWhenRunCannotStart(t *testing.T) {
	rt := &fakeRuntime{startErr: errors.New("runtime busy")}
	executor := newExecutor(rt)
	for i := range maxContexts {
		executor.contextClock++
		id := fmt.Sprintf("context_%03d", i)
		executor.contexts[id] = &contextBinding{sessionID: fmt.Sprintf("session_%03d", i), lastUsed: executor.contextClock}
	}
	execCtx := &a2asrv.ExecutorContext{
		TaskID: "task_new", ContextID: "context_new",
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("work")),
	}
	executeEvents(t, executor, execCtx)
	if executor.contexts["context_000"] == nil {
		t.Fatal("oldest context was evicted even though the run did not start")
	}
	if executor.contexts["context_new"] != nil {
		t.Fatal("failed run retained a new context")
	}
}

func TestExecutorRollsBackReusedContextWhenRunCannotStart(t *testing.T) {
	rt := &fakeRuntime{startErr: errors.New("runtime busy")}
	executor := newExecutor(rt)
	executor.contexts["context_1"] = &contextBinding{
		sessionID: "session_1", lastUsed: 7, turns: 3, inputBytes: 100,
	}
	executeEvents(t, executor, &a2asrv.ExecutorContext{
		TaskID: "task_new", ContextID: "context_1",
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("work")),
	})
	context := executor.contexts["context_1"]
	if context == nil {
		t.Fatal("reused context was removed after a failed start")
	}
	if context.active != 0 || context.turns != 3 || context.inputBytes != 100 || context.lastUsed != 7 {
		t.Fatalf("context after failed start = %#v", context)
	}
}

func TestExecutorSerializesContextReservationAndRunStart(t *testing.T) {
	rt := &fakeRuntime{
		startEntered: make(chan struct{}, 2),
		startRelease: make(chan struct{}, 2),
		eventBatches: [][]controlruntime.Event{
			{{Sequence: 1, Type: controlruntime.EventRunDone}},
			{{Sequence: 1, Type: controlruntime.EventRunDone}},
		},
	}
	executor := newExecutor(rt)
	for i := range maxContexts {
		executor.contextClock++
		id := fmt.Sprintf("context_%03d", i)
		executor.contexts[id] = &contextBinding{sessionID: fmt.Sprintf("session_%03d", i), lastUsed: executor.contextClock}
	}
	run := func(taskID a2a.TaskID, done chan<- struct{}) {
		defer func() { done <- struct{}{} }()
		execCtx := &a2asrv.ExecutorContext{
			TaskID: taskID, ContextID: "shared_new_context",
			Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("work")),
		}
		for range executor.Execute(context.Background(), execCtx) {
		}
	}
	done := make(chan struct{}, 2)
	go run("task_1", done)
	<-rt.startEntered
	go run("task_2", done)
	select {
	case <-rt.startEntered:
		rt.startRelease <- struct{}{}
		rt.startRelease <- struct{}{}
		t.Fatal("second task entered StartRunInSession before the first start completed")
	case <-time.After(50 * time.Millisecond):
	}
	rt.startRelease <- struct{}{}
	<-rt.startEntered
	rt.startRelease <- struct{}{}
	<-done
	<-done
}

func TestExecutorRetriesFailedSessionDeletion(t *testing.T) {
	rt := &fakeRuntime{
		deleteErr: errors.New("session busy"),
		eventBatches: [][]controlruntime.Event{
			{{Sequence: 1, Type: controlruntime.EventRunDone}},
		},
	}
	executor := newExecutor(rt)
	binding := executor.binding("old_task")
	binding.evictedSessionID = "session_old"
	executor.deleteBinding("old_task", binding)
	if _, queued := executor.pendingSessionDeletes["session_old"]; !queued {
		t.Fatal("failed session deletion was not queued")
	}

	rt.mu.Lock()
	rt.deleteErr = nil
	rt.mu.Unlock()
	executeEvents(t, executor, &a2asrv.ExecutorContext{
		TaskID: "new_task", ContextID: "new_context",
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("work")),
	})
	if _, queued := executor.pendingSessionDeletes["session_old"]; queued {
		t.Fatal("successful retry remained queued")
	}
	rt.mu.Lock()
	deletes := append([]string(nil), rt.deletedSessions...)
	rt.mu.Unlock()
	if len(deletes) != 2 || deletes[0] != "session_old" || deletes[1] != "session_old" {
		t.Fatalf("session deletion attempts = %#v, want two attempts", deletes)
	}
}

var _ Runtime = (*fakeRuntime)(nil)
