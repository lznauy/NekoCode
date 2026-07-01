package tools

import (
	"context"
	"nekocode/bot/tools/core"
	"nekocode/common"
	"testing"
)

type testTool struct{ name string }

func (t *testTool) Name() string                                  { return t.name }
func (t *testTool) Description() string                           { return "test" }
func (t *testTool) Parameters() []core.Parameter                       { return nil }
func (t *testTool) ExecutionMode(map[string]any) core.ExecutionMode    { return core.ModeParallel }
func (t *testTool) DangerLevel(map[string]any) common.DangerLevel { return common.LevelSafe }
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

func TestUnregister(t *testing.T) {
	r := NewRegistry()
	r.Register(&testTool{name: "x"})
	r.Register(&testTool{name: "y"})

	if _, err := r.Get("x"); err != nil {
		t.Error("x should exist before unregister")
	}

	r.Unregister("x")

	if _, err := r.Get("x"); err == nil {
		t.Error("x should be gone after unregister")
	}
	if _, err := r.Get("y"); err != nil {
		t.Error("y should still exist")
	}

	// List should only have y.
	if list := r.List(); len(list) != 1 || list[0].Name() != "y" {
		t.Errorf("List after unregister: got %d tools, want 1 (y)", len(list))
	}

	// Unregister non-existent — should be a no-op (no panic).
	r.Unregister("nonexistent")
}

func TestUnregisterThenReRegister(t *testing.T) {
	r := NewRegistry()
	r.Register(&testTool{name: "z"})
	r.Unregister("z")
	r.Register(&testTool{name: "z"}) // re-register same name

	tool, err := r.Get("z")
	if err != nil || tool.Name() != "z" {
		t.Error("z should exist after re-register")
	}
}
