package runtime

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type contextRunner struct {
	testBot
	snapshot ContextSnapshot
	memory   MemoryView
}

func (b *contextRunner) ContextSnapshot() ContextSnapshot  { return b.snapshot }
func (b *contextRunner) MemoryView(MemoryScope) MemoryView { return b.memory }

func TestManagerUsesExplicitRunnerCapabilities(t *testing.T) {
	runner := &contextRunner{
		snapshot: ContextSnapshot{Budget: 100, Used: 40},
		memory:   MemoryView{Scope: MemoryScopeProject, Path: "/tmp/memory.md"},
	}
	rt := New(runner, Services{ContextSnapshot: runner.ContextSnapshot, MemoryView: runner.MemoryView})

	if !rt.Capabilities().Context {
		t.Fatal("context capability not discovered")
	}
	if got := rt.ContextSnapshot(); got.Budget != 100 || got.Used != 40 {
		t.Fatalf("ContextSnapshot = %#v", got)
	}
	if got := rt.MemoryView(MemoryScopeProject); got.Path != "/tmp/memory.md" {
		t.Fatalf("MemoryView = %#v", got)
	}
}

func TestManagerReportsUnsupportedOptionalCapabilities(t *testing.T) {
	rt := New(RunnerFunc(func(_ context.Context, _ string, _ RunHost) (string, error) {
		return "", nil
	}), Services{})

	if rt.Capabilities().Models {
		t.Fatal("unexpected model capability")
	}
	if rt.Capabilities().Sessions {
		t.Fatal("unexpected session capability")
	}
	if got := rt.CurrentModel(); got != (ModelSelection{}) {
		t.Fatalf("CurrentModel = %#v, want zero value", got)
	}
	if got := rt.SessionMessages(); got != nil {
		t.Fatalf("SessionMessages = %#v, want nil", got)
	}
}

func TestNewRejectsNilRunner(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("New(nil) did not panic")
		}
	}()
	New(nil, Services{})
}

type modelServiceRunner struct{ testBot }

func (*modelServiceRunner) CurrentModel() ModelSelection {
	return ModelSelection{Model: "runner-method"}
}

func TestOptionalServicesRequireExplicitComposition(t *testing.T) {
	runner := &modelServiceRunner{}
	plain := New(runner, Services{})
	if plain.Capabilities().Models || plain.CurrentModel() != (ModelSelection{}) {
		t.Fatal("New must not discover optional services from runner methods")
	}

	explicit := New(runner, Services{
		CurrentModel: func() ModelSelection { return ModelSelection{Model: "explicit"} },
	})
	if !explicit.Capabilities().Models || explicit.CurrentModel().Model != "explicit" {
		t.Fatal("New did not expose the supplied model service")
	}
}

func TestNewRejectsIncompleteServiceGroups(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("New accepted an incomplete context service group")
		}
	}()
	New(&modelServiceRunner{}, Services{ContextSnapshot: func() ContextSnapshot { return ContextSnapshot{} }})
}

type reentrantModelMutator struct {
	testBot
	runtime *Runtime
	called  chan struct{}
}

type closeDuringMutationRunner struct {
	testBot
	started chan struct{}
	release chan struct{}
	closed  chan struct{}
}

func (r *closeDuringMutationRunner) SwitchModel(name string) (ModelSelection, error) {
	close(r.started)
	<-r.release
	return ModelSelection{Model: name}, nil
}

func (r *closeDuringMutationRunner) Close() error {
	close(r.closed)
	return nil
}

func (r *reentrantModelMutator) SwitchModel(name string) (ModelSelection, error) {
	_ = r.runtime.Status()
	close(r.called)
	return ModelSelection{Model: name}, nil
}

func TestMutationDoesNotHoldRuntimeStateLockAcrossRunnerCall(t *testing.T) {
	runner := &reentrantModelMutator{called: make(chan struct{})}
	rt := New(runner, Services{SwitchModel: runner.SwitchModel})
	runner.runtime = rt

	done := make(chan error, 1)
	go func() {
		_, err := rt.SwitchModel("next")
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("SwitchModel: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("SwitchModel deadlocked when mutator queried runtime status")
	}
}

func TestInteractionResolutionHonorsCancelledContext(t *testing.T) {
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) {
		return "", nil
	}), Services{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := rt.DecideApproval(ctx, "approval", ApprovalDecision{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("DecideApproval error = %v, want context.Canceled", err)
	}
	if err := rt.AnswerQuestion(ctx, "question", QuestionReply{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("AnswerQuestion error = %v, want context.Canceled", err)
	}
}

func TestCloseWaitsForActiveMutation(t *testing.T) {
	runner := &closeDuringMutationRunner{
		started: make(chan struct{}),
		release: make(chan struct{}),
		closed:  make(chan struct{}),
	}
	rt := New(runner, Services{SwitchModel: runner.SwitchModel, Close: runner.Close})
	mutationDone := make(chan error, 1)
	go func() {
		_, err := rt.SwitchModel("next")
		mutationDone <- err
	}()
	<-runner.started

	closeDone := make(chan error, 1)
	go func() { closeDone <- rt.Close() }()
	select {
	case <-runner.closed:
		t.Fatal("runner closed while mutation was still active")
	case <-time.After(50 * time.Millisecond):
	}

	close(runner.release)
	if err := <-mutationDone; err != nil {
		t.Fatalf("SwitchModel: %v", err)
	}
	if err := <-closeDone; err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case <-runner.closed:
	default:
		t.Fatal("runner was not closed after mutation completed")
	}
}

type liveMetricsRunner struct {
	started     chan struct{}
	release     chan struct{}
	releaseOnce sync.Once
	live        atomic.Int64 // packed live metrics marker, 0 = default snapshot
}

func newLiveMetricsRunner() *liveMetricsRunner {
	return &liveMetricsRunner{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (r *liveMetricsRunner) Run(ctx context.Context, _ string, _ RunHost) (string, error) {
	close(r.started)
	select {
	case <-r.release:
		return "done", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (r *liveMetricsRunner) Metrics() MetricsSnapshot {
	if marker := r.live.Load(); marker != 0 {
		return MetricsSnapshot{ContextTokens: int(marker), ContextBudget: 9000}
	}
	return MetricsSnapshot{TurnPrompt: 12, TurnCompletion: 3}
}

// setLive overrides the runner's live snapshot for the remainder of the test.
func (r *liveMetricsRunner) setLive(snapshot MetricsSnapshot) {
	r.live.Store(int64(snapshot.ContextTokens))
}

func (r *liveMetricsRunner) unblock() {
	r.releaseOnce.Do(func() { close(r.release) })
}

func TestCancelledRunPublishesFinalMetricsBeforeTerminal(t *testing.T) {
	runner := newLiveMetricsRunner()
	rt := New(runner, Services{Metrics: runner.Metrics})
	runID, err := rt.StartRun(context.Background(), Input{Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	<-runner.started
	if err := rt.CancelRun(context.Background(), runID); err != nil {
		t.Fatal(err)
	}
	waitForRun(t, rt, runID)

	events := rt.events.History(EventFilter{RunID: runID})
	if len(events) < 2 || events[len(events)-2].Type != EventMetricsUpdated || events[len(events)-1].Type != EventRunCancelled {
		t.Fatalf("terminal events = %+v, want final metrics then cancellation", events)
	}
}

func TestMetricsUpdatedPublishedWhileRunActive(t *testing.T) {
	runner := newLiveMetricsRunner()
	rt := New(runner, Services{Metrics: runner.Metrics})
	t.Cleanup(func() {
		runner.unblock()
		if err := rt.Close(); err != nil {
			t.Error(err)
		}
	})

	events, err := rt.Events(context.Background(), EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	runID, err := rt.StartRun(context.Background(), Input{Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	<-runner.started

	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	started := false
	for {
		select {
		case event := <-events:
			if event.RunID != runID {
				continue
			}
			if event.Type == EventRunStarted {
				started = true
				continue
			}
			if event.Type != EventMetricsUpdated {
				continue
			}
			if !started {
				t.Fatal("metrics_updated arrived before run_started")
			}
			metrics, ok := event.Payload.(MetricsSnapshot)
			if !ok {
				t.Fatalf("metrics payload type = %T", event.Payload)
			}
			if metrics.TurnPrompt != 12 || metrics.TurnCompletion != 3 {
				t.Fatalf("metrics payload = %+v", metrics)
			}
			if status := rt.Status().RunStatus; status != RunRunning {
				t.Fatalf("metrics arrived after run stopped: status = %s", status)
			}
			runner.unblock()
			waitForRun(t, rt, runID)
			return
		case <-timer.C:
			t.Fatal("no metrics_updated event while run was active")
		}
	}
}

func TestMetricsPrefersLiveServiceOverCache(t *testing.T) {
	runner := newLiveMetricsRunner()
	rt := New(runner, Services{Metrics: runner.Metrics})
	t.Cleanup(func() {
		runner.unblock()
		if err := rt.Close(); err != nil {
			t.Error(err)
		}
	})
	runID, err := rt.StartRun(context.Background(), Input{Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	<-runner.started
	runner.unblock()
	waitForRun(t, rt, runID)

	// The live service changes its answer after the run settled; Metrics()
	// must report the live value, not the cached event snapshot, so session
	// switches cannot leak the previous session's usage.
	runner.setLive(MetricsSnapshot{ContextTokens: 777, ContextBudget: 9000})
	if got := rt.Metrics(); got.ContextTokens != 777 || got.ContextBudget != 9000 {
		t.Fatalf("Metrics() = %+v, want live snapshot", got)
	}
}

func TestToolNamesDoesNotCallServiceAfterClose(t *testing.T) {
	calls := 0
	r := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), Services{
		ToolNames: func() []string { calls++; return []string{"read"} },
	})
	if names := r.ToolNames(); len(names) != 1 {
		t.Fatal(names)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if names := r.ToolNames(); names != nil || calls != 1 {
		t.Fatalf("closed runtime called tool service: names=%v calls=%d", names, calls)
	}
}
