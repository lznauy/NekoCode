package core

import (
	"context"
	"nekocode/bot/config"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/bot/session"
	"nekocode/mode"
	"nekocode/protocol"
	"strings"
	"testing"
)

func TestStandardCannotCreateOrResumeExecutor(t *testing.T) {
	b := newPersistTestBot(t)
	task, err := b.toolbox.Registry.Get("task")
	if err != nil {
		t.Fatal(err)
	}
	standard, _ := mode.Resolve(mode.Standard)
	ctx := mode.WithSnapshot(t.Context(), standard.Snapshot)
	if _, err := task.Execute(ctx, map[string]any{"profile": "executor", "prompt": "implement"}); err == nil || !strings.Contains(err.Error(), "coordinator") {
		t.Fatalf("standard created executor: %v", err)
	}
	parent := b.CurrentSessionID()
	child, err := session.OpenSubagent(parent, "executor-task", b.cwd, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := child.SetToolCeiling([]string{"read"}); err != nil {
		t.Fatal(err)
	}
	if err := child.Begin("executor", "read", nil); err != nil {
		t.Fatal(err)
	}
	if err := child.Checkpoint(ctxmgr.ManagerSnapshot{}, "completed", "done"); err != nil {
		t.Fatal(err)
	}
	child.Close()
	if _, err := task.Execute(ctx, map[string]any{"resume_id": "executor-task", "prompt": "continue"}); err == nil || !strings.Contains(err.Error(), "coordinator") {
		t.Fatalf("standard resumed executor: %v", err)
	}
	saved, err := session.ReadSubagentMetadata(parent, "executor-task")
	if err != nil || saved.RunCount != 1 {
		t.Fatalf("rejected resume changed saved task: %+v %v", saved, err)
	}
}

type executorOnlySubmitHost struct{}

func (executorOnlySubmitHost) SubmitBackground(context.Context, protocol.BackgroundWork) (protocol.BackgroundReceipt, error) {
	panic("legacy profile reached submission")
}
func (executorOnlySubmitHost) CancelBackground(context.Context, string) error { return nil }
func TestSuperAgentRejectsLegacyProfileAtAssemblyBoundary(t *testing.T) {
	b := newPersistTestBot(t)
	definition, _ := mode.Resolve(mode.SuperAgent)
	ctx := protocol.WithBackgroundSubmitter(mode.WithSnapshot(t.Context(), definition.Snapshot), executorOnlySubmitHost{})
	for _, profile := range []string{"explore", "coder", "plugin/custom"} {
		if _, err := b.submitBackgroundTask(ctx, taskbridge.TaskSpec{Profile: profile}, config.ModelConfig{}, config.Config{}, nil, nil); err == nil {
			t.Fatalf("assembly accepted %s", profile)
		}
	}
	child, err := session.OpenSubagent(b.CurrentSessionID(), "legacy-task", b.cwd, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Begin("explore", "inspect", nil); err != nil {
		t.Fatal(err)
	}
	if err := child.Checkpoint(ctxmgr.ManagerSnapshot{}, "completed", "done"); err != nil {
		t.Fatal(err)
	}
	child.Close()
	if _, err := b.submitBackgroundTask(ctx, taskbridge.TaskSpec{ResumeID: "legacy-task"}, config.ModelConfig{}, config.Config{}, nil, nil); err == nil || !strings.Contains(err.Error(), "legacy") {
		t.Fatalf("legacy resume accepted: %v", err)
	}
	saved, err := session.ReadSubagentMetadata(b.CurrentSessionID(), "legacy-task")
	if err != nil || saved.RunCount != 1 {
		t.Fatalf("rejection changed legacy task: %+v %v", saved, err)
	}
}

type capabilityPreflightHost struct{ submitted int }

func (h *capabilityPreflightHost) SubmitBackground(_ context.Context, w protocol.BackgroundWork) (protocol.BackgroundReceipt, error) {
	h.submitted++
	if w.Release != nil {
		w.Release()
	}
	return protocol.BackgroundReceipt{ID: w.ID}, nil
}
func (*capabilityPreflightHost) CancelBackground(context.Context, string) error { return nil }
func TestExecutorRejectsUnavailableRequiredToolsBeforeAdmission(t *testing.T) {
	b := newPersistTestBot(t)
	definition, _ := mode.Resolve(mode.SuperAgent)
	host := &capabilityPreflightHost{}
	ctx := protocol.WithBackgroundSubmitter(mode.WithSnapshot(t.Context(), definition.Snapshot), host)
	for _, spec := range []taskbridge.TaskSpec{
		{Prompt: "review", ReadOnly: true, RequiredTools: []string{"shell"}},
		{Prompt: "review", RequiredTools: []string{"missing_tool"}},
	} {
		if _, err := b.submitBackgroundTask(ctx, spec, config.ModelConfig{}, b.Configuration(), nil, b.getAgent()); err == nil {
			t.Errorf("admitted impossible task: %+v", spec)
		}
	}
	if host.submitted != 0 {
		t.Fatalf("started %d tasks before checking capabilities", host.submitted)
	}
}

func TestExecutorGeneralReviewHasShellAndResumeCannotExpandCeiling(t *testing.T) {
	b := newPersistTestBot(t)
	b.ensureSessionIdentity()
	definition, _ := mode.Resolve(mode.SuperAgent)
	host := &capabilityPreflightHost{}
	ctx := protocol.WithBackgroundSubmitter(mode.WithSnapshot(t.Context(), definition.Snapshot), host)
	if _, err := b.submitBackgroundTask(ctx, taskbridge.TaskSpec{Prompt: "review without modifying files", RequiredTools: []string{"shell"}}, config.ModelConfig{}, b.Configuration(), nil, b.getAgent()); err != nil {
		t.Fatalf("normal review lost shell: %v", err)
	}
	if host.submitted != 1 {
		t.Fatal("general review was not admitted")
	}
	child, err := session.OpenSubagent(b.CurrentSessionID(), "narrow-task", b.cwd, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := child.SetToolCeiling([]string{"read"}); err != nil {
		child.Close()
		t.Fatal(err)
	}
	if err := child.Begin("executor", "read files", nil); err != nil {
		child.Close()
		t.Fatal(err)
	}
	if err := child.Checkpoint(ctxmgr.ManagerSnapshot{}, "completed", "read result"); err != nil {
		child.Close()
		t.Fatal(err)
	}
	child.Close()
	b.SetFullAccess(true)
	if _, err := b.submitBackgroundTask(ctx, taskbridge.TaskSpec{ResumeID: "narrow-task", Prompt: "run git", RequiredTools: []string{"shell"}}, config.ModelConfig{}, b.Configuration(), nil, b.getAgent()); err == nil {
		t.Fatal("FULL silently expanded saved capability ceiling")
	}
	if host.submitted != 1 {
		t.Fatal("incapable resumed task was admitted")
	}
}
