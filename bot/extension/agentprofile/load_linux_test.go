package agentprofile

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestLoadRejectsFIFOWithoutBlocking(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "pipe.md")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := Load(root, path, knownTool); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("accepted FIFO")
		}
	case <-time.After(time.Second):
		// Release a reader on a regressed implementation before failing.
		file, err := os.OpenFile(path, os.O_RDWR|syscall.O_NONBLOCK, 0)
		if err == nil {
			file.Close()
		}
		t.Fatal("FIFO blocked profile loading")
	}
}
