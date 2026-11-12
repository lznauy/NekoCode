package core

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/config"
	"nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/bot/session"
	"nekocode/mode"
	"nekocode/protocol"
)

type isolationCaptureHost struct{ work protocol.BackgroundWork }

func (h *isolationCaptureHost) SubmitBackground(_ context.Context, work protocol.BackgroundWork) (protocol.BackgroundReceipt, error) {
	h.work = work
	if work.Release != nil {
		work.Release()
	}
	return protocol.BackgroundReceipt{ID: work.ID, Status: "queued"}, nil
}
func (*isolationCaptureHost) CancelBackground(context.Context, string) error { return nil }

type isolationTestWorker struct{}

func (isolationTestWorker) Step(protocol.StepEvent) {}
func (isolationTestWorker) Confirm(protocol.ConfirmRequest) protocol.ConfirmReply {
	return protocol.ConfirmReply{}
}
func (isolationTestWorker) Ask(protocol.QuestionRequest) protocol.QuestionReply {
	return protocol.QuestionReply{}
}

func TestBackgroundAutomaticIsolationAndSavedWorkspace(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	b := newPersistTestBot(t)
	b.ensureSessionIdentity()
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git: %s %v", out, err)
		}
	}
	git("init")
	git("config", "user.name", "Fixture")
	git("config", "user.email", "fixture@example.invalid")
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("base\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "base")
	definition, _ := mode.Resolve(mode.SuperAgent)
	host := &isolationCaptureHost{}
	ctx := protocol.WithCoordination(protocol.WithBackgroundSubmitter(mode.WithSnapshot(t.Context(), definition.Snapshot), host), protocol.Coordination{GroupID: "isolation-demand"})
	submit := func(spec taskbridge.TaskSpec) *taskbridge.TaskResult {
		t.Helper()
		r, err := b.submitBackgroundTask(ctx, spec, config.ModelConfig{}, b.Configuration(), nil, b.getAgent())
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	spec := taskbridge.TaskSpec{Prompt: "inspect", RequiredTools: []string{"shell"}}
	result := submit(spec)
	if host.work.Isolation != "worktree" || !strings.Contains(host.work.Workspace, "#worktree:") || !strings.Contains(result.Content, "Isolation: worktree") {
		t.Fatalf("clean repository remained shared: %+v %s", host.work, result.Content)
	}
	automatic := host.work
	spec.Isolation = "shared"
	submit(spec)
	if host.work.Isolation != "shared" || host.work.Workspace != root {
		t.Fatalf("explicit shared ignored: %+v", host.work)
	}
	// Changes arriving while queued must fail preflight, rather than inspect stale HEAD.
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := automatic.Run(t.Context(), isolationTestWorker{}); err == nil || !strings.Contains(err.Error(), "preflight changed: uncommitted_changes") {
		t.Fatalf("stale automatic baseline accepted: %v", err)
	}
	// A failure before the first worker attempt is not a legacy task. The
	// unchanged task cannot be resumed; diagnose setup rather than a migration.
	_, resumeErr := b.submitBackgroundTask(ctx, taskbridge.TaskSpec{ResumeID: automatic.ID, Prompt: "continue setup"}, config.ModelConfig{}, b.Configuration(), nil, b.getAgent())
	if resumeErr == nil || !strings.Contains(resumeErr.Error(), "before its first execution attempt") || strings.Contains(resumeErr.Error(), "legacy") {
		t.Fatalf("preflight failure produced misleading recovery: %v", resumeErr)
	}
	spec.Isolation = ""
	result = submit(spec)
	if host.work.Isolation != "worktree" || host.work.SchedulingNote != "workspace_snapshot" || !strings.Contains(result.Content, "snapshot") {
		t.Fatalf("dirty checkout lost scope: %+v", host.work)
	}
	child, err := session.OpenSubagent(b.CurrentSessionID(), "saved-shared", root, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = child.SetIsolation("shared"); err != nil {
		t.Fatal(err)
	}
	if err = child.SetToolCeiling([]string{"shell"}); err != nil {
		t.Fatal(err)
	}
	if err = child.Begin("executor", "previous", nil); err != nil {
		t.Fatal(err)
	}
	if err = child.Checkpoint(contextmgr.ManagerSnapshot{}, "completed", "previous result"); err != nil {
		t.Fatal(err)
	}
	if err = child.Close(); err != nil {
		t.Fatal(err)
	}
	git("checkout", "--", "file.txt")
	spec.ResumeID = "saved-shared"
	submit(spec)
	if host.work.Isolation != "shared" {
		t.Fatal("resume moved saved task into a new workspace")
	}
}

func TestBackgroundDependenciesRequireExplicitWorkspace(t *testing.T) {
	b := newPersistTestBot(t)
	b.ensureSessionIdentity()
	definition, _ := mode.Resolve(mode.SuperAgent)
	for _, spec := range []taskbridge.TaskSpec{
		{Prompt: "verify upstream changes", RequiredTools: []string{"shell"}, After: []string{"upstream"}},
		{Prompt: "inspect shared report", ReadOnly: true, InputRefs: []string{"report-ref"}},
	} {
		host := &isolationCaptureHost{}
		ctx := protocol.WithBackgroundSubmitter(mode.WithSnapshot(t.Context(), definition.Snapshot), host)
		_, err := b.submitBackgroundTask(ctx, spec, config.ModelConfig{}, b.Configuration(), nil, b.getAgent())
		if err == nil || !strings.Contains(err.Error(), "share reports only") {
			t.Errorf("ambiguous workspace was not rejected clearly: %+v %v", spec, err)
		}
		if host.work.ID != "" {
			t.Error("ambiguous dependency reached admission")
		}
	}
}

func TestBackgroundReportDependenciesAcceptExplicitWorkspace(t *testing.T) {
	b := newPersistTestBot(t)
	b.ensureSessionIdentity()
	definition, _ := mode.Resolve(mode.SuperAgent)
	for _, isolation := range []string{"shared", "worktree"} {
		host := &isolationCaptureHost{}
		ctx := protocol.WithBackgroundSubmitter(mode.WithSnapshot(t.Context(), definition.Snapshot), host)
		_, err := b.submitBackgroundTask(ctx, taskbridge.TaskSpec{Prompt: "analyze reports only", After: []string{"upstream"}, Isolation: isolation}, config.ModelConfig{}, b.Configuration(), nil, b.getAgent())
		if err != nil || host.work.Isolation != isolation {
			t.Fatalf("explicit report workspace rejected: %+v %v", host.work, err)
		}
	}
}

func TestBackgroundDispatchContinuesExplicitlyRestoredTodoPlan(t *testing.T) {
	t.Chdir(t.TempDir())
	b := newPersistTestBot(t)
	b.ensureSessionIdentity()
	b.ctxMgr.SelectTodoGroup("saved-plan")
	definition, _ := mode.Resolve(mode.SuperAgent)
	host := &isolationCaptureHost{}
	ctx := protocol.WithBackgroundSubmitter(mode.WithSnapshot(t.Context(), definition.Snapshot), host)
	_, err := b.submitBackgroundTask(ctx, taskbridge.TaskSpec{Prompt: "continue work", Isolation: "shared"}, config.ModelConfig{}, b.Configuration(), nil, b.getAgent())
	if err != nil || host.work.GroupID != "saved-plan" {
		t.Fatalf("worker detached from restored plan: %+v %v", host.work, err)
	}
	ctx = protocol.WithCoordination(ctx, protocol.Coordination{GroupID: "notification-plan", Notification: true})
	_, err = b.submitBackgroundTask(ctx, taskbridge.TaskSpec{Prompt: "follow up", Isolation: "shared"}, config.ModelConfig{}, b.Configuration(), nil, b.getAgent())
	if err != nil || host.work.GroupID != "notification-plan" {
		t.Fatalf("notification switched plan scope: %+v %v", host.work, err)
	}
}

func TestBackgroundResumeUsesCanonicalDemandWithoutRetainedRuntimeHistory(t *testing.T) {
	t.Chdir(t.TempDir())
	b := newPersistTestBot(t)
	b.ensureSessionIdentity()
	child, err := session.OpenSubagent(b.CurrentSessionID(), "owned-task", b.cwd, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = child.SetGroupID("original-plan"); err != nil {
		t.Fatal(err)
	}
	if err = child.SetToolCeiling([]string{"shell"}); err != nil {
		t.Fatal(err)
	}
	if err = child.SetIsolation("shared"); err != nil {
		t.Fatal(err)
	}
	if err = child.Begin("executor", "inspect", nil); err != nil {
		t.Fatal(err)
	}
	if err = child.Checkpoint(contextmgr.ManagerSnapshot{}, "completed", "done"); err != nil {
		t.Fatal(err)
	}
	if err = child.Close(); err != nil {
		t.Fatal(err)
	}
	b.ctxMgr.SelectTodoGroup("new-run")
	host := &isolationCaptureHost{}
	definition, _ := mode.Resolve(mode.SuperAgent)
	ctx := protocol.WithBackgroundSubmitter(mode.WithSnapshot(t.Context(), definition.Snapshot), host)
	spec := taskbridge.TaskSpec{ResumeID: "owned-task", Prompt: "continue"}
	if _, err = b.submitBackgroundTask(ctx, spec, config.ModelConfig{}, b.Configuration(), nil, b.getAgent()); err != nil {
		t.Fatal(err)
	}
	if host.work.GroupID != "original-plan" {
		t.Fatalf("resume reassigned canonical ownership: %q", host.work.GroupID)
	}
	ctx = protocol.WithCoordination(ctx, protocol.Coordination{GroupID: "unrelated", Notification: true})
	host.work = protocol.BackgroundWork{}
	if _, err = b.submitBackgroundTask(ctx, spec, config.ModelConfig{}, b.Configuration(), nil, b.getAgent()); err == nil || host.work.ID != "" {
		t.Fatal("notification resumed another demand")
	}
}
