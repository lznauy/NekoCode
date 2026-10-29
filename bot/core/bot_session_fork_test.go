package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/agent"
	"nekocode/bot/checkpoint"
	"nekocode/bot/command"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool/builtin/catalog"
	"nekocode/bot/extension/tool/runtime/workspace"
	"nekocode/bot/session"
	"nekocode/util/attachment"
)

func TestForkCommandSwitchesAndKeepsOriginalHistory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()
	manager := session.New(cwd)
	contextManager := ctxmgr.New(ctxmgr.Config{})
	contextManager.Add("user", "original question", "")
	contextManager.Add("assistant", "original answer", "")
	b := &Bot{cwd: cwd, sess: manager, ctxMgr: contextManager, checkpoints: checkpoint.New(t.TempDir())}
	b.checkpoints.Activate(manager.CurrentID(), nil, 0)
	b.cmd = command.New(command.Deps{CtxMgr: contextManager})
	b.registerSessionCommands(b.cmd.Parser())
	original := manager.CurrentID()
	if _, during := b.cmd.Parser().CommandAvailability("/fork"); during {
		t.Fatal("fork can run during active work")
	}
	out, handled := b.cmd.Parser().Execute(t.Context(), b.cmd.Parser().Parse("/fork"))
	if !handled || !strings.Contains(out, "Forked session") {
		t.Fatalf("fork: %s %v", out, handled)
	}
	branch := manager.CurrentID()
	if branch == original {
		t.Fatal("did not switch")
	}
	if got := b.Conversation(); len(got.Messages) != 2 || got.Messages[0].Content != "original question" {
		t.Fatalf("context: %+v", got)
	}
	contextManager.Add("user", "try alternative", "")
	if e := b.saveSession(); e != nil {
		t.Fatal(e)
	}
	if _, e := b.resumeSession(original); e != nil {
		t.Fatal(e)
	}
	if got := b.Conversation(); len(got.Messages) != 2 {
		t.Fatal("branch messages leaked into original")
	}
	if _, e := b.resumeSession(branch); e != nil {
		t.Fatal(e)
	}
	if got := b.Conversation(); len(got.Messages) != 3 || got.Messages[2].Content != "try alternative" {
		t.Fatal("branch continuation lost")
	}
	// Forking does not change workspace contents or create inherited checkpoints.
	file := filepath.Join(cwd, "same.txt")
	if e := os.WriteFile(file, []byte("shared workspace"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := b.ForkSession(t.Context()); e != nil {
		t.Fatal(e)
	}
	if got, _ := os.ReadFile(file); string(got) != "shared workspace" {
		t.Fatal("fork changed workspace file")
	}
	if saved, e := manager.Load(branch); e != nil || len(saved.Transcript) != 3 || saved.ForkedFrom != original {
		t.Fatalf("saved branch: %+v %v", saved, e)
	}
}
func TestForkSessionRejectsEmptyWithoutChangingIdentity(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := &Bot{sess: session.New(t.TempDir()), ctxMgr: ctxmgr.New(ctxmgr.Config{})}
	id := b.sess.CurrentID()
	if _, e := b.ForkSession(t.Context()); e == nil {
		t.Fatal("forked empty conversation")
	}
	if b.sess.CurrentID() != id {
		t.Fatal("empty fork changed identity")
	}
}

func TestForkResetsFileStateAndDropsTemporaryRoots(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()
	m := session.New(cwd)
	cm := ctxmgr.New(ctxmgr.Config{})
	toolbox := catalog.NewToolboxWithConfig(catalog.ToolboxConfig{Workspace: workspace.New(cwd, nil)})
	defer toolbox.Close()
	b := &Bot{cwd: cwd, sess: m, ctxMgr: cm, toolbox: toolbox, checkpoints: checkpoint.New(t.TempDir())}
	b.ag = agent.New(t.Context(), agent.Config{Context: cm, Tools: toolbox.Registry})
	b.checkpoints.Activate(m.CurrentID(), nil, 0)
	b.bindSessionResources()
	extra := t.TempDir()
	if _, e := toolbox.Workspace().AddSessionRoot(extra, workspace.AccessReadWrite); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(cwd, "cached.txt")
	if e := os.WriteFile(path, []byte("old read"), 0600); e != nil {
		t.Fatal(e)
	}
	b.ag.ToolExecutionState().FileCache.Put(path, []string{"old read"}, 1, 1)
	cm.Add("user", "continue from this point", "")
	parent := m.CurrentID()
	fork, e := b.ForkSession(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	if _, _, allowed, e := toolbox.Workspace().CheckRead(filepath.Join(extra, "file.txt")); e != nil || allowed {
		t.Fatalf("inherited temporary root: %v %v", allowed, e)
	}
	if _, ok := b.ag.ToolExecutionState().FileCache.Lines(path); ok {
		t.Fatal("inherited parent read authority")
	}
	parentImages, _ := attachment.ImageDir(parent)
	branchImages, _ := attachment.ImageDir(fork.ID)
	if _, _, allowed, e := toolbox.Workspace().CheckRead(filepath.Join(parentImages, "old.png")); e != nil || allowed {
		t.Fatalf("parent image authority remains: %v %v", allowed, e)
	}
	if _, _, allowed, e := toolbox.Workspace().CheckRead(filepath.Join(branchImages, "new.png")); e != nil || !allowed {
		t.Fatalf("branch image authority missing: %v %v", allowed, e)
	}
	if _, _, allowed, e := toolbox.Workspace().CheckWrite(filepath.Join(branchImages, "new.png")); e != nil || allowed {
		t.Fatalf("branch attachments writable: %v %v", allowed, e)
	}
}
