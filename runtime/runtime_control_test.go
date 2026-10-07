package runtime

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRedactInputText(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"/connect telegram token 123:secret-token", "/connect telegram token [redacted]"},
		{"/connect telegram add personal 123:secret-token", "/connect telegram add personal [redacted]"},
		{"/connect telegram add 123:secret-token", "/connect telegram add [redacted]"},
		{"/connect slack token xoxb-secret-token", "/connect slack token [redacted]"},
		{"/connect slack add workspace xoxb-secret-token", "/connect slack add workspace [redacted]"},
		{"/connect wecom add bot-id bot-secret", "/connect wecom add bot-id [redacted]"},
		{"/connect discord token secret-token", "/connect discord token [redacted]"},
		{"/connect telegram status", "/connect telegram status"},
		{"/plugin install https://token:secret@github.com/owner/repo --yes", "/plugin install https://github.com/owner/repo --yes"},
		{"/plugin install token@github.com:owner/repo --yes", "/plugin install github.com:owner/repo --yes"},
		{"hello world", "hello world"},
	}

	for _, tc := range cases {
		if got := RedactInputText(tc.input); got != tc.want {
			t.Fatalf("RedactInputText(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestManagerCancelsCompletedRunContext(t *testing.T) {
	contextDone := make(chan struct{})
	rt := New(RunnerFunc(func(ctx context.Context, _ string, _ RunHost) (string, error) {
		go func() {
			<-ctx.Done()
			close(contextDone)
		}()
		return "done", nil
	}), Services{})

	runID, err := rt.StartRun(context.Background(), Input{
		Source: SourceRef{Kind: "test"}, Text: "run",
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, rt, runID)
	select {
	case <-contextDone:
	case <-time.After(time.Second):
		t.Fatal("completed run context was not cancelled")
	}
}

func TestSteerCallbackDoesNotDeadlockWithCancel(t *testing.T) {
	runner := &callbackSteererRunner{
		started:      make(chan struct{}),
		steerStarted: make(chan struct{}),
		invoke:       make(chan struct{}),
	}
	rt := New(runner, Services{Steer: runner.Steer})
	runID, err := rt.StartRun(context.Background(), Input{
		Source: SourceRef{Kind: "test"}, Text: "run",
	})
	if err != nil {
		t.Fatal(err)
	}
	<-runner.started

	steered := make(chan error, 1)
	go func() {
		steered <- rt.SteerRun(context.Background(), runID, Input{
			Source: SourceRef{Kind: "test"}, Text: "more",
		})
	}()
	<-runner.steerStarted

	cancelled := make(chan error, 1)
	go func() { cancelled <- rt.CancelRun(context.Background(), runID) }()
	time.Sleep(20 * time.Millisecond)
	close(runner.invoke)

	select {
	case err := <-steered:
		if err != nil {
			t.Fatalf("accepted steering returned an error after cancellation raced: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("SteerRun deadlocked with CancelRun")
	}
	accepted := rt.events.History(EventFilter{RunID: runID, Types: []EventType{EventInputAccepted}})
	steeringEvents := 0
	for _, event := range accepted {
		if payload, ok := event.Payload.(MessagePayload); ok && payload.Content == "more" {
			steeringEvents++
		}
	}
	if steeringEvents != 1 {
		t.Fatalf("accepted steering events = %d, want 1", steeringEvents)
	}
	select {
	case err := <-cancelled:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("CancelRun deadlocked with Steer callback")
	}
	waitForRun(t, rt, runID)
}

func TestSteerEnvelopeNeverClaimsNativeDelivery(t *testing.T) {
	// The steer chain delivers text only; images never become content parts.
	// Even with VisionEnabled the envelope must keep the tool-proxy
	// instruction — the vision variant would tell the model images are
	// already visible when they are not.
	steered := make(chan string, 1)
	runner := &steerCaptureRunner{started: make(chan struct{}), steered: steered}
	rt := New(runner, Services{
		Steer:                   func(_ context.Context, message string) error { steered <- message; return nil },
		ImageAttachmentsEnabled: func() bool { return true },
		VisionEnabled:           func() bool { return true },
	})
	runID, err := rt.StartRun(context.Background(), Input{Source: SourceRef{Kind: "test"}, Text: "run"})
	if err != nil {
		t.Fatal(err)
	}
	<-runner.started

	if err := rt.SteerRun(context.Background(), runID, Input{
		Source: SourceRef{Kind: "test"},
		Text:   "look at [Image #1]",
		Images: []ImageAttachment{{Label: "[Image #1]", Path: "/tmp/paste.png"}},
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case message := <-steered:
		if !strings.Contains(message, "Use the image_understand tool") {
			t.Fatalf("steer envelope lost the tool instruction: %q", message)
		}
		if strings.Contains(message, "delivered natively") {
			t.Fatalf("steer envelope claimed native delivery that never happens: %q", message)
		}
		if !strings.Contains(message, `"path":"/tmp/paste.png"`) {
			t.Fatalf("steer envelope missing image path: %q", message)
		}
	case <-time.After(time.Second):
		t.Fatal("steer was never invoked")
	}
	if err := rt.CancelRun(context.Background(), runID); err != nil {
		t.Fatal(err)
	}
	waitForRun(t, rt, runID)
}

// steerCaptureRunner blocks the run so SteerRun can race it.
type steerCaptureRunner struct {
	started chan struct{}
	steered chan string
}

func (r *steerCaptureRunner) Run(ctx context.Context, _ string, _ RunHost) (string, error) {
	close(r.started)
	<-ctx.Done()
	return "", ctx.Err()
}

func TestCancelInterruptsBlockingSteerer(t *testing.T) {
	runner := &cancelAwareSteererRunner{
		started:      make(chan struct{}),
		steerStarted: make(chan struct{}),
	}
	rt := New(runner, Services{Steer: runner.Steer})
	runID, err := rt.StartRun(context.Background(), Input{
		Source: SourceRef{Kind: "test"}, Text: "run",
	})
	if err != nil {
		t.Fatal(err)
	}
	<-runner.started

	steered := make(chan error, 1)
	go func() {
		steered <- rt.SteerRun(context.Background(), runID, Input{
			Source: SourceRef{Kind: "test"}, Text: "more",
		})
	}()
	<-runner.steerStarted
	if err := rt.CancelRun(context.Background(), runID); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-steered:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("SteerRun error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocking Steerer did not receive run cancellation")
	}
	waitForRun(t, rt, runID)
}

func TestCancelDeadlineDefersTerminalUntilLeaseDrains(t *testing.T) {
	runner := &callbackSteererRunner{
		started:      make(chan struct{}),
		steerStarted: make(chan struct{}),
		invoke:       make(chan struct{}),
	}
	rt := New(runner, Services{Steer: runner.Steer})
	runID, err := rt.StartRun(context.Background(), Input{
		Source: SourceRef{Kind: "test"}, Text: "run",
	})
	if err != nil {
		t.Fatal(err)
	}
	<-runner.started

	steered := make(chan error, 1)
	go func() {
		steered <- rt.SteerRun(context.Background(), runID, Input{
			Source: SourceRef{Kind: "test"}, Text: "more",
		})
	}()
	<-runner.steerStarted

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := rt.CancelRun(ctx, runID); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("CancelRun error = %v, want deadline exceeded", err)
	}
	if events := rt.events.History(EventFilter{
		RunID: runID, Types: []EventType{EventRunCancelled},
	}); len(events) != 0 {
		t.Fatal("cancellation terminal published before lease drained")
	}

	close(runner.invoke)
	<-steered
	waitForRun(t, rt, runID)
	events := rt.events.History(EventFilter{RunID: runID})
	if len(events) == 0 {
		t.Fatal("run produced no events")
	}
	if events[len(events)-1].Type != EventRunCancelled {
		t.Fatalf("last event = %#v, want run cancellation terminal", events[len(events)-1])
	}
}

func TestManagerRejectsMessageWhileWaitingApproval(t *testing.T) {
	bot := &testBot{}
	rt := newTestRuntime(bot)
	bot.run = func(_ string, host RunHost) (string, error) {
		req := ConfirmRequest{
			ToolName: "shell",
			Args:     map[string]any{"command": "go test"},
			Kind:     ConfirmKindPermission,
		}
		reply := host.Confirm(req)
		if !reply.Allowed {
			return "", nil
		}
		return "ok", nil
	}

	runID, err := rt.StartRun(context.Background(), Input{Source: SourceRef{Kind: "test"}, Text: "run command"})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitForStatus(t, rt, RunWaitingApproval)

	gotRunID, err := rt.StartRun(context.Background(), Input{Source: SourceRef{Kind: "test"}, Text: "new text"})
	if err == nil {
		t.Fatal("Submit while waiting approval succeeded")
	}
	if gotRunID != runID {
		t.Fatalf("run id = %q, want %q", gotRunID, runID)
	}
	if bot.steerCount() != 0 {
		t.Fatalf("steer count = %d, want 0", bot.steerCount())
	}

	run, ok := rt.LookupRun(runID)
	if !ok || len(run.Approvals) != 1 {
		t.Fatalf("run approvals = %+v, want one", run.Approvals)
	}
	if err := rt.DecideApproval(context.Background(), run.Approvals[0].ID, ApprovalDecision{Allowed: true}); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	waitForRun(t, rt, runID)
	var resolved, terminal uint64
	for _, event := range rt.events.History(EventFilter{RunID: runID}) {
		switch event.Type {
		case EventApprovalResolved:
			resolved = event.Sequence
		case EventRunDone:
			terminal = event.Sequence
		}
	}
	if resolved == 0 || terminal == 0 || resolved >= terminal {
		t.Fatalf("approval ordering: resolved=%d terminal=%d", resolved, terminal)
	}
}

func TestManagerSerializesParallelInteractions(t *testing.T) {
	bot := &testBot{}
	rt := newTestRuntime(bot)
	bot.run = func(_ string, host RunHost) (string, error) {
		var wg sync.WaitGroup
		for _, tool := range []string{"shell", "write"} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				host.Confirm(ConfirmRequest{
					ToolName: tool,
				})
			}()
		}
		wg.Wait()
		return "done", nil
	}

	runID, err := rt.StartRun(context.Background(), Input{
		Source: SourceRef{Kind: "test"}, Text: "parallel tools",
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, rt, RunWaitingApproval)
	run, ok := rt.LookupRun(runID)
	if !ok || len(run.Approvals) != 1 {
		t.Fatalf("initial approvals = %+v, want exactly one", run.Approvals)
	}
	if err := rt.DecideApproval(context.Background(), run.Approvals[0].ID, ApprovalDecision{Allowed: true}); err != nil {
		t.Fatal(err)
	}

	var secondID string
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		run, _ = rt.LookupRun(runID)
		for _, approval := range run.Approvals {
			if approval.Status == ApprovalPending {
				secondID = approval.ID
			}
		}
		if secondID != "" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if secondID == "" {
		t.Fatalf("second approval was not presented: %+v", run.Approvals)
	}
	if err := rt.DecideApproval(context.Background(), secondID, ApprovalDecision{Allowed: true}); err != nil {
		t.Fatal(err)
	}
	waitForRun(t, rt, runID)
}

func TestManagerAbortPreservesAbortedRunAfterApprovalReturns(t *testing.T) {
	bot := &testBot{}
	rt := newTestRuntime(bot)
	runReturned := make(chan struct{})
	bot.run = func(_ string, host RunHost) (string, error) {
		defer close(runReturned)
		req := ConfirmRequest{
			ToolName: "shell",
			Args:     map[string]any{"command": "go test"},
			Kind:     ConfirmKindPermission,
		}
		reply := host.Confirm(req)
		if reply.Allowed {
			return "approved", nil
		}
		return "rejected", nil
	}

	runID, err := rt.StartRun(context.Background(), Input{Source: SourceRef{Kind: "test"}, Text: "run command"})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitForStatus(t, rt, RunWaitingApproval)
	if err := rt.CancelRun(context.Background(), runID); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	select {
	case <-runReturned:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for bot run to return")
	}
	waitForRun(t, rt, runID)
	run, ok := rt.LookupRun(runID)
	if !ok {
		t.Fatal("run snapshot missing")
	}
	if run.Status != RunCancelled {
		t.Fatalf("run status = %s, want %s; output=%q error=%q", run.Status, RunCancelled, run.Output, run.Error)
	}
	if run.Output != "" {
		t.Fatalf("aborted run output = %q, want empty", run.Output)
	}
}

func TestManagerAbortRejectsPendingApproval(t *testing.T) {
	bot := &testBot{}
	rt := newTestRuntime(bot)
	bot.run = func(_ string, host RunHost) (string, error) {
		req := ConfirmRequest{
			ToolName: "shell",
			Args:     map[string]any{"command": "go test"},
			Kind:     ConfirmKindPermission,
		}
		reply := host.Confirm(req)
		if reply.Allowed {
			return "approved", nil
		}
		return "rejected", nil
	}

	runID, err := rt.StartRun(context.Background(), Input{Source: SourceRef{Kind: "test"}, Text: "run command"})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitForStatus(t, rt, RunWaitingApproval)
	if err := rt.CancelRun(context.Background(), runID); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	waitForRun(t, rt, runID)
	run, ok := rt.LookupRun(runID)
	if !ok || len(run.Approvals) != 1 || run.Approvals[0].Status == ApprovalPending {
		t.Fatalf("approval after abort = %+v, want resolved", run.Approvals)
	}
}

func TestManagerSteerRejectsInactiveRun(t *testing.T) {
	bot := &testBot{}
	rt := newTestRuntime(bot)
	started := make(chan struct{})
	release := make(chan struct{})
	bot.run = func(string, RunHost) (string, error) {
		close(started)
		<-release
		return "ok", nil
	}

	runID, err := rt.StartRun(context.Background(), Input{Source: SourceRef{Kind: "test"}, Text: "start"})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for bot run to start")
	}
	if err := rt.SteerRun(context.Background(), RunID("run_missing"), Input{Source: SourceRef{Kind: "test"}, Text: "stale"}); err == nil {
		t.Fatal("Steer with inactive run succeeded")
	}
	if bot.steerCount() != 0 {
		t.Fatalf("steer count = %d, want 0", bot.steerCount())
	}
	close(release)
	waitForRun(t, rt, runID)
}

func TestManagerAbortRejectsInactiveRun(t *testing.T) {
	bot := &testBot{}
	rt := newTestRuntime(bot)
	started := make(chan struct{})
	release := make(chan struct{})
	bot.run = func(string, RunHost) (string, error) {
		close(started)
		<-release
		return "ok", nil
	}

	runID, err := rt.StartRun(context.Background(), Input{Source: SourceRef{Kind: "test"}, Text: "start"})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for bot run to start")
	}
	if err := rt.CancelRun(context.Background(), RunID("run_missing")); err == nil {
		t.Fatal("Abort with inactive run succeeded")
	}
	if bot.abortCount() != 0 {
		t.Fatalf("abort count = %d, want 0", bot.abortCount())
	}
	close(release)
	waitForRun(t, rt, runID)
}

func TestManagerCloseRejectsPendingApprovalAndBecomesTerminal(t *testing.T) {
	bot := &testBot{}
	replies := make(chan ConfirmReply, 1)
	bot.run = func(_ string, host RunHost) (string, error) {
		reply := host.Confirm(ConfirmRequest{
			ToolName: "shell",
			Args:     map[string]any{"command": "go test"},
		})
		replies <- reply
		return "", nil
	}
	rt := newTestRuntime(bot)

	if _, err := rt.StartRun(context.Background(), Input{Source: SourceRef{Kind: "test"}, Text: "run"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitForStatus(t, rt, RunWaitingApproval)
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}

	select {
	case reply := <-replies:
		if reply.Allowed {
			t.Fatalf("close approval reply = %+v, want rejection", reply)
		}
	case <-time.After(time.Second):
		t.Fatal("close did not release pending approval")
	}
	if _, err := rt.StartRun(context.Background(), Input{Source: SourceRef{Kind: "test"}, Text: "again"}); err == nil {
		t.Fatal("Submit after Close succeeded")
	}
	if _, err := rt.Events(context.Background(), EventFilter{}); err == nil {
		t.Fatal("Subscribe after Close succeeded")
	}
}

func TestRunFinishReleasesConcurrentApproval(t *testing.T) {
	replies := make(chan ConfirmReply, 1)
	started := make(chan struct{})
	bot := &testBot{run: func(_ string, host RunHost) (string, error) {
		go func() {
			close(started)
			replies <- host.Confirm(ConfirmRequest{ToolName: "late"})
		}()
		<-started
		return "done", nil
	}}
	rt := newTestRuntime(bot)
	runID, err := rt.StartRun(context.Background(), Input{Source: SourceRef{Kind: "test"}, Text: "run"})
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, rt, runID)

	select {
	case reply := <-replies:
		if reply.Allowed {
			t.Fatalf("concurrent approval = %+v, want rejection", reply)
		}
	case <-time.After(time.Second):
		t.Fatal("run finish left concurrent approval blocked")
	}
}

func TestManagerDoesNotStartNextRunUntilCancelledRunnerExits(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	bot := &testBot{run: func(_ string, _ RunHost) (string, error) {
		startedOnce.Do(func() { close(started) })
		<-release
		return "", nil
	}}
	rt := newTestRuntime(bot)
	runID, err := rt.StartRun(context.Background(), Input{Source: SourceRef{Kind: "test"}, Text: "first"})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if err := rt.CancelRun(context.Background(), runID); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.StartRun(context.Background(), Input{Source: SourceRef{Kind: "test"}, Text: "second"}); err == nil {
		t.Fatal("second run started before cancelled runner exited")
	}
	close(release)
	waitForStatus(t, rt, RunIdle)
	if _, err := rt.StartRun(context.Background(), Input{Source: SourceRef{Kind: "test"}, Text: "second"}); err != nil {
		t.Fatalf("second run after exit: %v", err)
	}
}

func TestCancelAndClosePreserveCancellationTerminal(t *testing.T) {
	runner := &closeBarrierRunner{
		started: make(chan struct{}),
		release: make(chan struct{}),
		closed:  make(chan struct{}),
	}
	rt := New(runner, Services{Close: runner.Close})
	runID, err := rt.StartRun(context.Background(), Input{Source: SourceRef{Kind: "test"}, Text: "run"})
	if err != nil {
		t.Fatal(err)
	}
	<-runner.started

	publishing := make(chan struct{})
	releasePublish := make(chan struct{})
	rt.events.AddObserver(func(event Event) {
		if event.Type == EventRunCancelled {
			close(publishing)
			<-releasePublish
		}
	})
	cancelled := make(chan error, 1)
	go func() { cancelled <- rt.CancelRun(context.Background(), runID) }()
	waitForStatus(t, rt, RunCancelled)
	close(runner.release)
	<-publishing
	if _, err := rt.StartRun(context.Background(), Input{Source: SourceRef{Kind: "test"}, Text: "next"}); err == nil {
		t.Fatal("new run started before cancellation terminal was published")
	}

	closed := make(chan error, 1)
	go func() { closed <- rt.Close() }()
	select {
	case err := <-closed:
		t.Fatalf("Close returned before cancellation publication: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(releasePublish)
	if err := <-cancelled; err != nil {
		t.Fatal(err)
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}

	run, ok := rt.LookupRun(runID)
	if !ok || run.Status != RunCancelled {
		t.Fatalf("cancelled run snapshot = %#v, ok=%v", run, ok)
	}
}
