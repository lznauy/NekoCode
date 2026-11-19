package capability

import (
	"context"
	"testing"
)

type argumentAuditRuntime struct{ calls int }

func (*argumentAuditRuntime) ListCapabilities() string                   { return "stub" }
func (*argumentAuditRuntime) InspectTool(string, string) (string, error) { return "schema", nil }
func (r *argumentAuditRuntime) CallServerTool(context.Context, string, string, map[string]any) (string, error) {
	r.calls++
	return "called", nil
}
func TestCapabilityRejectsInvalidArgumentsBeforeInvocation(t *testing.T) {
	runtime := &argumentAuditRuntime{}
	tool := New(runtime)
	for _, value := range []any{nil, `{"target":"one"}`, []any{"one"}, 42} {
		args := map[string]any{"action": "call", "server": "stub", "tool": "mutate", "args": value}
		if _, err := tool.Execute(context.Background(), args); err == nil {
			t.Errorf("invalid args accepted: %#v", value)
		}
		if _, ok := ResolveTarget(args); ok {
			t.Errorf("invalid args resolved to an invocation: %#v", value)
		}
	}
	if runtime.calls != 0 {
		t.Fatalf("invalid requests invoked backend %d times", runtime.calls)
	}
	for _, args := range []map[string]any{{"action": "call", "server": "stub", "tool": "read"}, {"action": "call", "server": "stub", "tool": "read", "args": map[string]any{}}} {
		if _, err := tool.Execute(context.Background(), args); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDiscoveryRejectsFieldsForOtherActions(t *testing.T) {
	tool := New(nil) // Any backend access would panic.
	for _, args := range []map[string]any{
		{"action": "list", "server": "s"}, {"action": "list", "tool": "t"}, {"action": "list", "args": map[string]any{}},
		{"action": "inspect", "server": "s", "tool": "t", "args": map[string]any{}},
	} {
		if _, err := tool.Execute(t.Context(), args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
