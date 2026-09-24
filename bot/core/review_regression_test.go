package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"nekocode/bot/agent/subagent"
	"nekocode/bot/config"
	toolcore "nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/bot/extension/tool/runtime/runner"
	"nekocode/bot/extension/tool/runtime/taskbridge"
)

type availableRiskJudge struct{}

func (availableRiskJudge) Dangerous(context.Context, string) (bool, bool) { return false, true }

func TestFullAccessToggleDoesNotRestoreHiddenAutoMode(t *testing.T) {
	b := newPersistTestBot(t)
	b.riskJudge = availableRiskJudge{}
	b.jevAvailable.Store(true)
	executor := b.getAgent().Executor()
	calls := 0
	executor.SetBashAutoJudge(func(context.Context, string) (bool, bool) { calls++; return true, true })
	b.SetBashAuto(true)
	b.SetFullAccess(true)
	b.SetFullAccess(false)
	if b.FullAccess() || b.BashAuto() {
		t.Fatal("expected manual mode")
	}
	executor.ExecuteBatch(context.Background(), []toolcore.ToolCallItem{{ID: "review", Name: "shell", Args: map[string]any{"command": "find . -maxdepth 0"}}})
	if calls != 0 {
		t.Fatal("manual mode still consulted auto judge")
	}
}

func TestApplyConfigurationRefreshesDecisionEngine(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "")
	b := newPersistTestBot(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer updated" {
			t.Errorf("key = %q", got)
		}
		w.Write([]byte(`{"answers":{"q0":{"type":"noul","noul":0.9}}}`))
	}))
	defer server.Close()
	next := b.Configuration()
	next.Jev = &config.JevConfig{APIKey: "updated", BaseURL: server.URL}
	if err := b.ApplyConfiguration(next); err != nil {
		t.Fatal(err)
	}
	if b.riskJudge == nil {
		t.Fatal("configuration did not enable decision engine")
	}
	if dangerous, ok := b.riskJudge.Dangerous(context.Background(), "example"); !ok || !dangerous {
		t.Fatal("updated judge not used")
	}
	b.SetBashAuto(true)
	if !b.BashAuto() {
		t.Fatal("configured judge did not enable auto mode")
	}
	next.Jev = nil
	if err := b.ApplyConfiguration(next); err != nil {
		t.Fatal(err)
	}
	if b.riskJudge != nil {
		t.Fatal("configuration did not disable decision engine")
	}
	if b.BashAuto() {
		t.Fatal("disabling Jev left auto mode active")
	}
}

func TestAutoModeExitsFullAccessInExecutor(t *testing.T) {
	b := newPersistTestBot(t)
	b.riskJudge = availableRiskJudge{}
	b.jevAvailable.Store(true)
	b.SetFullAccess(true)
	b.SetBashAuto(true)
	if b.getAgent().Executor().FullAccess() {
		t.Fatal("auto mode retained full-access executor")
	}
}

func TestSubagentConfigCarriesJevPermissions(t *testing.T) {
	b := newPersistTestBot(t)
	parent := b.getAgent().Executor()
	parent.SetPermissionMode(permission.ModeAuto)
	calls := 0
	parent.SetBashAutoJudge(func(context.Context, string) (bool, bool) { calls++; return false, true })
	cfg := buildSubagentRunConfig(context.Background(), taskbridge.TaskSpec{}, subagent.Profile{}, nil, 1000, 80, b.getAgent(), "test", nil)
	if cfg.ConfigurePermissions == nil {
		t.Fatal("delegation omitted permission configuration")
	}
	child := runner.NewExecutor(nil)
	cfg.ConfigurePermissions(child)
	dec := child.SandboxEngine().EvaluateContext(context.Background(), "shell", map[string]any{"command": "make all"}, permission.EffectAsk)
	if calls != 1 || dec.Effect != permission.EffectAllow {
		t.Fatalf("child judge missing: calls=%d decision=%+v", calls, dec)
	}
}
