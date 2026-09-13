package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/checkpoint"
	"nekocode/bot/command"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool/builtin/catalog"
	"nekocode/bot/extension/tool/builtin/shell"
	"nekocode/bot/extension/tool/runtime/workspace"
	"nekocode/bot/provider/types"
	"nekocode/bot/session"
	"nekocode/util/fs"
)

func TestRewindMenuShowsUserMessagesAndChangedFiles(t *testing.T) {
	cwd := t.TempDir()
	manager := session.New(cwd)
	cp := checkpoint.New(t.TempDir())
	cp.Activate(manager.CurrentID(), nil, 0)
	turn, err := cp.BeginMessage(manager.CurrentID(), "Update the main entrypoint")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cwd, "main.go")
	if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cp.Capture(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("after"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cp.Finalize(path); err != nil {
		t.Fatal(err)
	}
	if err := cp.Finish(manager.CurrentID()); err != nil {
		t.Fatal(err)
	}
	if _, err := cp.BeginMessage(manager.CurrentID(), "Review the result"); err != nil {
		t.Fatal(err)
	}
	if err := cp.Finish(manager.CurrentID()); err != nil {
		t.Fatal(err)
	}

	contextManager := ctxmgr.New(ctxmgr.Config{})
	b := &Bot{cwd: cwd, sess: manager, checkpoints: cp, ctxMgr: contextManager}
	parser := command.NewParser()
	parser.Register("rewind", func(context.Context, *command.Command) (string, bool) { return "", true })
	b.registerCommandMenus(parser)
	menu, ok := parser.Menu(context.Background(), "/rewind")
	if !ok || len(menu.Items) != 2 || menu.Items[0].Label != "Review the result" ||
		!strings.Contains(menu.Items[0].Description, "Latest message") ||
		!strings.Contains(menu.Items[0].Description, "0 files") ||
		menu.Items[1].Value != "/rewind "+turn || menu.Items[1].Label != "Update the main entrypoint" ||
		!menu.Items[1].Submit || !strings.Contains(menu.Items[1].Description, "1 messages ago") ||
		!strings.Contains(menu.Items[1].Description, "1 files · +0 ~1 -0") {
		t.Fatalf("rewind menu = %+v, %v", menu, ok)
	}
	message, err := b.rewindCheckpoint(turn)
	if err != nil || !strings.Contains(message, `Rewound to "Update the main entrypoint"`) || !strings.Contains(message, "1 files across 1 directories") {
		t.Fatalf("rewind = %q, %v", message, err)
	}
	messages := contextManager.Snapshot().Messages
	event := messages[len(messages)-1]
	if event.Source != types.MessageSourceRuntimeEvent ||
		!strings.Contains(event.Content, `"rewind_id": ".rewind-`) ||
		!strings.Contains(event.Content, filepath.ToSlash(path)) ||
		!strings.Contains(event.Content, filepath.ToSlash(cwd)) ||
		!strings.Contains(event.Content, `"action": "restored_previous_file"`) {
		t.Fatalf("rewind event = %+v", event)
	}
	if pending := cp.Recovered(manager.CurrentID()); len(pending) != 0 {
		t.Fatalf("persisted rewind journal was not acknowledged: %+v", pending)
	}
}

func TestInitSessionBacksUpSnapshotBeforeCompaction(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := &Bot{cwd: t.TempDir(), ctxMgr: ctxmgr.New(ctxmgr.Config{
		Summarizer: func([]types.Message, string) (string, error) {
			return "<summary>durable summary of the earlier conversation messages</summary>", nil
		},
	})}
	b.initSession()
	for range 8 {
		b.ctxMgr.Add("user", "old question")
		b.ctxMgr.Add("assistant", "old answer")
	}
	if applied, err := b.ctxMgr.Summarize(context.Background()); err != nil || !applied {
		t.Fatalf("compact = %v, %v", applied, err)
	}
	backups, err := filepath.Glob(filepath.Join(fs.NekocodeHome(), "sessions", b.sess.CurrentID(), "backups", "pre-compact-*.session.json"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups = %v, %v", backups, err)
	}
	data, err := os.ReadFile(backups[0])
	if err != nil || !strings.Contains(string(data), `"old question"`) {
		t.Fatalf("pre-compaction backup missing history: %v", err)
	}
	if got := b.ctxMgr.Snapshot(); len(got.Transcript) != 16 || len(got.Messages) >= 16 {
		t.Fatalf("transcript/context after compaction = %d/%d", len(got.Transcript), len(got.Messages))
	}
}

func TestCheckpointChangeHelpersDoNotTreatUnknownAsModified(t *testing.T) {
	turn := checkpoint.TurnInfo{Changes: []checkpoint.FileChange{
		{Kind: checkpoint.ChangeCreated},
		{Kind: checkpoint.ChangeModified},
		{Kind: checkpoint.ChangeDeleted},
		{Kind: checkpoint.ChangeKind("renamed")},
	}}
	created, modified, deleted := checkpointChangeCounts(turn)
	if created != 1 || modified != 1 || deleted != 1 {
		t.Fatalf("change counts = +%d ~%d -%d", created, modified, deleted)
	}
}

func TestResumePersistsCommittedRewindEventBeforeAcknowledgingJournal(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()
	checkpointRoot := t.TempDir()
	manager := session.New(cwd)
	sessionID := manager.CurrentID()
	cp := checkpoint.New(checkpointRoot)
	cp.Activate(sessionID, nil, 0)
	path := filepath.Join(cwd, "state.txt")
	if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	turn, err := cp.BeginMessage(sessionID, "change state")
	if err != nil {
		t.Fatal(err)
	}
	if err := cp.Capture(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("after"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cp.Finalize(path); err != nil {
		t.Fatal(err)
	}
	if err := cp.Finish(sessionID); err != nil {
		t.Fatal(err)
	}
	snapshot := manager.Current()
	snapshot.Messages = []types.Message{{Role: "user", Content: "change state"}}
	snapshot.CheckpointTurns = cp.Index(sessionID)
	snapshot.CheckpointNext = cp.Next(sessionID)
	if err := manager.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	result, err := cp.Rewind(sessionID, turn)
	if err != nil {
		t.Fatal(err)
	}
	if len(cp.Recovered(sessionID)) != 1 {
		t.Fatal("committed rewind journal was not retained for session recovery")
	}

	reloadedCP := checkpoint.New(checkpointRoot)
	b := &Bot{cwd: cwd, sess: manager, checkpoints: reloadedCP, ctxMgr: ctxmgr.New(ctxmgr.Config{})}
	if _, err := b.resumeSession(sessionID); err != nil {
		t.Fatal(err)
	}
	if !b.hasRewindEvent(result.RewindID) {
		t.Fatal("resume did not append the committed rewind workspace event")
	}
	if pending := reloadedCP.Recovered(sessionID); len(pending) != 0 {
		t.Fatalf("recovered journal was not acknowledged: %+v", pending)
	}
	persisted, err := manager.Load(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, message := range persisted.Messages {
		found = found || strings.Contains(message.Content, result.RewindID)
	}
	if !found {
		t.Fatal("recovered rewind event was not persisted in the session")
	}
}

func TestSessionCommandResumesDirectManager(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()

	seed := session.New(cwd)
	target := seed.Current()
	target.Messages = []types.Message{{Role: "user", Content: "hello"}}
	if err := seed.Save(target); err != nil {
		t.Fatal(err)
	}

	contextManager := ctxmgr.New(ctxmgr.Config{})
	manager := session.New(cwd)

	b := &Bot{sess: manager, ctxMgr: contextManager}
	parser := command.NewParser()
	b.registerSessionCommands(parser)
	menu, ok := parser.Menu(context.Background(), "/sessions")
	if !ok || len(menu.Items) != 1 || menu.Items[0].Key != target.ID || menu.Items[0].Value != "/sessions "+target.ID {
		t.Fatalf("sessions menu = %+v, %v", menu, ok)
	}
	if _, handled := parser.Execute(context.Background(), parser.Parse("/sessions "+target.ID)); !handled {
		t.Fatal("sessions command was not handled")
	}
	if manager.CurrentID() != target.ID {
		t.Fatalf("current session = %q, want %q", manager.CurrentID(), target.ID)
	}
}

func TestEnsureSessionIdentitySyncsManagedProcesses(t *testing.T) {
	manager := session.New(t.TempDir())
	manager.ClearCurrent()
	toolbox := catalog.NewToolbox(nil)
	t.Cleanup(func() { _ = toolbox.Close() })
	b := &Bot{sess: manager, toolbox: toolbox}

	b.ensureSessionIdentity()
	if manager.CurrentID() == "" {
		t.Fatal("session identity was not created")
	}
	registered, err := toolbox.Registry.Get("shell")
	if err != nil {
		t.Fatal(err)
	}
	shellTool := registered.(*shell.ShellTool)
	if shellTool.CurrentSessionID() != manager.CurrentID() {
		t.Fatalf("shell owner = %q, session = %q", shellTool.CurrentSessionID(), manager.CurrentID())
	}
}

func TestBotNewAndDeleteSessionResetCurrentConversation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()
	contextManager := ctxmgr.New(ctxmgr.Config{})
	contextManager.Restore(ctxmgr.ManagerSnapshot{
		Archive: "old summary", Messages: []types.Message{{Role: "user", Content: "old conversation"}},
	})
	manager := session.New(cwd)
	b := &Bot{cwd: cwd, ctxMgr: contextManager, sess: manager}

	meta, err := b.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	if meta.ID == "" || manager.CurrentID() != meta.ID {
		t.Fatalf("new session = %#v, current = %q", meta, manager.CurrentID())
	}
	if got := len(contextManager.Snapshot().Messages); got != 0 {
		t.Fatalf("new session retained %d old messages", got)
	}
	if archive := contextManager.Snapshot().Archive; archive != "" {
		t.Fatalf("new session retained old archive %q", archive)
	}

	if err := b.DeleteSession(meta.ID); err != nil {
		t.Fatal(err)
	}
	if manager.CurrentID() != "" {
		t.Fatalf("deleted session remained current: %q", manager.CurrentID())
	}
}

func TestResetConversationStopsOldSessionProcesses(t *testing.T) {
	cwd := t.TempDir()
	manager := session.New(cwd)
	toolbox := catalog.NewToolbox(nil)
	t.Cleanup(func() { _ = toolbox.Close() })
	oldID := manager.CurrentID()
	toolbox.SetSessionID(oldID)
	b := &Bot{
		cwd: cwd, ctxMgr: ctxmgr.New(ctxmgr.Config{}), sess: manager, toolbox: toolbox,
	}

	registered, err := toolbox.Registry.Get("shell")
	if err != nil {
		t.Fatal(err)
	}
	shellTool := registered.(*shell.ShellTool)
	ctx := workspace.WithManager(context.Background(), toolbox.Workspace())
	if _, err := shellTool.Execute(ctx, map[string]any{"command": "sleep 5", "name": "service"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(shellTool.ProcessSummary(), "service(running") {
		t.Fatalf("managed process did not start: %q", shellTool.ProcessSummary())
	}

	if _, err := b.resetConversation(); err != nil {
		t.Fatal(err)
	}
	shellTool.SetSessionID(oldID)
	if strings.Contains(shellTool.ProcessSummary(), "(running") {
		t.Fatalf("old session process survived reset: %q", shellTool.ProcessSummary())
	}
}
