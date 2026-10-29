//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPluginFileReadsRejectFIFOWithoutBlocking(t *testing.T) {
	for _, test := range []struct {
		name string
		path string
		read func(string) error
	}{
		{
			name: "hooks",
			path: "hooks.json",
			read: func(root string) error {
				_, err := Load(root, "hooks.json")
				return err
			},
		},
		{
			name: "readFile",
			path: "data.txt",
			read: func(root string) error {
				_, err := runPluginJS(root, Event{Context: t.Context()}, hookAction{
					Type: "js",
					Code: `readFile("data.txt")`,
				})
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, test.path)
			if err := syscall.Mkfifo(path, 0o600); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- test.read(root) }()
			select {
			case err := <-done:
				if err == nil || !strings.Contains(err.Error(), "regular file") {
					t.Fatalf("FIFO read error = %v", err)
				}
			case <-time.After(time.Second):
				releasePluginFIFO(path)
				t.Fatal("FIFO blocked plugin file read")
			}
		})
	}
}

func releasePluginFIFO(path string) {
	file, err := os.OpenFile(path, os.O_RDWR|syscall.O_NONBLOCK, 0)
	if err == nil {
		_ = file.Close()
	}
}
