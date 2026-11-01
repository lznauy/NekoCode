package execution

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestReadEvidenceDeliveryAndIsolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	var reads, other ReadEvidence
	data := []byte("original\r\n")
	id := reads.Record(path, data, "preview")
	if reads.Check(path, data) == nil {
		t.Fatal("pending read authorized mutation")
	}
	reads.Observe(map[string]string{id: "truncated"})
	if reads.Check(path, data) == nil {
		t.Fatal("truncated read authorized mutation")
	}
	reads.Observe(map[string]string{id: "preview"})
	if err := reads.Check(path, data); err != nil {
		t.Fatal(err)
	}
	if reads.Check(path, []byte("original\n")) == nil {
		t.Fatal("raw byte change was ignored")
	}
	other.Observe(map[string]string{id: "preview"})
	if other.Check(path, data) == nil {
		t.Fatal("other actor accepted receipt")
	}
	reads.Observe(nil)
	if reads.Check(path, data) == nil {
		t.Fatal("removed context retained authorization")
	}
	reads.Observe(map[string]string{id: "preview"})
	reads.Reset()
	reads.Observe(map[string]string{id: "preview"})
	if reads.Check(path, data) == nil {
		t.Fatal("reset resurrected receipt")
	}
}

func TestReadEvidenceBoundAndInvalidation(t *testing.T) {
	var reads ReadEvidence
	path := filepath.Join(t.TempDir(), "file")
	first := reads.Record(path, nil, "preview")
	for i := 0; i < maxReadEvidence; i++ {
		reads.Record(path, nil, "preview")
	}
	if len(reads.entries) != maxReadEvidence {
		t.Fatal("unbounded receipts")
	}
	reads.Observe(map[string]string{first: "preview"})
	if reads.Check(path, nil) == nil {
		t.Fatal("evicted receipt authorized mutation")
	}
	reads.Invalidate(path)
	if len(reads.entries) != 0 {
		t.Fatal("invalidation retained receipts")
	}
}

func TestFileLockCancellationAndReclamation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	unlock, err := LockFile(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		release, err := LockFile(ctx, path)
		if release != nil {
			release()
		}
		done <- err
	}()
	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatalf("wait error = %v", err)
	}
	unlock()
	unlock()
	fileLocks.Lock()
	defer fileLocks.Unlock()
	if _, ok := fileLocks.entries[normalizePath(path)]; ok {
		t.Fatal("lock entry leaked")
	}
}

func TestFileLockSerializesAliases(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "target")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	unlock, err := LockFile(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		release, err := LockFile(ctx, alias)
		if release != nil {
			release()
		}
		done <- err
	}()
	// Wait until the alias waiter has registered against the same target.
	deadline := time.After(time.Second)
	for {
		fileLocks.Lock()
		users := fileLocks.entries[normalizePath(path)].users
		fileLocks.Unlock()
		if users == 2 {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("alias bypassed target lock: %v", err)
		case <-deadline:
			t.Fatal("alias waiter did not register")
		default:
			runtime.Gosched()
		}
	}
	releaseOther, err := LockFile(context.Background(), filepath.Join(root, "other"))
	if err != nil {
		t.Fatal(err)
	}
	releaseOther()
	unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
