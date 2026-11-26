//go:build unix || windows

package filelock

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireSerializesProcesses(t *testing.T) {
	const variable = "NEKOCODE_FILELOCK_TEST_PATH"
	if path := os.Getenv(variable); path != "" {
		fmt.Println("waiting")
		unlock, err := Acquire(path)
		if err != nil {
			t.Fatal(err)
		}
		unlock()
		return
	}
	path := filepath.Join(t.TempDir(), "locks", "shared.lock")
	unlock, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestAcquireSerializesProcesses$")
	cmd.Env = append(os.Environ(), variable+"="+path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != "waiting" {
		t.Fatalf("waiter did not start: %s", scanner.Text())
	}
	done := make(chan error, 1)
	go func() {
		for scanner.Scan() {
		}
		done <- cmd.Wait()
	}()
	select {
	case err := <-done:
		t.Fatalf("waiter returned before lock release: %v %s", err, stderr.String())
	case <-time.After(100 * time.Millisecond):
	}
	unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("waiter: %v %s", err, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("lock still held after release")
	}
}
