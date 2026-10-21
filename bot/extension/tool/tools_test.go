package tools

import (
	"context"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/workspace"
	"sync"
	"testing"
)

type testTool struct{ name string }

func (t *testTool) Name() string                                    { return t.name }
func (t *testTool) Description() string                             { return "test" }
func (t *testTool) Parameters() []core.Parameter                    { return nil }
func (t *testTool) ExecutionMode(map[string]any) core.ExecutionMode { return core.ModeParallel }
func (t *testTool) Execute(ctx context.Context, args map[string]any) (core.ToolOutput, error) {
	return core.ToolOutput{Text: "ok"}, nil
}

func TestRegistry(t *testing.T) {
	r := New()

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
	r := New()
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
	r := New()
	r.Register(&testTool{name: "z"})
	r.Unregister("z")
	r.Register(&testTool{name: "z"}) // re-register same name

	tool, err := r.Get("z")
	if err != nil || tool.Name() != "z" {
		t.Error("z should exist after re-register")
	}
}

func TestRegistryMetadataIsConcurrentSafe(t *testing.T) {
	r := New()
	manager := workspace.New(t.TempDir(), nil)
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.SetWorkspace(manager)
			_ = r.Workspace()
		}()
	}
	wg.Wait()
	if r.Workspace() == nil {
		t.Fatal("registry metadata was not retained")
	}
}

func TestRegistrationOptionsLifecycle(t *testing.T) {
	r := New()
	task := &testTool{name: "proxy"}
	r.RegisterWithOptions(task, RegistrationOptions{
		ResolveTarget: func(args map[string]any) (CallTarget, bool) {
			return CallTarget{Name: "real", Args: args}, true
		},
		Preview: func(_ context.Context, args map[string]any) string {
			return args["preview"].(string)
		},
		Privileged: func(context.Context, map[string]any, core.PermissionRequest) (core.ToolOutput, error) {
			return core.ToolOutput{Text: "privileged"}, nil
		},
		PermissionPlan: func(map[string]any, string) *core.PermissionRequest {
			return &core.PermissionRequest{Capabilities: []string{core.CapNetOutbound}}
		},
		PlanAllowed: true,
	})

	args := map[string]any{"preview": "ready"}
	if target, ok := r.ResolveTarget("proxy", args); !ok || target.Name != "real" {
		t.Fatalf("target = %+v, %v", target, ok)
	}
	if preview, ok := r.Preview(context.Background(), "proxy", args); !ok || preview != "ready" {
		t.Fatalf("preview = %q, %v", preview, ok)
	}
	entry, err := r.Lookup("proxy")
	if err != nil || entry.Privileged == nil || entry.PermissionPlan == nil || !entry.PlanAllowed {
		t.Fatal("privileged registration metadata is missing")
	}

	// A plain replacement must not inherit metadata from the old tool.
	r.Register(&testTool{name: "proxy"})
	if _, ok := r.ResolveTarget("proxy", args); ok {
		t.Fatal("target resolver survived plain replacement")
	}
	if _, ok := r.Preview(context.Background(), "proxy", args); ok {
		t.Fatal("preview survived plain replacement")
	}
	entry, err = r.Lookup("proxy")
	if err != nil || entry.Privileged != nil || entry.PermissionPlan != nil || entry.PlanAllowed {
		t.Fatal("privileged metadata survived plain replacement")
	}

	r.RegisterWithOptions(task, RegistrationOptions{ResolveTarget: func(map[string]any) (CallTarget, bool) {
		return CallTarget{Name: "real"}, true
	}})
	r.Unregister("proxy")
	if _, ok := r.ResolveTarget("proxy", nil); ok {
		t.Fatal("target resolver survived unregister")
	}
}
