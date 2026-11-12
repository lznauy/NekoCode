package mcp

import (
	"context"
	"strings"
	"testing"
)

func TestCapturedToolSetDoesNotFollowReplacementOrExpansion(t *testing.T) {
	cmd, cleanup := startMockMCP(t, []toolDef{{Name: "alpha", Description: "original"}, {Name: "beta"}})
	defer cleanup()
	m := New()
	defer m.Close()
	if err := m.Add(t.Context(), "owner", "service", ServerConfig{Command: cmd.Path}); err != nil {
		t.Fatal(err)
	}
	snapshot := m.CaptureTools().Filter(func(id ToolIdentity) bool { return id.Tool == "alpha" })
	if got := snapshot.ListCapabilities(); !strings.Contains(got, "alpha") || strings.Contains(got, "beta") {
		t.Fatal(got)
	}
	m.mu.Lock()
	m.servers["owner"].tools = append(m.servers["owner"].tools, toolDef{Name: "late"})
	m.mu.Unlock()
	if _, err := snapshot.InspectTool("service", "late"); err == nil {
		t.Fatal("existing connection expanded a captured scope")
	}
	if _, err := snapshot.InspectTool("service", "beta"); err == nil {
		t.Fatal("snapshot discovered excluded tool")
	}
	if _, err := snapshot.CallServerTool(t.Context(), "service", "beta", nil); err == nil {
		t.Fatal("snapshot executed excluded tool")
	}
	if _, err := snapshot.CallServerTool(t.Context(), "service", "alpha", nil); err != nil {
		t.Fatal(err)
	}
	fresh, cleanupFresh := startMockMCP(t, []toolDef{{Name: "alpha", Description: "replacement"}, {Name: "new"}})
	defer cleanupFresh()
	if err := m.Add(t.Context(), "owner", "service", ServerConfig{Command: fresh.Path}); err != nil {
		t.Fatal(err)
	}
	if _, err := snapshot.CallServerTool(t.Context(), "service", "alpha", nil); err == nil {
		t.Fatal("old scope followed replacement connection")
	}
	if _, err := snapshot.InspectTool("service", "new"); err == nil {
		t.Fatal("old scope expanded")
	}
	current := m.CaptureTools()
	if got, err := current.InspectTool("service", "alpha"); err != nil || !strings.Contains(got, "replacement") {
		t.Fatalf("new submission cannot see replacement: %q %v", got, err)
	}
	m.Close()
	if _, err := current.CallServerTool(context.Background(), "service", "alpha", nil); err == nil {
		t.Fatal("closed service retained authority")
	}
}
