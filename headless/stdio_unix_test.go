//go:build unix

package headless

import (
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestOnceSchemaRejectsFIFO(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema.json")
	if err := unix.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := prepareOnce(t.Context(), Options{OutputSchema: path}); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO accepted as schema")
		}
	case <-time.After(time.Second):
		t.Fatal("schema open blocked on FIFO")
	}
}
