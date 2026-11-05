//go:build unix

package deliver

import (
	"golang.org/x/sys/unix"
	"path/filepath"
	"testing"
	"time"
)

func TestDeliveryRejectsFIFOWithoutCallingBackend(t *testing.T) {
	ctx, root := wsContext(t)
	path := filepath.Join(root, "fifo")
	if err := unix.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	backend := &fakeDeliverer{sent: []string{"telegram"}}
	done := make(chan error, 1)
	go func() { _, err := NewTool(backend).Execute(ctx, map[string]any{"path": path}); done <- err }()
	select {
	case err := <-done:
		if err == nil || backend.path != "" {
			t.Fatalf("FIFO reached backend: path=%q err=%v", backend.path, err)
		}
	case <-time.After(time.Second):
		t.Fatal("FIFO blocked delivery")
	}
}
