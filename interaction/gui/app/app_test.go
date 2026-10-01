package app

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	controlruntime "nekocode/runtime"
)

func TestSaveClipboardImageUsesSessionTemporaryDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	runner := &imageSessionRunner{cwd: t.TempDir()}
	rt := controlruntime.New(runner, controlruntime.Services{
		CurrentSessionID:        runner.CurrentSessionID,
		ListSessions:            runner.ListSessions,
		SessionMessages:         runner.SessionMessages,
		ImageAttachmentsEnabled: func() bool { return true },
	})
	app := &App{ctx: context.Background(), rt: rt}
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0, 'I', 'H', 'D', 'R'}

	path, err := app.SaveClipboardImage("data:image/png;base64," + base64.StdEncoding.EncodeToString(png))
	if err != nil {
		t.Fatal(err)
	}
	wantDir := filepath.Join(home, ".nekocode", "tmp", "images", "session_1")
	if filepath.Dir(path) != wantDir {
		t.Fatalf("saved path = %q, want directory %q", path, wantDir)
	}
	if _, ok := app.pendingImages[path]; !ok {
		t.Fatal("saved draft image was not tracked for shutdown cleanup")
	}
	if err := app.DeleteClipboardImage(path); err != nil {
		t.Fatal(err)
	}
	if _, ok := app.pendingImages[path]; ok {
		t.Fatal("deleted draft image remained tracked")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("draft image still exists: %v", err)
	}
}

func TestSaveClipboardImageRequiresConfiguredUnderstandingModel(t *testing.T) {
	runner := &imageSessionRunner{cwd: t.TempDir()}
	rt := controlruntime.New(runner, controlruntime.Services{
		CurrentSessionID: runner.CurrentSessionID,
		ListSessions:     runner.ListSessions,
		SessionMessages:  runner.SessionMessages,
	})
	app := &App{ctx: context.Background(), rt: rt}
	if _, err := app.SaveClipboardImage("data:image/png;base64,anything"); err == nil || !strings.Contains(err.Error(), "image_understand_models") {
		t.Fatalf("SaveClipboardImage error = %v", err)
	}
}

func TestDeleteClipboardImageRejectsUntrackedSessionImage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	runner := &imageSessionRunner{cwd: t.TempDir()}
	rt := controlruntime.New(runner, controlruntime.Services{
		CurrentSessionID: runner.CurrentSessionID,
		ListSessions:     runner.ListSessions,
		SessionMessages:  runner.SessionMessages,
	})
	app := &App{ctx: context.Background(), rt: rt}
	path := filepath.Join(home, ".nekocode", "tmp", "images", "session_1", "sent.png")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("sent image"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := app.DeleteClipboardImage(path); err == nil || !strings.Contains(err.Error(), "active draft") {
		t.Fatalf("DeleteClipboardImage error = %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("untracked session image was deleted: %v", err)
	}
}

func TestAttachmentDraftEventMatchesItsCreatedSession(t *testing.T) {
	app := &App{attachmentDraftSessionID: "draft-session"}
	if app.consumeAttachmentDraftSession("other-session") {
		t.Fatal("unrelated session event consumed the attachment draft marker")
	}
	if !app.consumeAttachmentDraftSession("draft-session") {
		t.Fatal("created attachment session was not marked as a draft event")
	}
	if app.consumeAttachmentDraftSession("draft-session") {
		t.Fatal("attachment draft marker was not consumed exactly once")
	}
}

func TestFailedImageSavePreservesDraftWhileCreatedSessionEventSynchronizes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".nekocode", "tmp"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".nekocode", "tmp", "images"), []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	current := ""
	runner := &imageSessionRunner{cwd: t.TempDir()}
	rt := controlruntime.New(runner, controlruntime.Services{
		CurrentSessionID: func() string { return current },
		ListSessions:     runner.ListSessions,
		SessionMessages:  runner.SessionMessages,
		NewSession: func() (controlruntime.SessionMeta, error) {
			current = "created-session"
			return controlruntime.SessionMeta{ID: current}, nil
		},
		ImageAttachmentsEnabled: func() bool { return true },
	})
	app := &App{ctx: context.Background(), rt: rt}
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0, 'I', 'H', 'D', 'R'}
	if _, err := app.SaveClipboardImage("data:image/png;base64," + base64.StdEncoding.EncodeToString(png)); err == nil {
		t.Fatal("SaveClipboardImage succeeded despite blocked attachment storage")
	}
	if app.attachmentDraftSessionID != "created-session" {
		t.Fatalf("failed image save did not preserve draft event marker: %q", app.attachmentDraftSessionID)
	}
}

func TestSendMessageReturnsStartErrorAndKeepsPendingDraft(t *testing.T) {
	runner := &imageSessionRunner{cwd: t.TempDir()}
	rt := controlruntime.New(runner, controlruntime.Services{})
	app := &App{
		ctx:           context.Background(),
		rt:            rt,
		pendingImages: map[string]struct{}{`/tmp/draft.png`: {}},
	}
	err := app.startMessage("inspect [Image #1]", []controlruntime.ImageAttachment{{
		Label: "[Image #1]", Path: `/tmp/draft.png`,
	}})
	if err == nil || !strings.Contains(err.Error(), "image attachments require") {
		t.Fatalf("SendMessage error = %v", err)
	}
	if _, ok := app.pendingImages[`/tmp/draft.png`]; !ok {
		t.Fatal("failed run start committed the pending image draft")
	}
}

func TestAcceptedInputCommitsOnlyAcceptedDraftImages(t *testing.T) {
	app := &App{pendingImages: map[string]struct{}{
		`/tmp/accepted.png`:  {},
		`/tmp/preserved.png`: {},
	}}
	app.commitAcceptedImages([]controlruntime.ImageAttachment{{Path: `/tmp/accepted.png`}})
	if _, ok := app.pendingImages[`/tmp/accepted.png`]; ok {
		t.Fatal("accepted image remained part of the editable draft")
	}
	if _, ok := app.pendingImages[`/tmp/preserved.png`]; !ok {
		t.Fatal("unaccepted image was committed")
	}
}

type imageSessionRunner struct {
	cwd string
}

func (*imageSessionRunner) Run(context.Context, string, controlruntime.RunHost) (string, error) {
	return "", nil
}

func (*imageSessionRunner) CurrentSessionID() string { return "session_1" }

func (r *imageSessionRunner) ListSessions() []controlruntime.SessionMeta {
	return []controlruntime.SessionMeta{{ID: "session_1", CWD: r.cwd}}
}

func (*imageSessionRunner) SessionMessages() []controlruntime.DisplayMessage { return nil }

func TestReadImageBase64RejectsSymlinkOutsideSession(t *testing.T) {
	cwd := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.png")
	if err := os.WriteFile(outside, []byte("not really an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(cwd, "linked.png")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	runner := &imageSessionRunner{cwd: cwd}
	rt := controlruntime.New(runner, controlruntime.Services{
		CurrentSessionID: runner.CurrentSessionID,
		ListSessions:     runner.ListSessions,
		SessionMessages:  runner.SessionMessages,
	})
	app := &App{ctx: context.Background(), rt: rt}
	if _, err := app.ReadImageBase64(link); err == nil || !strings.Contains(err.Error(), "outside allowed") {
		t.Fatalf("ReadImageBase64 error = %v, want path rejection", err)
	}
}

func TestCompactConfirmArgsEditUsesV2Fields(t *testing.T) {
	req := controlruntime.ConfirmRequest{
		ToolName: "edit",
		Args: map[string]any{
			"path":       "/tmp/file.go",
			"oldString":  strings.Repeat("a", 250),
			"newString":  "next",
			"replaceAll": true,
			"patch":      "legacy",
			"_preview":   "diff",
		},
		Kind: controlruntime.ConfirmKindPermission,
	}

	got := compactConfirmArgs(req)
	if got["path"] != "/tmp/file.go" {
		t.Fatalf("path = %v", got["path"])
	}
	if _, ok := got["patch"]; ok {
		t.Fatalf("legacy patch should not be exposed: %#v", got)
	}
	if got["replaceAll"] != true {
		t.Fatalf("replaceAll = %v", got["replaceAll"])
	}
	old, _ := got["oldString"].(string)
	if len(old) != 203 || !strings.HasSuffix(old, "...") {
		t.Fatalf("oldString was not truncated: len=%d value=%q", len(old), old)
	}
}

func TestCompactConfirmArgsKeepsApprovalMetadataOutOfToolArgs(t *testing.T) {
	got := compactConfirmArgs(controlruntime.ConfirmRequest{
		ToolName: "shell",
		Args: map[string]any{
			"command": `echo "$(date)"`,
		},
		Approval: &controlruntime.ApprovalContext{Combined: true, Structures: []string{"command_substitution"}},
	})
	if len(got) != 1 || got["command"] == nil {
		t.Fatalf("approval metadata leaked into tool args: %#v", got)
	}
}

func TestReplyConfirmDecisionCarriesUnifiedApproval(t *testing.T) {
	app, id, replies := startApprovalApp(t)

	app.ReplyConfirmDecision(id, true, true)
	reply := waitConfirmReply(t, replies)
	if !reply.Allowed || !reply.Remember {
		t.Fatalf("reply = %+v, want allowed+remember", reply)
	}
}

type approvalBot struct {
	replies chan controlruntime.ConfirmReply
}

func (b *approvalBot) Run(_ context.Context, _ string, host controlruntime.RunHost) (string, error) {
	req := controlruntime.ConfirmRequest{
		ToolName: "shell",
		Args:     map[string]any{"command": "go get example.com/pkg"},
		Kind:     controlruntime.ConfirmKindPermission,
	}
	reply := host.Confirm(req)
	b.replies <- reply
	return "", nil
}

func startApprovalApp(t *testing.T) (*App, string, <-chan controlruntime.ConfirmReply) {
	t.Helper()
	bot := &approvalBot{replies: make(chan controlruntime.ConfirmReply, 1)}
	rt := controlruntime.New(bot, controlruntime.Services{})
	t.Cleanup(func() {
		if err := rt.Close(); err != nil {
			t.Error(err)
		}
	})
	app := &App{ctx: context.Background(), rt: rt}

	_, err := rt.StartRun(context.Background(), controlruntime.Input{
		Source: controlruntime.SourceRef{Kind: "test"},
		Text:   "run approval",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		run, ok := rt.CurrentRun()
		if ok && len(run.Approvals) == 1 {
			return app, run.Approvals[0].ID, bot.replies
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for pending approval")
	return nil, "", nil
}

func waitConfirmReply(t *testing.T, replies <-chan controlruntime.ConfirmReply) controlruntime.ConfirmReply {
	t.Helper()
	select {
	case reply := <-replies:
		return reply
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for confirm reply")
		return controlruntime.ConfirmReply{}
	}
}
