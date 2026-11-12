package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"nekocode/bot/agent/subagent"
	"nekocode/bot/config"
	tools "nekocode/bot/extension/tool"
	toolcore "nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/bot/extension/tool/runtime/runner"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/mode"
	"nekocode/protocol"
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
	cfg := buildSubagentRunConfig(context.Background(), taskbridge.TaskSpec{}, subagent.Profile{}, nil, 1000, 80, b.getAgent(), "test", nil, "attempt", 1)
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

func TestSubagentCompletionForwardsEvidenceWithAttemptIdentity(t *testing.T) {
	b := newPersistTestBot(t)
	var got protocol.StepEvent
	ctx := taskbridge.WithTaskCallback(t.Context(), func(event protocol.StepEvent) { got = event })
	cfg := buildSubagentRunConfig(ctx, taskbridge.TaskSpec{}, subagent.Profile{}, nil, 1000, 80, b.getAgent(), "parent", nil, "attempt", 2)
	evidence := protocol.ToolEvidence{Schema: 1, Executions: []protocol.ExecutionReceipt{{ID: "physical", Tool: "read", Status: "success"}}, ReturnedHash: "delivery"}
	cfg.OnToolCall(subagent.ToolCallEvent{Action: protocol.StepActionExecuteTool, CallID: "call", ToolName: "read", Evidence: evidence})
	if got.SubAgentAttemptID != "attempt" || got.SubAgentAttemptSeq != 2 || len(got.Evidence.Executions) != 1 || got.Evidence.Executions[0].ID != "physical" {
		t.Fatalf("child evidence lost in host bridge: %+v", got)
	}
}

func TestBackgroundPermissionRevocationSurvivesAgentRebuild(t *testing.T) {
	b := newPersistTestBot(t)
	b.SetFullAccess(true)
	worker := runner.NewExecutor(nil)
	b.getAgent().Executor().ConfigureChildPermissions(worker)
	b.permissionAuthority.ConfigureChildPermissionMode(worker)
	if !worker.FullAccess() {
		t.Fatal("worker did not inherit full mode")
	}
	before := b.getAgent()
	// Reasoning and model changes both rebuild the foreground Agent.
	b.mu.Lock()
	b.rebuildAgentLocked()
	b.mu.Unlock()
	if b.getAgent() == before {
		t.Fatal("foreground was not rebuilt")
	}
	b.SetFullAccess(false)
	if worker.FullAccess() {
		t.Fatal("old worker retained FULL after revocation")
	}
	dec := worker.SandboxEngine().EvaluateContext(t.Context(), "shell", map[string]any{"command": "make all"}, permission.EffectAsk)
	if dec.Effect == permission.EffectAllow {
		t.Fatalf("revoked worker silently allowed shell: %+v", dec)
	}
}

func TestBackgroundPermissionTracksCurrentSessionMode(t *testing.T) {
	b := newPersistTestBot(t)
	b.jevAvailable.Store(true)
	risky := false
	b.getAgent().Executor().SetBashAutoJudge(func(context.Context, string) (bool, bool) { return risky, true })
	authority := runner.NewExecutor(nil)
	b.getAgent().Executor().ConfigureChildPermissions(authority)
	b.permissionAuthority.ConfigureChildPermissionMode(authority)
	worker := runner.NewExecutor(tools.New(approvalModeProbeTool{}))
	authority.ConfigureChildPermissions(worker)
	asked := 0
	worker.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply { asked++; return protocol.AllowOnce() })
	check := func(want string, wantAsked int) {
		t.Helper()
		if got := worker.AccessObservation().Mode; got != want {
			t.Fatalf("worker approval mode = %s, want %s", got, want)
		}
		before := asked
		result := worker.ExecuteBatch(t.Context(), []toolcore.ToolCallItem{{ID: "mode-probe", Name: "shell", Args: map[string]any{"command": "make all"}}})[0]
		if result.Error != "" {
			t.Fatal(result.Error)
		}
		if got := asked - before; got != wantAsked {
			t.Fatalf("%s prompted %d times, want %d", want, got, wantAsked)
		}
	}
	check("manual", 1)
	b.SetFullAccess(true)
	check("full", 0)
	b.SetFullAccess(false)
	check("manual", 1)
	b.SetBashAuto(true)
	check("auto", 0)
	risky = true
	check("auto", 1)
	risky = false
	b.SetBashAuto(false)
	check("manual", 1)
	b.SetFullAccess(true)
	check("full", 0)
	// FULL suppresses asks, but a configured deny must still block execution.
	worker.SetPermissionPolicy(permission.PermissionsDecl{Deny: []string{"shell(make all)"}}, b.cwd, b.home)
	before := asked
	result := worker.ExecuteBatch(t.Context(), []toolcore.ToolCallItem{{ID: "deny-probe", Name: "shell", Args: map[string]any{"command": "make all"}}})[0]
	if result.Error == "" || asked != before {
		t.Fatalf("FULL bypassed deny or prompted instead: %+v", result)
	}
}

func TestBackgroundPermissionsStayWithOriginSession(t *testing.T) {
	b := newPersistTestBot(t)
	b.ensureSessionIdentity()
	originalID := b.CurrentSessionID()
	// Save a normal parent conversation so it can be resumed.
	b.ctxMgr.Add("user", "original session")
	if err := b.saveSession(); err != nil {
		t.Fatal(err)
	}
	b.SetFullAccess(true)
	originalWorker := runner.NewExecutor(nil)
	b.permissionAuthority.ConfigureChildPermissionMode(originalWorker)
	if _, err := b.NewSession(); err != nil {
		t.Fatal(err)
	}
	b.SetFullAccess(false)
	if !originalWorker.FullAccess() {
		t.Fatal("changing another session revoked original session policy")
	}
	otherWorker := runner.NewExecutor(nil)
	b.permissionAuthority.ConfigureChildPermissionMode(otherWorker)
	b.SetFullAccess(true)
	if !otherWorker.FullAccess() {
		t.Fatal("current-session worker did not follow FULL")
	}
	b.SetFullAccess(false)
	if otherWorker.FullAccess() {
		t.Fatal("current-session worker did not follow manual")
	}
	if err := b.ResumeSession(originalID); err != nil {
		t.Fatal(err)
	}
	if !b.FullAccess() || !originalWorker.FullAccess() {
		t.Fatal("origin session approval mode was not restored")
	}
	b.SetFullAccess(false)
	if originalWorker.FullAccess() {
		t.Fatal("origin worker ignored revocation after return")
	}
	b.SetFullAccess(true)
	if !originalWorker.FullAccess() || otherWorker.FullAccess() {
		t.Fatal("session approval modes became coupled")
	}
}

type approvalModeProbeTool struct{}

func (approvalModeProbeTool) Name() string                     { return "shell" }
func (approvalModeProbeTool) Description() string              { return "approval mode test" }
func (approvalModeProbeTool) Parameters() []toolcore.Parameter { return nil }
func (approvalModeProbeTool) ExecutionMode(map[string]any) toolcore.ExecutionMode {
	return toolcore.ModeParallel
}
func (approvalModeProbeTool) Execute(context.Context, map[string]any) (toolcore.ToolOutput, error) {
	return toolcore.ToolOutput{Text: "ran"}, nil
}

func TestConfigurationRebuildRevokesDormantFull(t *testing.T) {
	b := newPersistTestBot(t)
	b.ensureSessionIdentity()
	id := b.CurrentSessionID()
	b.ctxMgr.Add("user", "saved")
	if err := b.saveSession(); err != nil {
		t.Fatal(err)
	}
	b.SetFullAccess(true)
	worker := runner.NewExecutor(nil)
	b.permissionAuthority.ConfigureChildPermissionMode(worker)
	if _, err := b.NewSession(); err != nil {
		t.Fatal(err)
	}
	b.SetFullAccess(false)
	if err := b.ApplyConfiguration(b.Configuration()); err != nil {
		t.Fatal(err)
	}
	if worker.FullAccess() {
		t.Error("dormant worker retained FULL")
	}
	if err := b.ResumeSession(id); err != nil {
		t.Fatal(err)
	}
	if b.FullAccess() {
		t.Error("dormant session restored revoked FULL")
	}
}

type permissionCaptureSubmitHost struct{}

func (permissionCaptureSubmitHost) SubmitBackground(_ context.Context, work protocol.BackgroundWork) (protocol.BackgroundReceipt, error) {
	if work.Release != nil {
		work.Release()
	}
	return protocol.BackgroundReceipt{ID: work.ID, Status: "queued"}, nil
}
func (permissionCaptureSubmitHost) CancelBackground(context.Context, string) error { return nil }
func TestBackgroundPermissionCaptureConcurrentModeChange(t *testing.T) {
	b := newPersistTestBot(t)
	b.ensureSessionIdentity()
	ag := b.getAgent()
	cfg := b.Configuration()
	definition, _ := mode.Resolve(mode.SuperAgent)
	ctx := protocol.WithBackgroundSubmitter(mode.WithSnapshot(t.Context(), definition.Snapshot), permissionCaptureSubmitHost{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			b.SetFullAccess(i%2 == 0)
		}
	}()
	for i := 0; i < 40; i++ {
		if _, err := b.submitBackgroundTask(ctx, taskbridge.TaskSpec{Prompt: "inspect"}, config.ModelConfig{}, cfg, nil, ag); err != nil {
			t.Error(err)
			break
		}
	}
	wg.Wait()
}

func TestDeleteSessionReleasesOnlyItsPermissionAuthority(t *testing.T) {
	b := newPersistTestBot(t)
	b.ensureSessionIdentity()
	oldID := b.CurrentSessionID()
	b.ctxMgr.Add("user", "old session")
	if err := b.saveSession(); err != nil {
		t.Fatal(err)
	}
	b.SetFullAccess(true)
	oldWorker := runner.NewExecutor(nil)
	b.permissionAuthority.ConfigureChildPermissionMode(oldWorker)
	if _, err := b.NewSession(); err != nil {
		t.Fatal(err)
	}
	b.ctxMgr.Add("user", "current session")
	if err := b.saveSession(); err != nil {
		t.Fatal(err)
	}
	currentID := b.CurrentSessionID()
	currentWorker := runner.NewExecutor(nil)
	b.permissionAuthority.ConfigureChildPermissionMode(currentWorker)
	b.SetFullAccess(true)
	if err := b.DeleteSession(oldID); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	_, retained := b.permissionAuthorities[oldID]
	b.mu.Unlock()
	if retained || oldWorker.FullAccess() {
		t.Error("deleted session retained its authority or FULL")
	}
	if !b.FullAccess() || !currentWorker.FullAccess() {
		t.Error("deleting another session changed current approvals")
	}
	if err := b.DeleteSession(currentID); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	_, retained = b.permissionAuthorities[currentID]
	b.mu.Unlock()
	if retained || currentWorker.FullAccess() || b.FullAccess() {
		t.Error("current session deletion kept stale FULL authority")
	}
	b.SetFullAccess(true)
	if !b.FullAccess() || currentWorker.FullAccess() {
		t.Error("draft reused deleted session authority")
	}
}
