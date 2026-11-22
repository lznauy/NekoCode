package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"nekocode/runtime/internal/broker"
	"nekocode/runtime/internal/recording"
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
	}), ServiceGroups{})

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
	rt := New(runner, ServiceGroups{
		Commands: CommandsServices{
			Steer: runner.Steer,
		},
	})
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
	rt := New(runner, ServiceGroups{
		Commands: CommandsServices{
			Steer: func(_ context.Context, message string) error { steered <- message; return nil },
		},
		Models: ModelsServices{
			ImageAttachmentsEnabled: func() bool { return true },
			VisionEnabled:           func() bool { return true },
		},
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
	rt := New(runner, ServiceGroups{
		Commands: CommandsServices{
			Steer: runner.Steer,
		},
	})
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
	rt := New(runner, ServiceGroups{
		Commands: CommandsServices{
			Steer: runner.Steer,
		},
	})
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
	rt := New(runner, ServiceGroups{
		Lifecycle: LifecycleServices{
			Close: runner.Close,
		},
	})
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

func TestSuperAgentSteerBindsExplicitRun(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	release, started := make(chan struct{}), make(chan struct{})
	rt := New(RunnerFunc(func(ctx context.Context, input string, _ RunHost) (string, error) {
		if input == "active" {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		return input, nil
	}), backgroundServices(t.TempDir()))
	defer rt.Close()
	run, err := rt.StartRun(ctx, Input{Text: "active"})
	if err != nil {
		t.Fatal(err)
	}
	awaitBackground(t, started)
	// WaitRun settles the demand, while the background coordinator can still
	// briefly hold the admission gate to check whether another demand is ready.
	steer := func(id RunID, text string) error {
		for {
			err := rt.SteerRun(ctx, id, Input{Text: text})
			var target *ProtocolError
			if !errors.As(err, &target) || target.Code != ErrorBusy {
				return err
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Millisecond):
			}
		}
	}
	assertConflict := func(id RunID) {
		t.Helper()
		err := steer(id, "wrong target")
		var target *ProtocolError
		if !errors.As(err, &target) || target.Code != ErrorConflict {
			t.Fatalf("steer %s: %v", id, err)
		}
	}
	assertConflict("different")
	if err := steer(run, "valid next demand"); err != nil {
		t.Fatal(err)
	}
	demands, err := rt.BackgroundDemands("session")
	if err != nil || len(demands) != 1 || demands[0].Input.Text != "valid next demand" {
		t.Fatalf("unexpected queued demands: %+v, %v", demands, err)
	}
	close(release)
	if err := rt.WaitRun(ctx, demands[0].ID); err != nil {
		t.Fatal(err)
	}
	assertConflict(demands[0].ID)
}

func TestToolCancellationReleasesOnlyItsInteraction(t *testing.T) {
	bot := &testBot{}
	hosts := make(chan RunHost, 1)
	release := make(chan struct{})
	defer close(release)
	bot.run = func(_ string, host RunHost) (string, error) {
		hosts <- host
		<-release
		return "done", nil
	}
	rt := newTestRuntime(bot)
	runID, err := rt.StartRun(t.Context(), Input{Source: SourceRef{Kind: "test"}, Text: "run"})
	if err != nil {
		t.Fatal(err)
	}
	host := <-hosts
	confirm := func(name string, done <-chan struct{}) <-chan ConfirmReply {
		replies := make(chan ConfirmReply, 1)
		go func() { replies <- host.Confirm(ConfirmRequest{ToolName: "shell", SubAgentID: name, Done: done}) }()
		return replies
	}
	requireCancelled := func(replies <-chan ConfirmReply) {
		t.Helper()
		select {
		case reply := <-replies:
			if !reply.Cancelled || reply.Allowed {
				t.Fatalf("approval cancellation returned %+v", reply)
			}
		case <-time.After(time.Second):
			t.Fatal("tool cancellation left approval blocked")
		}
	}

	active, cancelActive := context.WithCancel(t.Context())
	defer cancelActive()
	pending := confirm("active", active.Done())
	waitForStatus(t, rt, RunWaitingApproval)
	queued, cancelQueued := context.WithCancel(t.Context())
	waiter := confirm("queued", queued.Done())
	cancelQueued()
	requireCancelled(waiter)
	queue := rt.events.History(EventFilter{RunID: runID, Types: []EventType{EventInteractionQueue}})
	if len(queue) != 2 {
		t.Fatalf("queue boundaries: %+v", queue)
	}
	start, end := queue[0].Payload.(InteractionQueuePayload), queue[1].Payload.(InteractionQueuePayload)
	if start.SubAgentID != "queued" || !start.Waiting || end.Waiting || start.ID != end.ID {
		t.Fatalf("queue boundaries %v %v", start, end)
	}
	records := rt.approvals.Pending()
	ask := func(done <-chan struct{}) <-chan QuestionReply {
		replies := make(chan QuestionReply, 1)
		go func() { replies <- host.Ask(QuestionRequest{SubAgentID: "child", Done: done}) }()
		return replies
	}
	requireRejected := func(replies <-chan QuestionReply) {
		t.Helper()
		select {
		case reply := <-replies:
			if !reply.Rejected {
				t.Fatal("cancelled question accepted")
			}
		case <-time.After(time.Second):
			t.Fatal("question did not release on tool cancellation")
		}
	}
	requireRejected(ask(queued.Done()))
	if len(records) != 1 || records[0].SubAgentID != "active" {
		t.Fatalf("queued cancellation disturbed pending approval: %+v", records)
	}
	cancelActive()
	requireCancelled(pending)
	resolved := rt.events.History(EventFilter{RunID: runID, Types: []EventType{EventApprovalResolved}})
	if len(resolved) != 1 || resolved[0].Payload.(ApprovalView).Status != ApprovalExpired {
		t.Fatalf("cancelled approval recorded as a decision: %+v", resolved)
	}
	if len(rt.approvals.Pending()) != 0 {
		t.Fatal("cancelled tool left a pending approval")
	}
	if err := rt.DecideApproval(t.Context(), records[0].ID, ApprovalDecision{Allowed: true}); err == nil {
		t.Fatal("late approval accepted after tool cancellation")
	}
	if events := rt.events.History(EventFilter{RunID: runID, Types: []EventType{EventApprovalRequested}}); len(events) != 1 {
		t.Fatalf("queued tool published an approval after cancellation: %v", events)
	}
	question, cancelQuestion := context.WithCancel(t.Context())
	defer cancelQuestion()
	answer := ask(question.Done())
	waitForStatus(t, rt, RunWaitingQuestion)
	cancelQuestion()
	requireRejected(answer)
	if len(rt.questions.Pending()) != 0 {
		t.Fatal("cancelled tool left a pending question")
	}
	if events := rt.events.History(EventFilter{RunID: runID, Types: []EventType{EventQuestionRequested}}); len(events) != 1 {
		t.Fatalf("queued question was published after cancellation: %v", events)
	}
}

func TestQueuedChildReleasesInteractionSlot(t *testing.T) {
	rt := newTestRuntime(&testBot{})
	defer rt.Close()
	host := runHost{runtime: rt, runID: "run", lease: newRunLease()}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	first, ok := host.acquireInteraction("first", ctx.Done())
	if !ok {
		t.Fatal("first child could not acquire the slot")
	}
	var events []InteractionQueuePayload
	rt.events.AddObserver(func(e Event) {
		if e.Type != EventInteractionQueue {
			return
		}
		event := e.Payload.(InteractionQueuePayload)
		events = append(events, event)
		if len(events) > 2 {
			t.Fatalf("duplicate queue event: %+v", event)
		}
		if event.Waiting {
			// Release the first child only after the second has queued.
			first()
		} else {
			// A synchronous observer of the end must be able to take the slot.
			next, ok := host.acquireInteraction("", ctx.Done())
			if !ok {
				t.Fatal("queue end observer could not take the released slot")
			}
			next()
		}
	})
	second, ok := host.acquireInteraction("second", ctx.Done())
	if !ok {
		t.Fatal("queued child could not acquire the slot")
	}
	second()

	third, ok := host.acquireInteraction("", ctx.Done())
	if !ok {
		t.Fatal("queued child did not release the slot")
	}
	third()
	if len(events) != 2 || events[0].SubAgentID != "second" || !events[0].Waiting {
		t.Fatalf("unexpected queue events: %+v", events)
	}
	wantEnd := events[0]
	wantEnd.Waiting = false
	if events[1] != wantEnd {
		t.Fatalf("queue end = %+v, want %+v", events[1], wantEnd)
	}
}

func TestCancelDuringInteractionRegistration(t *testing.T) {
	for _, kind := range []string{"approval", "question"} {
		t.Run(kind, func(t *testing.T) {
			entered, resume := make(chan struct{}), make(chan struct{})
			replied := make(chan bool, 1)
			rt := New(RunnerFunc(func(_ context.Context, _ string, host RunHost) (string, error) {
				if kind == "approval" {
					replied <- host.Confirm(ConfirmRequest{ToolName: "shell"}).Cancelled
				} else {
					replied <- host.Ask(QuestionRequest{}).Rejected
				}
				return "", nil
			}), ServiceGroups{})
			defer rt.Close()
			// Registration has entered the run lease, but the broker has not
			// inserted the request into its pending map yet.
			current := func() RunID {
				close(entered)
				<-resume
				return rt.currentRunID()
			}
			rt.approvals = broker.NewApprovalBroker(rt.events, SourceRef{}, current)
			rt.questions = broker.NewQuestionBroker(rt.events, SourceRef{}, current)
			run, err := rt.StartRun(t.Context(), Input{Text: "ask"})
			if err != nil {
				t.Fatal(err)
			}
			<-entered
			ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
			err = rt.CancelRun(ctx, run)
			cancel()
			close(resume)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("cancel = %v", err)
			}
			select {
			case rejected := <-replied:
				if !rejected {
					t.Fatal("late registration accepted after cancellation")
				}
			case <-time.After(time.Second):
				rt.approvals.RejectRun(run)
				rt.questions.RejectRun(run)
				t.Fatal("cancel waited for a runner that was still waiting for its interaction")
			}
			if err := rt.WaitRun(t.Context(), run); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestConcurrentChildApprovalsAndQuestions(t *testing.T) {
	const children = 16
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	rt := New(RunnerFunc(func(ctx context.Context, _ string, host RunHost) (string, error) {
		replies := make(chan bool, children)
		for i := range children {
			go func() {
				child := fmt.Sprintf("child-%d", i)
				if i%2 == 0 {
					replies <- host.Confirm(ConfirmRequest{SubAgentID: child, ToolName: "shell", Done: ctx.Done()}).Allowed
				} else {
					replies <- !host.Ask(QuestionRequest{SubAgentID: child, Done: ctx.Done()}).Rejected
				}
			}()
		}
		for range children {
			if !<-replies {
				return "", errors.New("child interaction rejected")
			}
		}
		return "done", nil
	}), ServiceGroups{})
	defer rt.Close()
	if err := rt.EnableEventRecording(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	stream, err := rt.events.Subscribe(ctx, EventFilter{Types: []EventType{
		EventInteractionQueue, EventApprovalRequested, EventQuestionRequested, EventApprovalResolved, EventQuestionResolved, EventRunDone, EventRunFailed,
	}})
	if err != nil {
		t.Fatal(err)
	}
	runID, err := rt.StartRun(ctx, Input{Text: "parallel interactions"})
	if err != nil {
		t.Fatal(err)
	}

	// Hold the first reply until every other child has queued.
	queued := map[uint64]string{}
	starts, ends, resolved := 0, 0, 0
	var active string
	var answer func() error
	for event := range stream {
		switch event.Type {
		case EventInteractionQueue:
			p := event.Payload.(InteractionQueuePayload)
			if p.Waiting {
				if _, found := queued[p.ID]; found {
					t.Fatalf("duplicate start: %+v", p)
				}
				queued[p.ID] = p.SubAgentID
				starts++
			} else {
				if queued[p.ID] != p.SubAgentID || p.SubAgentID == "" {
					t.Fatalf("unmatched end: %+v", p)
				}
				delete(queued, p.ID)
				ends++
			}
		case EventApprovalRequested, EventQuestionRequested:
			if active != "" {
				t.Fatalf("new request before resolution of %s", active)
			}
			switch p := event.Payload.(type) {
			case ApprovalView:
				active = p.ID
				answer = func() error { return rt.DecideApproval(ctx, p.ID, ApprovalDecision{Allowed: true}) }
			case QuestionView:
				active = p.ID
				answer = func() error { return rt.AnswerQuestion(ctx, p.ID, QuestionReply{}) }
			}
		case EventApprovalResolved, EventQuestionResolved:
			var id string
			switch p := event.Payload.(type) {
			case ApprovalView:
				id = p.ID
			case QuestionView:
				id = p.ID
			}
			if id != active {
				t.Fatalf("resolved %q, active %q", id, active)
			}
			active = ""
			resolved++
		case EventRunDone:
			if starts != children-1 || ends != starts || len(queued) != 0 || resolved != children || active != "" {
				t.Fatalf("starts=%d ends=%d resolved=%d active=%q queued=%v", starts, ends, resolved, active, queued)
			}
			if err := rt.WaitRun(ctx, runID); err != nil {
				t.Fatal(err)
			}
			rt.mu.Lock()
			path := rt.archivedRuns[runID].Path
			rt.mu.Unlock()
			var recorded int
			if err := recording.WalkRecordedEventFile(path, func(ev Event) {
				if ev.Type == EventInteractionQueue {
					recorded++
				}
			}); err != nil {
				t.Fatal(err)
			}
			if recorded != starts+ends {
				t.Fatalf("journal has %d queue events, observed %d", recorded, starts+ends)
			}
			return
		case EventRunFailed:
			t.Fatalf("run failed: %+v", event.Payload)
		}
		if starts >= children-1 && answer != nil {
			if err := answer(); err != nil {
				t.Fatal(err)
			}
			answer = nil
		}
	}
	t.Fatalf("stream closed before all children finished: %v", ctx.Err())
}
