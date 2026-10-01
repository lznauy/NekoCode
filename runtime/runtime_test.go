package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"nekocode/runtime/internal/core"
)

type testBot struct {
	mu       sync.Mutex
	steers   int
	aborts   int
	run      func(string, RunHost) (string, error)
	command  func(string, RunHost) CommandResult
	menu     func(string) (CommandMenu, bool)
	commands []string
}

func (b *testBot) Run(_ context.Context, input string, host RunHost) (string, error) {
	if b.run != nil {
		return b.run(input, host)
	}
	return "", nil
}

func (b *testBot) ExecuteCommand(_ context.Context, input string, host RunHost) (CommandResult, error) {
	if b.command != nil {
		return b.command(input, host), nil
	}
	return CommandResult{Action: CommandIgnored}, nil
}

func (b *testBot) Metrics() MetricsSnapshot { return MetricsSnapshot{} }

func (b *testBot) CommandMenu(_ context.Context, input string) (CommandMenu, bool) {
	if input == "/" && len(b.commands) > 0 {
		items := make([]CommandMenuItem, 0, len(b.commands))
		for _, name := range b.commands {
			items = append(items, CommandMenuItem{Value: commandDisplayForTest(name), Label: commandDisplayForTest(name)})
		}
		return CommandMenu{Title: "Commands", Items: items}, true
	}
	if b.menu == nil {
		return CommandMenu{}, false
	}
	return b.menu(input)
}

func commandDisplayForTest(name string) string {
	if len(name) > 0 && (name[0] == '/' || name[0] == '$') {
		return name
	}
	return "/" + name
}

func (b *testBot) Steer(context.Context, string) error {
	b.mu.Lock()
	b.steers++
	b.mu.Unlock()
	return nil
}

func (b *testBot) Abort() {
	b.mu.Lock()
	b.aborts++
	b.mu.Unlock()
}

func (b *testBot) Close() error { return nil }

func (b *testBot) CurrentModel() ModelSelection { return ModelSelection{} }

func (b *testBot) SessionMessages() []DisplayMessage { return nil }

func (b *testBot) steerCount() int { b.mu.Lock(); defer b.mu.Unlock(); return b.steers }

func (b *testBot) abortCount() int { b.mu.Lock(); defer b.mu.Unlock(); return b.aborts }

func newTestRuntime(b *testBot) *Runtime {
	return New(b, testBotServices(b))
}

func testBotServices(b *testBot) Services {
	return Services{
		ExecuteCommand:          b.ExecuteCommand,
		CommandMenu:             b.CommandMenu,
		Steer:                   b.Steer,
		Metrics:                 b.Metrics,
		ImageAttachmentsEnabled: func() bool { return true },
		Close:                   b.Close,
	}
}

func sessionRunnerServices(r *sessionCommandRunner) Services {
	services := testBotServices(&r.testBot)
	services.CurrentSessionID = r.CurrentSessionID
	services.ListSessions = r.ListSessions
	services.SessionMessages = r.SessionMessages
	services.ResumeSession = r.ResumeSession
	services.NewSession = r.NewSession
	services.DeleteSession = r.DeleteSession
	return services
}

type statusPublishingConnector struct {
	rt core.ConnectorRuntime
}

type sessionCommandRunner struct {
	testBot
	current string
}

func (r *sessionCommandRunner) CurrentSessionID() string { return r.current }

func (r *sessionCommandRunner) ResumeSession(id string) error {
	r.current = id
	return nil
}

func (r *sessionCommandRunner) ListSessions() []SessionMeta { return nil }

func (r *sessionCommandRunner) NewSession() (SessionMeta, error) {
	r.current = "session_new"
	return SessionMeta{ID: r.current}, nil
}

func (r *sessionCommandRunner) DeleteSession(id string) error {
	if r.current == id {
		r.current = ""
	}
	return nil
}

type failingCloseRunner struct {
	testBot
	err error
}

func (r *failingCloseRunner) Close() error { return r.err }

type modelMutationRunner struct {
	testBot
	switches int
}

type closeBarrierRunner struct {
	started chan struct{}
	release chan struct{}
	closed  chan struct{}
}

func (r *closeBarrierRunner) Run(context.Context, string, RunHost) (string, error) {
	close(r.started)
	<-r.release
	return "", nil
}

func (r *closeBarrierRunner) Close() error {
	close(r.closed)
	return nil
}

type callbackSteererRunner struct {
	host         RunHost
	started      chan struct{}
	steerStarted chan struct{}
	invoke       chan struct{}
}

func (r *callbackSteererRunner) Run(ctx context.Context, _ string, host RunHost) (string, error) {
	r.host = host
	close(r.started)
	<-ctx.Done()
	return "", ctx.Err()
}

func (r *callbackSteererRunner) Steer(context.Context, string) error {
	close(r.steerStarted)
	<-r.invoke
	r.host.Text("steered")
	return nil
}

type cancelAwareSteererRunner struct {
	started      chan struct{}
	steerStarted chan struct{}
}

func (r *cancelAwareSteererRunner) Run(ctx context.Context, _ string, _ RunHost) (string, error) {
	close(r.started)
	<-ctx.Done()
	return "", ctx.Err()
}

func (r *cancelAwareSteererRunner) Steer(ctx context.Context, _ string) error {
	close(r.steerStarted)
	<-ctx.Done()
	return ctx.Err()
}

func (r *modelMutationRunner) SwitchModel(name string) (ModelSelection, error) {
	r.switches++
	return ModelSelection{Model: name}, nil
}

func (c statusPublishingConnector) Name() string { return "telegram" }

func (c statusPublishingConnector) Start(context.Context) error {
	return nil
}

func (c statusPublishingConnector) Stop() error {
	c.rt.ReportConnectorStatus(ConnectorStatusPayload{
		Name:    "telegram",
		Status:  "stopped",
		Message: "Telegram connector stopped.",
	})
	return nil
}

func (c statusPublishingConnector) HandleCommand(context.Context, []string) (string, error) {
	return "connected", nil
}

func waitForStatus(t *testing.T, rt *Runtime, status RunStatus) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if runtimeStatus(rt) == status {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for status %s, got %s", status, runtimeStatus(rt))
}

func runtimeStatus(rt *Runtime) RunStatus {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.status
}

func TestManagerCloseIsIdempotent(t *testing.T) {
	rt := newTestRuntime(&testBot{})
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestManagerReturnsRunnerCloseError(t *testing.T) {
	closeErr := errors.New("runner close failed")
	runner := &failingCloseRunner{err: closeErr}
	rt := New(runner, Services{Close: runner.Close})
	if err := rt.Close(); !errors.Is(err, closeErr) {
		t.Fatalf("Close error = %v, want %v", err, closeErr)
	}
	if err := rt.Close(); !errors.Is(err, closeErr) {
		t.Fatalf("second Close error = %v, want %v", err, closeErr)
	}
}

func TestManagerCloseWaitsForRunnerExit(t *testing.T) {
	runner := &closeBarrierRunner{
		started: make(chan struct{}),
		release: make(chan struct{}),
		closed:  make(chan struct{}),
	}
	rt := New(runner, Services{Close: runner.Close})
	if _, err := rt.StartRun(context.Background(), Input{Source: SourceRef{Kind: "test"}, Text: "run"}); err != nil {
		t.Fatal(err)
	}
	<-runner.started
	done := make(chan error, 1)
	go func() { done <- rt.Close() }()

	select {
	case <-runner.closed:
		t.Fatal("runner closed before active Run returned")
	case <-time.After(20 * time.Millisecond):
	}
	close(runner.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not finish after runner returned")
	}
}

func hasString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func waitForRun(t *testing.T, rt *Runtime, runID RunID) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if run, ok := rt.LookupRun(runID); ok && (run.Status == RunDone || run.Status == RunFailed || run.Status == RunCancelled) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	run, _ := rt.LookupRun(runID)
	t.Fatalf("timed out waiting for run %s to finish: %#v", runID, run)
}
