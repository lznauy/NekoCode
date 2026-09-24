package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestManagerReplaceDoesNotHoldLockDuringRemoteStartup(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		once.Do(func() { close(started) })
		<-release
		http.Error(w, "stopped", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	m := New()
	defer m.Close()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- m.Replace(ctx, nil, []Registration{{ID: "session:test:slow", Name: "slow", Config: ServerConfig{URL: server.URL}}})
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		cancel()
		close(release)
		t.Fatal("remote startup did not begin")
	}
	healthDone := make(chan struct{})
	go func() { m.Health(); close(healthDone) }()
	select {
	case <-healthDone:
	case <-time.After(200 * time.Millisecond):
		cancel()
		close(release)
		t.Fatal("Health blocked behind remote replacement startup")
	}
	cancel()
	close(release)
	<-result
}

func TestManagerCloseStopsStagedRemoteReplacement(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var startOnce, releaseOnce sync.Once
	releaseRequests := func() { releaseOnce.Do(func() { close(release) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		startOnce.Do(func() { close(started) })
		<-release
	}))
	defer func() {
		releaseRequests()
		server.Close()
	}()

	m := New()
	result := make(chan error, 1)
	go func() {
		result <- m.Replace(context.Background(), nil, []Registration{{ID: "session:test:slow", Name: "slow", Config: ServerConfig{URL: server.URL}}})
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		m.Close()
		t.Fatal("remote startup did not begin")
	}
	m.Close()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("replacement succeeded after manager close")
		}
	case <-time.After(2 * time.Second):
		releaseRequests()
		t.Fatal("replacement did not finish after manager close")
	}
}

func TestManagerAddServer(t *testing.T) {
	mockTools := []toolDef{
		{Name: "alpha", Description: "tool alpha", InputSchema: inputSchema{Type: "object"}},
		{Name: "beta", Description: "tool beta", InputSchema: inputSchema{Type: "object"}},
	}
	cmd, cleanup := startMockMCP(t, mockTools)
	defer cleanup()

	m := New()
	if err := m.Add(context.Background(), "plugin:p:srv", "srv", ServerConfig{Command: cmd.Path}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	list := m.ListCapabilities()
	for _, want := range []string{"srv:", "- alpha", "- beta"} {
		if !strings.Contains(list, want) {
			t.Fatalf("capabilities missing %q: %s", want, list)
		}
	}

	h := m.Health()["srv"]
	if h.Status != StatusReady || h.ToolCount != 2 {
		t.Errorf("health = %+v, want ready with 2 tools", h)
	}

	m.Close()
	if got := m.ListCapabilities(); got != "No MCP servers configured." {
		t.Fatalf("after close, list = %q", got)
	}
}

func TestManagerAddServerStartFailure(t *testing.T) {
	m := New()
	if err := m.Add(context.Background(), "config:bad", "bad", ServerConfig{Command: "/nonexistent-mcp-server"}); err == nil {
		t.Fatal("Add should fail for a missing command")
	}

	if _, ok := m.Health()["bad"]; ok {
		t.Fatal("failed synchronous server leaked health state")
	}
	if owner := m.Owner("bad"); owner != "" {
		t.Fatalf("failed synchronous server retained owner %q", owner)
	}
}

func TestManagerRemoveServer(t *testing.T) {
	cmd, cleanup := startMockMCP(t, []toolDef{
		{Name: "alpha", InputSchema: inputSchema{Type: "object"}},
	})
	defer cleanup()

	m := New()
	if err := m.Add(context.Background(), "plugin:p:srv", "srv", ServerConfig{Command: cmd.Path}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	m.Remove("plugin:p:srv")
	if _, ok := m.Health()["srv"]; ok {
		t.Error("health should be cleared after RemoveServer")
	}
	if _, err := m.CallServerTool(context.Background(), "srv", "alpha", nil); err == nil {
		t.Fatal("removed server must not route calls")
	}

	// Removing an unknown server is a no-op.
	m.Remove("plugin:p:srv")
}

func TestManagerReAddReplacesTools(t *testing.T) {
	// First generation exposes alpha+beta; second exposes only gamma.
	cmd1, cleanup1 := startMockMCP(t, []toolDef{
		{Name: "alpha", InputSchema: inputSchema{Type: "object"}},
		{Name: "beta", InputSchema: inputSchema{Type: "object"}},
	})
	defer cleanup1()
	cmd2, cleanup2 := startMockMCP(t, []toolDef{
		{Name: "gamma", InputSchema: inputSchema{Type: "object"}},
	})
	defer cleanup2()

	m := New()
	if err := m.Add(context.Background(), "plugin:p:srv", "srv", ServerConfig{Command: cmd1.Path}); err != nil {
		t.Fatalf("first Add: %v", err)
	}
	if err := m.Add(context.Background(), "plugin:p:srv", "srv", ServerConfig{Command: cmd2.Path}); err != nil {
		t.Fatalf("second Add: %v", err)
	}

	list := m.ListCapabilities()
	if !strings.Contains(list, "- gamma") || strings.Contains(list, "- alpha") {
		t.Fatalf("re-added server tools = %s, want only gamma", list)
	}
}

func TestManagerReplaceIsAtomic(t *testing.T) {
	oldCmd, cleanupOld := startMockMCP(t, []toolDef{{Name: "old", InputSchema: inputSchema{Type: "object"}}})
	defer cleanupOld()
	newCmd, cleanupNew := startMockMCP(t, []toolDef{{Name: "new", InputSchema: inputSchema{Type: "object"}}})
	defer cleanupNew()

	m := New()
	defer m.Close()
	if err := m.Add(context.Background(), "session:acp:old", "old", ServerConfig{Command: oldCmd.Path}); err != nil {
		t.Fatal(err)
	}
	err := m.Replace(context.Background(), []string{"session:acp:old"}, []Registration{
		{ID: "session:acp:new", Name: "new", Config: ServerConfig{Command: newCmd.Path}},
		{ID: "session:acp:bad", Name: "bad", Config: ServerConfig{Command: "/nonexistent-mcp-server"}},
	})
	if err == nil {
		t.Fatal("replacement with a broken server succeeded")
	}
	if _, err := m.CallServerTool(context.Background(), "old", "old", nil); err != nil {
		t.Fatalf("old server was lost after failed replacement: %v", err)
	}
	if _, exists := m.Health()["new"]; exists {
		t.Fatal("staged server leaked into live health state")
	}
}

func TestManagerReplaceCancellationLeavesOldSet(t *testing.T) {
	oldCmd, cleanupOld := startMockMCP(t, []toolDef{{Name: "old", InputSchema: inputSchema{Type: "object"}}})
	defer cleanupOld()
	slowCmd, cleanupSlow := startMockMCPWithDelay(t, nil, 5*time.Second)
	defer cleanupSlow()

	m := New()
	defer m.Close()
	if err := m.Add(context.Background(), "session:acp:old", "old", ServerConfig{Command: oldCmd.Path}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := m.Replace(ctx, []string{"session:acp:old"}, []Registration{
		{ID: "session:acp:slow", Name: "slow", Config: ServerConfig{Command: slowCmd.Path}},
	})
	if err == nil || !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("Replace error = %v, want deadline exceeded", err)
	}
	if _, err := m.CallServerTool(context.Background(), "old", "old", nil); err != nil {
		t.Fatalf("old server was lost after cancellation: %v", err)
	}
}

func TestManagerRejectsDuplicateNameFromDifferentOwner(t *testing.T) {
	cmd1, cleanup1 := startMockMCP(t, []toolDef{
		{Name: "alpha", InputSchema: inputSchema{Type: "object"}},
	})
	defer cleanup1()
	cmd2, cleanup2 := startMockMCP(t, []toolDef{
		{Name: "beta", InputSchema: inputSchema{Type: "object"}},
	})
	defer cleanup2()

	m := New()
	if err := m.Add(context.Background(), "plugin:p:srv", "srv", ServerConfig{Command: cmd1.Path}); err != nil {
		t.Fatalf("add plugin server: %v", err)
	}
	if err := m.Add(context.Background(), "config:srv", "srv", ServerConfig{Command: cmd2.Path}); err == nil {
		t.Fatal("duplicate display name should be rejected")
	}

	// Removing an owner that never started must not affect the active server.
	m.Remove("config:srv")
	if _, err := m.CallServerTool(context.Background(), "srv", "alpha", nil); err != nil {
		t.Fatalf("active server broken by removing a non-owner: %v", err)
	}
}

func TestManagerClose(t *testing.T) {
	cmd, cleanup := startMockMCP(t, []toolDef{
		{Name: "alpha", InputSchema: inputSchema{Type: "object"}},
	})
	defer cleanup()

	m := New()
	if err := m.Add(context.Background(), "config:a", "a", ServerConfig{Command: cmd.Path}); err != nil {
		t.Fatalf("Add a: %v", err)
	}
	// Failed synchronous starts roll back immediately; Close remains safe.
	_ = m.Add(context.Background(), "config:bad", "bad", ServerConfig{Command: "/nonexistent-mcp-server"})

	m.Close()
	if len(m.Health()) != 0 {
		t.Errorf("health should be empty, got %v", m.Health())
	}
}

func TestManagerAddBackgroundDoesNotWaitForServer(t *testing.T) {
	cmd, cleanup := startMockMCPWithDelay(t, []toolDef{
		{Name: "alpha", InputSchema: inputSchema{Type: "object"}},
	}, 300*time.Millisecond)
	defer cleanup()

	m := New()
	defer m.Close()
	start := time.Now()
	if err := m.AddBackground("config:slow", "slow", ServerConfig{Command: cmd.Path}); err != nil {
		t.Fatalf("AddBackground: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("AddBackground blocked for %s", elapsed)
	}
	if h := m.Health()["slow"]; h.Status != StatusStarting {
		t.Fatalf("initial health = %+v, want starting", h)
	}
	if _, err := m.CallServerTool(context.Background(), "slow", "alpha", nil); err == nil || !strings.Contains(err.Error(), "still starting") {
		t.Fatalf("call while starting error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h := m.Health()["slow"]
		if h.Status == StatusReady {
			if h.ToolCount != 1 {
				t.Fatalf("ready health = %+v, want one tool", h)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server did not become ready: %+v", m.Health()["slow"])
}

func TestManagerAddBackgroundRecordsFailure(t *testing.T) {
	m := New()
	defer m.Close()
	if err := m.AddBackground("config:bad", "bad", ServerConfig{Command: "/nonexistent-mcp-server"}); err != nil {
		t.Fatalf("AddBackground: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		h := m.Health()["bad"]
		if h.Status == StatusError {
			if h.Error == "" {
				t.Fatal("error health should include a message")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server failure was not recorded: %+v", m.Health()["bad"])
}

func TestManagerCloseCancelsBackgroundStartup(t *testing.T) {
	cmd, cleanup := startMockMCPWithDelay(t, nil, 5*time.Second)
	defer cleanup()

	m := New()
	if err := m.AddBackground("config:slow", "slow", ServerConfig{Command: cmd.Path}); err != nil {
		t.Fatalf("AddBackground: %v", err)
	}
	start := time.Now()
	m.Close()
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Close waited too long for background startup: %s", elapsed)
	}
	if len(m.Health()) != 0 {
		t.Fatalf("health after Close = %v, want empty", m.Health())
	}
}

func TestAuthOverrideSurvivesRebuildAndIsConsumed(t *testing.T) {
	cmd, cleanup := startMockMCP(t, []toolDef{
		{Name: "alpha", InputSchema: inputSchema{Type: "object"}},
	})
	defer cleanup()

	m := New()
	defer m.Close()
	// AuthorizationAction("login") records the interactive state under the
	// owner id so a rebuild (config reload, error restart) keeps it.
	m.mu.Lock()
	m.authOverrides["config:srv"] = authOverride{interactive: true, scopes: []string{"docs.read"}, gen: 1}
	m.mu.Unlock()

	if err := m.AddBackground("config:srv", "srv", ServerConfig{Command: cmd.Path}); err != nil {
		t.Fatalf("AddBackground: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if m.Health()["srv"].Status == StatusReady {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if h := m.Health()["srv"]; h.Status != StatusReady {
		t.Fatalf("server did not become ready: %+v", h)
	}

	m.mu.Lock()
	s := m.servers["config:srv"]
	if s == nil || s.client.config.interactive || len(s.client.config.authorizationScopes) != 0 {
		m.mu.Unlock()
		t.Fatal("transient authorization options leaked into persistent definition")
	}
	if _, pending := m.authOverrides["config:srv"]; pending {
		m.mu.Unlock()
		t.Fatal("override should be consumed once the start finished")
	}
	m.mu.Unlock()

	// After consumption, a rebuild re-registers non-interactive.
	m.Remove("config:srv")
	if err := m.AddBackground("config:srv", "srv", ServerConfig{Command: cmd.Path}); err != nil {
		t.Fatalf("rebuild AddBackground: %v", err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if m.Health()["srv"].Status == StatusReady {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	m.mu.Lock()
	s = m.servers["config:srv"]
	interactive := s != nil && s.client.config.interactive
	m.mu.Unlock()
	if interactive {
		t.Fatal("consumed override must not leak into later rebuilds")
	}
}

func TestAuthOverrideFailedStartConsumesOverride(t *testing.T) {
	m := New()
	defer m.Close()
	m.mu.Lock()
	m.authOverrides["config:bad"] = authOverride{interactive: true, scopes: []string{"docs.read"}, gen: 1}
	m.mu.Unlock()

	if err := m.AddBackground("config:bad", "bad", ServerConfig{Command: "/nonexistent-mcp-server"}); err != nil {
		t.Fatalf("AddBackground: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		_, pending := m.authOverrides["config:bad"]
		m.mu.Unlock()
		if !pending {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("failed background start must still consume the override")
}
