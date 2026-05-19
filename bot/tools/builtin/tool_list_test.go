package builtin

import (
	"context"
	"testing"
)

func TestListTool(t *testing.T) {
	td := setupTemp(t)
	l := &ListTool{}

	out, err := l.Execute(context.Background(), map[string]any{"path": td})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if out == "" {
		t.Error("expected directory listing")
	}

	_, err = l.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error for missing path")
	}
}
