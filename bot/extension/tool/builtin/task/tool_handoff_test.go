package task

import (
	"context"
	"math"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/mode"
	"nekocode/protocol"
	"testing"
)

func TestSubmitForkCarriesExplicitWorkspaceAndBudget(t *testing.T) {
	definition, _ := mode.Resolve(mode.SuperAgent)
	ctx := protocol.WithBackgroundSubmitter(mode.WithSnapshot(t.Context(), definition.Snapshot), submitTestHost{})
	tool := NewSubmitTool()
	calls := 0
	tool.Wire(func(_ context.Context, spec taskbridge.TaskSpec) (*taskbridge.TaskResult, error) {
		calls++
		if spec.ForkID != "prior" || spec.ResumeID != "" || spec.Isolation != "snapshot" || spec.MaxSteps != 30 {
			t.Fatalf("bad handoff spec: %+v", spec)
		}
		return &taskbridge.TaskResult{Status: taskbridge.TaskStatusAccepted}, nil
	})
	args := func() map[string]any {
		return map[string]any{"prompt": "regress parent fixes", "required_tools": []any{"shell"}, "fork_from": "prior", "isolation": "snapshot", "max_steps": float64(30)}
	}
	if _, err := tool.Execute(ctx, args()); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"isolation", "fork_from"} {
		a := args()
		a[key] = ""
		if _, err := tool.Execute(ctx, a); err == nil {
			t.Fatalf("missing %s admitted", key)
		}
	}
	a := args()
	a["resume_id"] = "prior"
	if _, err := tool.Execute(ctx, a); err == nil {
		t.Fatal("resume plus fork accepted")
	}
	for _, value := range []any{float64(0), 1.5, math.NaN(), math.Inf(1), "30"} {
		a := args()
		a["max_steps"] = value
		if _, err := tool.Execute(ctx, a); err == nil {
			t.Fatalf("invalid budget accepted: %v", value)
		}
	}
	if calls != 1 {
		t.Fatal("rejected requests reached runner")
	}
}

func TestSubmitReviewPolicyValidationAndBridge(t *testing.T) {
	definition, _ := mode.Resolve(mode.SuperAgent)
	ctx := protocol.WithBackgroundSubmitter(mode.WithSnapshot(t.Context(), definition.Snapshot), submitTestHost{})
	tool := NewSubmitTool()
	var got []string
	tool.Wire(func(_ context.Context, spec taskbridge.TaskSpec) (*taskbridge.TaskResult, error) {
		got = append(got, spec.ReviewPolicy)
		return &taskbridge.TaskResult{Status: taskbridge.TaskStatusAccepted}, nil
	})
	for _, policy := range []string{"", "immediate", "batch"} {
		args := map[string]any{"prompt": "check", "required_tools": []any{"shell"}}
		if policy != "" {
			args["review_policy"] = policy
		}
		if _, err := tool.Execute(ctx, args); err != nil {
			t.Fatal(err)
		}
	}
	for _, policy := range []any{"", "other", nil, 1} {
		args := map[string]any{"prompt": "check", "required_tools": []any{"shell"}, "review_policy": policy}
		if _, err := tool.Execute(ctx, args); err == nil {
			t.Fatalf("invalid policy accepted: %v", policy)
		}
	}
	if len(got) != 3 || got[0] != "batch" || got[1] != "immediate" || got[2] != "batch" {
		t.Fatalf("policy bridge: %v", got)
	}
}

func TestAttemptBudgetValidationAndResume(t *testing.T) {
	definition, _ := mode.Resolve(mode.SuperAgent)
	ctx := protocol.WithBackgroundSubmitter(mode.WithSnapshot(t.Context(), definition.Snapshot), submitTestHost{})
	tool := NewSubmitTool()
	var got []taskbridge.TaskSpec
	tool.Wire(func(_ context.Context, spec taskbridge.TaskSpec) (*taskbridge.TaskResult, error) {
		got = append(got, spec)
		return &taskbridge.TaskResult{Status: taskbridge.TaskStatusAccepted}, nil
	})
	for _, args := range []map[string]any{
		{"prompt": "review", "required_tools": []any{}},
		{"prompt": "review", "required_tools": []any{}, "max_steps": 3},
		{"prompt": "continue", "resume_id": "same", "max_steps": 18},
	} {
		if _, err := tool.Execute(ctx, args); err != nil {
			t.Fatal(err)
		}
	}
	if got[0].MaxSteps != 0 || got[1].MaxSteps != 3 || got[2].MaxSteps != 18 || got[2].ResumeID != "same" {
		t.Fatal(got)
	}
	for _, v := range []any{nil, 0, -1, 1.5, "10", math.NaN(), math.Inf(1)} {
		_, err := tool.Execute(ctx, map[string]any{"prompt": "review", "required_tools": []any{}, "max_steps": v})
		if err == nil {
			t.Fatalf("invalid limit admitted: %v", v)
		}
	}
	if _, err := tool.Execute(ctx, map[string]any{"prompt": "review", "required_tools": []any{}, "stage_steps": 18}); err == nil {
		t.Fatal("removed stage_steps silently accepted")
	}
	if len(got) != 3 {
		t.Fatal("invalid requests reached task runner")
	}
}

func TestGenericBatchDefaultDoesNotOverrideSavedResumePolicy(t *testing.T) {
	d, _ := mode.Resolve(mode.SuperAgent)
	ctx := protocol.WithBackgroundSubmitter(mode.WithSnapshot(t.Context(), d.Snapshot), submitTestHost{})
	tool := NewSubmitTool()
	var got []taskbridge.TaskSpec
	tool.Wire(func(_ context.Context, spec taskbridge.TaskSpec) (*taskbridge.TaskResult, error) {
		got = append(got, spec)
		return &taskbridge.TaskResult{Status: taskbridge.TaskStatusAccepted}, nil
	})
	for _, args := range []map[string]any{{"prompt": "independent work", "required_tools": []any{}}, {"prompt": "continue saved work", "required_tools": []any{}, "resume_id": "saved"}, {"prompt": "needs incremental decisions", "required_tools": []any{}, "review_policy": "immediate"}} {
		if _, err := tool.Execute(ctx, args); err != nil {
			t.Fatal(err)
		}
	}
	if got[0].ReviewPolicy != "batch" || got[1].ReviewPolicy != "" || got[2].ReviewPolicy != "immediate" {
		t.Fatal("default changed explicit or saved policy", got)
	}
}

func TestPendingDeliveryRejectionDoesNotBlockAnotherTaskResume(t *testing.T) {
	definition, _ := mode.Resolve(mode.SuperAgent)
	ctx := protocol.WithBackgroundSubmitter(mode.WithSnapshot(t.Context(), definition.Snapshot), submitTestHost{})
	ctx = protocol.WithExecutionIdentity(ctx, protocol.ExecutionIdentity{RunID: "coordinator-run"})
	tool := NewSubmitTool()
	var resumed []string
	tool.Wire(func(_ context.Context, spec taskbridge.TaskSpec) (*taskbridge.TaskResult, error) {
		resumed = append(resumed, spec.ResumeID)
		if spec.ResumeID == "pending" {
			return nil, protocol.ErrBackgroundResumeDeliveryPending
		}
		return &taskbridge.TaskResult{ID: spec.ResumeID, Status: taskbridge.TaskStatusAccepted}, nil
	})
	if _, err := tool.Execute(ctx, map[string]any{"prompt": "continue", "resume_id": "pending"}); err == nil {
		t.Fatal("pending result was resumed")
	}
	if _, err := tool.Execute(ctx, map[string]any{"prompt": "continue", "resume_id": "settled"}); err != nil {
		t.Fatalf("unrelated settled task resume was blocked: %v", err)
	}
	if len(resumed) != 2 || resumed[1] != "settled" {
		t.Fatalf("runtime did not evaluate each task: %v", resumed)
	}
}
