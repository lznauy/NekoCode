package task

import (
	"context"
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
	if err != nil || out != "done" {
		t.Fatalf("Execute() = %q, %v", out, err)
	}
	want := taskbridge.TaskSpec{Profile: "explore", Skills: []string{"check", "hunt"}, Prompt: "review"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("spec = %+v, want %+v", got, want)
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
