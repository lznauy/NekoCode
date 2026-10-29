package runtime

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	tools "nekocode/bot/extension/tool"
	toolcore "nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/bot/extension/tool/runtime/runner"
)

type approvalObserverFixture struct{}

func (approvalObserverFixture) Name() string                     { return "shell" }
func (approvalObserverFixture) Description() string              { return "fixture" }
func (approvalObserverFixture) Parameters() []toolcore.Parameter { return nil }
func (approvalObserverFixture) ExecutionMode(map[string]any) toolcore.ExecutionMode {
	return toolcore.ModeSequential
}
func (approvalObserverFixture) Execute(context.Context, map[string]any) (toolcore.ToolOutput, error) {
	return toolcore.ToolOutput{}, fmt.Errorf("rejected tool should not execute")
}

func TestRuntimeApprovalObserverDistinguishesCancellation(t *testing.T) {
	for _, mode := range []string{"user", "cancel", "close", "context"} {
		t.Run(mode, func(t *testing.T) {
			parent, cancel := context.WithCancel(t.Context())
			defer cancel()
			var mu sync.Mutex
			userDenials := 0
			executor := runner.NewExecutor(tools.New(approvalObserverFixture{}))
			executor.SetPermissionPolicy(permission.PermissionsDecl{Ask: []string{"shell"}}, t.TempDir(), t.TempDir())
			executor.SetPermissionObserver(func(_ context.Context, event runner.PermissionEvent) {
				mu.Lock()
				defer mu.Unlock()
				if event.Denied && event.Args["source"] == "user" {
					userDenials++
				}
			})
			runtime := New(RunnerFunc(func(ctx context.Context, _ string, host RunHost) (string, error) {
				executor.SetConfirmFn(host.Confirm)
				executor.ExecuteBatch(ctx, []toolcore.ToolCallItem{{ID: "call", Name: "shell", Args: map[string]any{"command": "fixture"}}})
				return "", nil
			}), ServiceGroups{})
			defer runtime.Close()
			events, err := runtime.Events(t.Context(), EventFilter{Reliable: true, Types: []EventType{EventApprovalRequested}})
			if err != nil {
				t.Fatal(err)
			}
			id, err := runtime.StartRun(parent, Input{Text: "run"})
			if err != nil {
				t.Fatal(err)
			}
			var request ApprovalView
			select {
			case event := <-events:
				request = event.Payload.(ApprovalView)
			case <-time.After(3 * time.Second):
				t.Fatal("no approval request")
			}
			switch mode {
			case "user":
				if err := runtime.DecideApproval(t.Context(), request.ID, ApprovalDecision{}); err != nil {
					t.Fatal(err)
				}
			case "cancel":
				if err := runtime.CancelRun(t.Context(), id); err != nil {
					t.Fatal(err)
				}
			case "close":
				if err := runtime.Close(); err != nil {
					t.Fatal(err)
				}
			case "context":
				cancel()
				// Adapters cancel the active run when their outer context ends.
				if err := runtime.CancelRun(t.Context(), id); err != nil {
					t.Fatal(err)
				}
			}
			if err := runtime.WaitRun(t.Context(), id); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			got := userDenials
			mu.Unlock()
			want := 0
			if mode == "user" {
				want = 1
			}
			if got != want {
				t.Fatalf("%s: user denials=%d want=%d", mode, got, want)
			}
		})
	}
}
