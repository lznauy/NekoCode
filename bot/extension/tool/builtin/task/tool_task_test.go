package task

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"nekocode/bot/extension/tool/runtime/taskbridge"
)

func TestTaskToolPassesComposedSpec(t *testing.T) {
	tk := &TaskTool{}
	var got taskbridge.TaskSpec
	tk.Wire(func(ctx context.Context, spec taskbridge.TaskSpec) (*taskbridge.TaskResult, error) {
		got = spec
		if taskbridge.TaskIDFromCtx(ctx) != "sub-test" {
			t.Fatal("subagent identity not forwarded")
		}
		return &taskbridge.TaskResult{Content: "done"}, nil
	})
	out, err := tk.Execute(context.Background(), map[string]any{
		"_sub_callback": taskbridge.TaskCallback{ID: "sub-test"}, "profile": "explore", "skills": []any{"check", "hunt"}, "prompt": "review",
	})
	if err != nil || out.Render() != "done" {
		t.Fatalf("Execute() = %q, %v", out.Render(), err)
	}
	want := taskbridge.TaskSpec{Profile: "explore", Skills: []string{"check", "hunt"}, Prompt: "review"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("spec = %+v, want %+v", got, want)
	}
}

func TestTaskToolResultAndErrorContract(t *testing.T) {
	cause := errors.New("request budget exceeded")
	for _, tc := range []struct {
		name      string
		status    taskbridge.TaskStatus
		err       error
		wantError bool
		partial   bool
	}{
		{"completed", taskbridge.TaskStatusCompleted, nil, false, false},
		{"completed_with_error", taskbridge.TaskStatusCompleted, cause, true, false},
		{"partial", taskbridge.TaskStatusPartial, nil, false, true},
		{"partial_with_error", taskbridge.TaskStatusPartial, cause, true, true},
		{"cancelled", taskbridge.TaskStatusPartial, context.Canceled, true, true},
		{"failed", taskbridge.TaskStatusFailed, nil, true, false},
		{"failed_with_error", taskbridge.TaskStatusFailed, cause, true, false},
		{"invalid_status", taskbridge.TaskStatus(99), nil, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tk := NewTaskTool()
			tk.Wire(func(context.Context, taskbridge.TaskSpec) (*taskbridge.TaskResult, error) {
				return &taskbridge.TaskResult{Status: tc.status, Content: "available evidence"}, tc.err
			})
			output, err := tk.Execute(context.Background(), map[string]any{"profile": "explore", "prompt": "review"})
			if (err != nil) != tc.wantError || !strings.Contains(output.Text, "available evidence") {
				t.Fatalf("lost outcome: %+v, %v", output, err)
			}
			if tc.err != nil && !errors.Is(err, tc.err) {
				t.Fatalf("lost error identity: %v", err)
			}
			if tc.partial && !strings.Contains(output.Text, "partial result") {
				t.Fatal("partial result was not labeled")
			}
		})
	}
}

func TestTaskToolRequiresProfile(t *testing.T) {
	tk := &TaskTool{}
	tk.Wire(func(context.Context, taskbridge.TaskSpec) (*taskbridge.TaskResult, error) { return nil, nil })
	_, err := tk.Execute(context.Background(), map[string]any{"prompt": "test"})
	if err == nil || !strings.Contains(err.Error(), "profile") {
		t.Fatalf("error = %v, want missing profile", err)
	}
}
