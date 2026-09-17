package runner

import (
	"context"
	"encoding/json"
	rt "nekocode/runtime"
	"testing"
	"time"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/protocol"
)

func TestApprovalExcludesRuntimeArgs(t *testing.T) {
	for _, escalation := range []bool{false, true} {
		t.Run(map[bool]string{false: "policy", true: "escalation"}[escalation], func(t *testing.T) {
			args := map[string]any{"prompt": "hello", "_id": "public-id", "_sub_id": "public-sub-id",
				"_preview": "--- before\n+++ after", "_sub_callback": taskbridge.TaskCallback{ID: "child", Callback: func(protocol.StepEvent) {}}}
			called := false
			confirm := func(req protocol.ConfirmRequest) protocol.ConfirmReply {
				called = true
				if _, err := json.Marshal(req); err != nil {
					t.Fatalf("approval cannot be encoded: %v", err)
				}
				if len(req.Args) != 4 || req.Args["_preview"] != args["_preview"] || req.Args["_id"] != "public-id" || req.Args["_sub_id"] != "public-sub-id" {
					t.Fatal(req.Args)
				}
				verifyBrokerPreview(t, req)
				return protocol.Deny()
			}
			tc := core.ToolCallItem{ID: "call", Name: "task", Args: args}
			if escalation {
				tool := &permissionTool{fakeTool: fakeTool{name: "task", mode: core.ModeSequential}}
				tool.req = core.PermissionRequest{Reason: "needs host", Capabilities: []string{core.CapProcessHost}}
				e := newTestExecutor(fakeRegistry{"task": tool})
				e.SetConfirmFn(confirm)
				e.ExecuteBatch(context.Background(), []core.ToolCallItem{tc})
			} else {
				e := &Executor{}
				e.promptConfirm(tc, confirm, nil, permissionDecision{})
			}
			if !called {
				t.Fatal("approval not requested")
			}
			if _, ok := args["_sub_callback"].(taskbridge.TaskCallback); !ok {
				t.Fatal("execution callback removed")
			}
		})
	}
}

func verifyBrokerPreview(t *testing.T, req protocol.ConfirmRequest) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := rt.New(rt.RunnerFunc(func(_ context.Context, _ string, host rt.RunHost) (string, error) {
		host.Confirm(req)
		return "", nil
	}), rt.Services{})
	defer r.Close()
	events, err := r.Events(ctx, rt.EventFilter{Types: []rt.EventType{rt.EventApprovalRequested}, Reliable: true})
	if err != nil {
		t.Fatal(err)
	}
	id, err := r.StartRun(ctx, rt.Input{Text: "test"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-events:
		view, ok := ev.Payload.(rt.ApprovalView)
		if !ok {
			t.Fatalf("payload=%T", ev.Payload)
		}
		if view.Args["_preview"] != req.Args["_preview"] || view.ToConfirmRequest().Args["_preview"] != req.Args["_preview"] {
			t.Fatal("preview lost between producer, broker and UI")
		}
		if _, err := json.Marshal(view); err != nil {
			t.Fatal(err)
		}
		if view.ArgsHash == "" || view.ToolCallHash == "" {
			t.Fatal("missing hash")
		}
		if err := r.DecideApproval(ctx, view.ID, rt.ApprovalDecision{}); err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := r.WaitRun(ctx, id); err != nil {
		t.Fatal(err)
	}
}
