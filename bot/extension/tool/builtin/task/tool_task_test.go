package task

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/protocol"
)

func TestTaskResumePassesIdentityAndReturnsIt(t *testing.T) {
	tool := NewTaskTool()
	tool.Wire(func(_ context.Context, spec taskbridge.TaskSpec) (*taskbridge.TaskResult, error) {
		if spec.ResumeID != "saved" || spec.Skills != nil {
			t.Fatalf("resume spec=%+v", spec)
		}
		return &taskbridge.TaskResult{ID: "saved", Content: "continued"}, nil
	})
	args := map[string]any{"profile": "explore", "prompt": "continue", "resume_id": "saved"}
	out, err := tool.Execute(context.Background(), args)
	if err != nil || !strings.Contains(out.Text, "[Subagent ID: saved]") {
		t.Fatalf("output=%+v err=%v", out, err)
	}
	for _, invalid := range []any{nil, "", 42} {
		args["resume_id"] = invalid
		if _, err := tool.Execute(context.Background(), args); err == nil {
			t.Fatal("invalid resume ID accepted")
		}
	}
}

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
		"_sub_callback": taskbridge.TaskCallback{ID: "sub-test"}, "title": "Review auth", "profile": "explore", "skills": []any{"check", "hunt"}, "prompt": "review",
	})
	if err != nil || out.Render() != "done" {
		t.Fatalf("Execute() = %q, %v", out.Render(), err)
	}
	want := taskbridge.TaskSpec{Title: "Review auth", Profile: "explore", Skills: []string{"check", "hunt"}, Prompt: "review"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("spec = %+v, want %+v", got, want)
	}
}

func TestTaskToolPublishesActualTerminalStatus(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status taskbridge.TaskStatus
		err    error
		want   string
	}{
		{"completed", taskbridge.TaskStatusCompleted, nil, "completed"},
		{"partial", taskbridge.TaskStatusPartial, nil, "partial"},
		{"failed-save", taskbridge.TaskStatusPartial, errors.New("save failed"), "failed"},
		{"cancelled", taskbridge.TaskStatusPartial, context.Canceled, "interrupted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool := NewTaskTool()
			tool.Wire(func(context.Context, taskbridge.TaskSpec) (*taskbridge.TaskResult, error) {
				return &taskbridge.TaskResult{Status: tc.status}, tc.err
			})
			var terminal protocol.StepEvent
			_, _ = tool.Execute(context.Background(), map[string]any{"profile": "explore", "prompt": "inspect", "_sub_callback": taskbridge.TaskCallback{Callback: func(ev protocol.StepEvent) { terminal = ev }}})
			if terminal.Action != protocol.StepActionSubAgentEnd || terminal.SubAgentStatus != tc.want {
				t.Fatalf("terminal=%+v", terminal)
			}
		})
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

func TestTaskResumeAllowsOmittedProfile(t *testing.T) {
	tool := NewTaskTool()
	tool.Wire(func(_ context.Context, spec taskbridge.TaskSpec) (*taskbridge.TaskResult, error) {
		if spec.ResumeID != "saved" || spec.Profile != "" {
			t.Fatalf("resume must resolve saved profile in runner: %+v", spec)
		}
		return &taskbridge.TaskResult{ID: "saved", Content: "continued"}, nil
	})
	if _, err := tool.Execute(t.Context(), map[string]any{"resume_id": "saved", "prompt": "continue"}); err != nil {
		t.Fatal(err)
	}
	for _, parameter := range tool.Parameters() {
		if parameter.Name == "profile" && parameter.Required {
			t.Fatal("schema requires profile even for resume")
		}
	}
}
