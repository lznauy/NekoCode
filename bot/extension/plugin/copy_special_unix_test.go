//go:build unix

package plugin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestCopyDirRejectsFIFOWithoutWaitingForWriter(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	fifo := filepath.Join(src, "pipe")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- copyDir(ctx, src, dst) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO accepted as a plugin file")
		}
	case <-time.After(200 * time.Millisecond):
		cancel()
		// Release the old implementation's blocking open before reporting failure.
		if file, err := os.OpenFile(fifo, os.O_WRONLY|unix.O_NONBLOCK, 0600); err == nil {
			_ = file.Close()
		}
		select {
		case <-done:
		case <-time.After(time.Second):
		}
		t.Fatal("copying FIFO waited for a writer and ignored cancellation")
	}
}
