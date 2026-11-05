//go:build unix

package types

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestLoadImageRejectsSpecialFileWithoutBlocking(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "image.png")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan bool, 1)
	go func() { _, _, ok := LoadImage(MessageImage{Path: fifo}); done <- ok }()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("FIFO accepted as image data")
		}
	case <-time.After(200 * time.Millisecond):
		if file, err := os.OpenFile(fifo, os.O_WRONLY|unix.O_NONBLOCK, 0600); err == nil {
			_ = file.Close()
		}
		t.Fatal("image read blocked on FIFO")
	}
}
