//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package media

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestReadImageFileRejectsFIFOWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image.png")
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := readImageFile(path)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "regular file") {
			t.Fatalf("readImageFile error = %v", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("readImageFile blocked while opening a FIFO")
	}
}

func TestOpenImageFileRejectsIntermediateSymlink(t *testing.T) {
	realDir := t.TempDir()
	path := filepath.Join(realDir, "image.png")
	if err := os.WriteFile(path, testPNG, 0o600); err != nil {
		t.Fatal(err)
	}
	linkRoot := t.TempDir()
	link := filepath.Join(linkRoot, "linked")
	if err := os.Symlink(realDir, link); err != nil {
		t.Fatal(err)
	}
	file, err := openImageFile(filepath.Join(link, "image.png"))
	if file != nil {
		_ = file.Close()
	}
	if err == nil {
		t.Fatal("secure image open followed an intermediate symlink")
	}
}
