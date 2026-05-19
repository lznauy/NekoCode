package tools

import (
	"nekocode/common"
	"context"
	"testing"
)

type testTool struct{ name string }

func (t *testTool) Name() string                                       { return t.name }
func (t *testTool) Description() string                                { return "test" }
func (t *testTool) Parameters() []Parameter                            { return nil }
func (t *testTool) ExecutionMode(map[string]any) ExecutionMode         { return ModeParallel }
func (t *testTool) DangerLevel(map[string]any) common.DangerLevel             { return common.LevelSafe }
func (t *testTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	return "ok", nil
}

func TestRegistry(t *testing.T) {
	r := NewRegistry()

	// Get on empty registry.
	_, err := r.Get("missing")
	if err == nil {
		t.Error("expected error for missing tool")
	}

	// Register + Get.
	r.Register(&testTool{name: "a"})
	r.Register(&testTool{name: "b"})
	tool, err := r.Get("a")
	if err != nil || tool.Name() != "a" {
		t.Error("Get failed")
	}

	// List.
	names := r.List()
	if len(names) != 2 {
		t.Errorf("List: got %d, want 2", len(names))
	}

	// Descriptors.
	descs := r.Descriptors()
	if len(descs) != 2 {
		t.Errorf("Descriptors: got %d, want 2", len(descs))
	}
}
