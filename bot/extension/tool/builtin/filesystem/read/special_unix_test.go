//go:build unix

package read

import (
	"context"
	"golang.org/x/sys/unix"
	"nekocode/bot/extension/tool/runtime/workspace"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadFIFOStopsOnCancellation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "input.txt")
	if err := unix.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(workspace.WithManager(context.Background(), workspace.New(dir, nil)))
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := (&ReadTool{}).Execute(ctx, map[string]any{"path": path, "startLine": float64(1), "endLine": float64(1)})
		done <- err
	}()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case <-done:
		return
	case <-time.After(100 * time.Millisecond):
	}
	// Unblock the existing implementation so the test leaves no worker behind.
	f, err := os.OpenFile(path, os.O_WRONLY|unix.O_NONBLOCK, 0600)
	if err == nil {
		f.Write([]byte("done\n"))
		f.Close()
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reader never settled")
	}
	t.Fatal("read of a FIFO remained blocked after cancellation")
}
