//go:build unix

package diff

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"nekocode/bot/extension/tool/runtime/workspace"
	utilfs "nekocode/util/fs"
)

func TestDiffRejectsFIFOWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "input.pipe")
	if err := unix.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	ctx := workspace.WithManager(t.Context(), workspace.New(dir, nil))
	done := make(chan error, 1)
	go func() {
		_, err := NewTool().Execute(ctx, map[string]any{"old": "path:" + path, "new": ""})
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, utilfs.ErrNotRegular) {
			t.Fatalf("FIFO accepted: %v", err)
		}
	case <-time.After(time.Second):
		// Unblock the old implementation before failing, so the test leaks no reader.
		f, err := os.OpenFile(path, os.O_WRONLY|unix.O_NONBLOCK, 0600)
		if err == nil {
			f.Close()
			<-done
		}
		t.Fatal("diff blocked while opening a FIFO")
	}
}
