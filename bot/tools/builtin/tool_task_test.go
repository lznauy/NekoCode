package builtin

import (
	"context"
	"testing"
)

func TestTaskTool(t *testing.T) {
	tk := &TaskTool{}
	_, err := tk.Execute(context.Background(), map[string]any{"type": "explore", "prompt": "test"})
	if err == nil {
		t.Error("expected 'not wired' error")
	}
}
