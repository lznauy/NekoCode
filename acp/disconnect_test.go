package acp

import (
	"context"
	"encoding/json"
	rt "nekocode/runtime"
	"strings"
	"testing"
	"time"
)

type disconnectAuditBackend struct {
	*fakeBackend
	live *rt.Runtime
}

func (b *disconnectAuditBackend) StartRun(c context.Context, i rt.Input) (rt.RunID, error) {
	return b.live.StartRun(c, i)
}
func (b *disconnectAuditBackend) CancelRun(c context.Context, id rt.RunID) error {
	return b.live.CancelRun(c, id)
}
func (b *disconnectAuditBackend) WaitRun(c context.Context, id rt.RunID) error {
	return b.live.WaitRun(c, id)
}
func (b *disconnectAuditBackend) Events(c context.Context, f rt.EventFilter) (<-chan rt.Event, error) {
	return b.live.Events(c, f)
}
func (b *disconnectAuditBackend) ReplaceMCPServers(c context.Context, s string, v []rt.MCPServerSpec) error {
	return b.live.ReplaceMCPServers(c, s, v)
}
func (b *disconnectAuditBackend) ApplySessionSettings(v rt.SessionSettings) error {
	return b.live.ApplySessionSettings(v)
}
func (b *disconnectAuditBackend) SwitchSessionModel(v string) (rt.ModelSelection, error) {
	return b.live.SwitchSessionModel(v)
}
func (b *disconnectAuditBackend) SetSessionReasoning(v string) error {
	return b.live.SetSessionReasoning(v)
}

func TestDisconnectWaitsBeforeRestoringRuntimeConfig(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	replaced := 0
	active, effort, full := "original", "low", false
	live := rt.New(rt.RunnerFunc(func(c context.Context, _ string, _ rt.RunHost) (string, error) {
		close(started)
		<-c.Done()
		<-release
		return "", c.Err()
	}), rt.ServiceGroups{
		Models: rt.ModelsServices{
			ModelOptions: func() ([]rt.ModelOption, string) { return []rt.ModelOption{{Name: "original"}, {Name: "acp"}}, active },
			CurrentModel: func() rt.ModelSelection { return rt.ModelSelection{Model: active, ReasoningEffort: effort} },
			SwitchSessionModel: func(name string) (rt.ModelSelection, error) {
				active = name
				return rt.ModelSelection{Model: name}, nil
			},
			SetSessionReasoning: func(v string) error { effort = v; return nil },
		},
		Permissions: rt.PermissionsServices{PermissionMode: func() string {
			if full {
				return "full"
			}
			return "manual"
		}, SetFullAccess: func(v bool) { full = v }},
		MCP: rt.MCPServices{ReplaceMCPServers: func(context.Context, string, []rt.MCPServerSpec) error { replaced++; return nil }}})
	defer live.Close()
	b := &disconnectAuditBackend{fakeBackend: &fakeBackend{current: "session-1"}, live: live}
	s := &server{backend: b, cwd: "/workspace", active: make(map[string]*activeTurn), activeMCPSession: "session-1", defaultConfig: sessionConfig{Model: "original", ReasoningEffort: "low"}, sessionConfigs: map[string]sessionConfig{"session-1": {Model: "acp", ReasoningEffort: "high", FullAccess: true}}, conn: newConnection(&strings.Builder{})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.prompt(ctx, json.RawMessage(`{"sessionId":"session-1","prompt":[{"type":"text","text":"hello"}]}`))
	}()
	<-started
	cancel()
	select {
	case <-done:
		close(release)
		t.Fatal("prompt returned before runtime settled")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("prompt did not settle")
	}
	err := s.cleanup()
	if err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}
	if replaced != 1 || active != "original" || effort != "low" || full {
		t.Fatalf("configuration not restored: MCP=%d model=%s effort=%s full=%v", replaced, active, effort, full)
	}
}
