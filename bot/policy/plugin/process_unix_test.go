//go:build unix

package plugin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCancelStopsDescendantCommands(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, _, err := runPluginCommandEvent(root, Event{Context: ctx}, hookAction{Command: `sh -c 'printf ready > ready; sleep 0.2; printf leaked > leaked' & wait`})
		done <- err
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(root, "ready")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child did not start")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled command reported success")
		}
	case <-time.After(time.Second):
		t.Fatal("command did not stop")
	}
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(root, "leaked")); !os.IsNotExist(err) {
		t.Fatal("descendant survived cancellation")
	}
}
