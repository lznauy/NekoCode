package task

import (
	"context"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/mode"
	"nekocode/protocol"
	"testing"
)

type submitTestHost struct{}

func (submitTestHost) SubmitBackground(context.Context, protocol.BackgroundWork) (protocol.BackgroundReceipt, error) {
	return protocol.BackgroundReceipt{}, nil
}
func (submitTestHost) CancelBackground(context.Context, string) error { return nil }
func TestSubmitUsesDedicatedExecutorWithoutProfileSelection(t *testing.T) {
	tool := NewSubmitTool()
	hasRequirements := false
	for _, p := range tool.Parameters() {
		if p.Name == "required_tools" && p.Required {
			hasRequirements = true
		}
		if p.Name == "read_only" {
			t.Fatal("ambiguous read_only still exposed")
		}
		if p.Name == "profile" {
			t.Fatal("coordinator still selects legacy profiles")
		}
	}
	if !hasRequirements {
		t.Fatal("required tools are not part of the submission contract")
	}
	definition, _ := mode.Resolve(mode.SuperAgent)
	ctx := protocol.WithBackgroundSubmitter(mode.WithSnapshot(t.Context(), definition.Snapshot), submitTestHost{})
	called := 0
	tool.Wire(func(_ context.Context, spec taskbridge.TaskSpec) (*taskbridge.TaskResult, error) {
		called++
		if spec.Profile != "executor" {
			t.Errorf("wrong worker: %s", spec.Profile)
		}
		return &taskbridge.TaskResult{ID: "task", Status: taskbridge.TaskStatusAccepted}, nil
	})
	if _, err := tool.Execute(ctx, nil); err == nil {
		t.Fatal("missing arguments accepted")
	}
	args := map[string]any{"required_tools": []any{}, "prompt": "inspect"}
	if _, err := tool.Execute(ctx, args); err != nil {
		t.Fatal(err)
	}
	if _, exists := args["profile"]; exists {
		t.Fatal("tool mutated caller arguments")
	}
	for _, profile := range []string{"explore", "coder", "executor", "plugin/custom"} {
		if _, err := tool.Execute(ctx, map[string]any{"required_tools": []any{}, "prompt": "inspect", "profile": profile}); err == nil {
			t.Fatalf("accepted profile override: %s", profile)
		}
	}
	if _, err := tool.Execute(ctx, map[string]any{"required_tools": []any{}, "prompt": "inspect", "read_only": "true"}); err == nil {
		t.Fatal("invalid read_only accepted")
	}
	if called != 1 {
		t.Fatalf("override reached runner: %d", called)
	}
}

func TestSubmitReadOnlyUsesSameExecutor(t *testing.T) {
	definition, _ := mode.Resolve(mode.SuperAgent)
	ctx := protocol.WithBackgroundSubmitter(mode.WithSnapshot(t.Context(), definition.Snapshot), submitTestHost{})
	tool := NewSubmitTool()
	tool.Wire(func(_ context.Context, spec taskbridge.TaskSpec) (*taskbridge.TaskResult, error) {
		if spec.Profile != "executor" || !spec.ReadOnly {
			t.Fatalf("wrong constraint: %+v", spec)
		}
		return &taskbridge.TaskResult{Status: taskbridge.TaskStatusAccepted}, nil
	})
	if _, err := tool.Execute(ctx, map[string]any{"required_tools": []any{}, "prompt": "read", "tool_scope": "read_tools_only"}); err != nil {
		t.Fatal(err)
	}
}

func TestSubmitPassesDependenciesAndRejectsMalformedList(t *testing.T) {
	definition, _ := mode.Resolve(mode.SuperAgent)
	ctx := protocol.WithBackgroundSubmitter(mode.WithSnapshot(t.Context(), definition.Snapshot), submitTestHost{})
	tool := NewSubmitTool()
	calls := 0
	tool.Wire(func(_ context.Context, spec taskbridge.TaskSpec) (*taskbridge.TaskResult, error) {
		calls++
		if len(spec.After) != 2 || spec.After[0] != "A" || spec.After[1] != "B" {
			t.Fatalf("dependencies lost: %+v", spec)
		}
		return &taskbridge.TaskResult{Status: taskbridge.TaskStatusAccepted}, nil
	})
	if _, err := tool.Execute(ctx, map[string]any{"required_tools": []any{}, "prompt": "verify", "after": []any{"A", "B"}}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []any{"A", []any{""}, []any{1}, []any{"1", "2", "3", "4", "5", "6", "7", "8", "9"}} {
		if _, err := tool.Execute(ctx, map[string]any{"required_tools": []any{}, "prompt": "verify", "after": bad}); err == nil {
			t.Fatalf("accepted malformed dependencies: %#v", bad)
		}
	}
	if calls != 1 {
		t.Fatalf("invalid dependencies reached runner: %d", calls)
	}
}

func TestSubmitRejectsAmbiguousReadOnlyBeforeDispatch(t *testing.T) {
	definition, _ := mode.Resolve(mode.SuperAgent)
	ctx := protocol.WithBackgroundSubmitter(mode.WithSnapshot(t.Context(), definition.Snapshot), submitTestHost{})
	tool := NewSubmitTool()
	calls := 0
	tool.Wire(func(context.Context, taskbridge.TaskSpec) (*taskbridge.TaskResult, error) {
		calls++
		return &taskbridge.TaskResult{Status: taskbridge.TaskStatusAccepted}, nil
	})
	for _, args := range []map[string]any{
		{"required_tools": []any{}, "prompt": "review git diff and run tests without editing", "read_only": true},
		{"prompt": "review", "tool_scope": "read_tools_only", "required_tools": []any{"shell"}},
	} {
		if _, err := tool.Execute(ctx, args); err == nil {
			t.Errorf("accepted conflicting submission: %+v", args)
		}
	}
	if calls != 0 {
		t.Errorf("started %d impossible worker tasks", calls)
	}
}

func TestSubmitGeneralReviewPreservesRequiredTools(t *testing.T) {
	definition, _ := mode.Resolve(mode.SuperAgent)
	ctx := protocol.WithBackgroundSubmitter(mode.WithSnapshot(t.Context(), definition.Snapshot), submitTestHost{})
	tool := NewSubmitTool()
	tool.Wire(func(_ context.Context, spec taskbridge.TaskSpec) (*taskbridge.TaskResult, error) {
		if spec.ReadOnly || len(spec.RequiredTools) != 1 || spec.RequiredTools[0] != "shell" {
			t.Fatalf("wrong review capabilities: %+v", spec)
		}
		return &taskbridge.TaskResult{Status: taskbridge.TaskStatusAccepted}, nil
	})
	if _, err := tool.Execute(ctx, map[string]any{"prompt": "review without editing, run git and tests", "required_tools": []any{"shell"}}); err != nil {
		t.Fatal(err)
	}
}

func TestSubmitRejectsMalformedCapabilityRequests(t *testing.T) {
	definition, _ := mode.Resolve(mode.SuperAgent)
	ctx := protocol.WithBackgroundSubmitter(mode.WithSnapshot(t.Context(), definition.Snapshot), submitTestHost{})
	tool := NewSubmitTool()
	tool.Wire(func(context.Context, taskbridge.TaskSpec) (*taskbridge.TaskResult, error) {
		t.Fatal("invalid capabilities reached runner")
		return nil, nil
	})
	for _, args := range []map[string]any{
		{"prompt": "review"},
		{"prompt": "review", "required_tools": nil},
		{"prompt": "review", "required_tools": []any{""}},
		{"prompt": "review", "required_tools": []string{"  "}},
		{"prompt": "review", "required_tools": []any{}, "tool_scope": "read_only"},
		{"prompt": "review", "required_tools": []any{}, "tool_scope": true},
	} {
		if _, err := tool.Execute(ctx, args); err == nil {
			t.Errorf("accepted invalid request: %+v", args)
		}
	}
}

func TestSubmitAllowsExplicitSnapshotForCurrentWorkspaceReview(t *testing.T) {
	definition, _ := mode.Resolve(mode.SuperAgent)
	ctx := protocol.WithBackgroundSubmitter(mode.WithSnapshot(t.Context(), definition.Snapshot), submitTestHost{})
	tool := NewSubmitTool()
	tool.Wire(func(_ context.Context, spec taskbridge.TaskSpec) (*taskbridge.TaskResult, error) {
		if spec.Isolation != "snapshot" {
			t.Fatalf("snapshot selection lost: %+v", spec)
		}
		return &taskbridge.TaskResult{Status: taskbridge.TaskStatusAccepted}, nil
	})
	if _, err := tool.Execute(ctx, map[string]any{"prompt": "review uncommitted code", "required_tools": []string{"shell"}, "isolation": "snapshot"}); err != nil {
		t.Fatal(err)
	}
}
