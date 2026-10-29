//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package extension

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"nekocode/bot/command"
	ctxmgr "nekocode/bot/contextmgr"
)

func TestPluginCheckCommandRejectsFIFOWithoutBlockingOperations(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("HOME", t.TempDir())
	dir := agentFixture(t, root, "demo", `"agents/review.md"`)
	manifest := filepath.Join(dir, ".claude-plugin", "plugin.json")
	if err := os.WriteFile(manifest, []byte(`{"name":"demo","hooks":{"source":"hooks.json"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "hooks.json")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	m := newAgentManager()
	defer m.Close()
	m.Load()
	cm := ctxmgr.New(ctxmgr.Config{})
	handler := command.New(command.Deps{CtxMgr: cm})
	m.RegisterCommands(handler, nil)

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	type commandResult struct {
		report  string
		handled bool
	}
	done := make(chan commandResult, 1)
	go func() {
		report, handled := handler.Execute(ctx, "/plugin check demo", cm)
		done <- commandResult{report: report, handled: handled}
	}()
	select {
	case result := <-done:
		if !result.handled || !strings.Contains(result.report, "[error] hooks.json") || !strings.Contains(result.report, "regular file") {
			t.Fatalf("FIFO check report = %q, handled = %t", result.report, result.handled)
		}
	case <-ctx.Done():
		releasePluginCheckFIFO(fifo)
		t.Fatal("FIFO blocked /plugin check")
	}

	canceled, cancelCheck := context.WithCancel(t.Context())
	cancelCheck()
	checkDone := make(chan error, 1)
	go func() {
		_, err := m.CheckPlugins(canceled, "")
		checkDone <- err
	}()
	select {
	case err := <-checkDone:
		if err != context.Canceled {
			t.Fatalf("follow-up check error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("/plugin check left manager operations locked")
	}
}

func releasePluginCheckFIFO(path string) {
	file, err := os.OpenFile(path, os.O_RDWR|syscall.O_NONBLOCK, 0)
	if err == nil {
		_ = file.Close()
	}
}
