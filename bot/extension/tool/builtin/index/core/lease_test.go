package core

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestIndexLeaseProcessHelper(t *testing.T) {
	root := os.Getenv("NEKOCODE_INDEX_LEASE_TEST")
	if root == "" {
		return
	}
	lease, err := acquireIndexLease(root, false)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("ready")
	_, _ = io.Copy(io.Discard, os.Stdin)
	if err := lease.close(); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}

func TestIndexLeaseCoordinatesProcessesAndReleasesOnExit(t *testing.T) {
	for _, crash := range []bool{false, true} {
		t.Run(fmt.Sprintf("crash=%v", crash), func(t *testing.T) {
			root := t.TempDir()
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestIndexLeaseProcessHelper$")
			command.Env = append(os.Environ(), "NEKOCODE_INDEX_LEASE_TEST="+root)
			input, err := command.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			output, err := command.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer output.Close()
			command.Stderr = os.Stderr
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if command.ProcessState == nil {
					_ = command.Process.Kill()
					_ = command.Wait()
				}
			}()
			scanner := bufio.NewScanner(output)
			if !scanner.Scan() || scanner.Text() != "ready" {
				t.Fatalf("child not ready: %q %v", scanner.Text(), scanner.Err())
			}
			shared, err := acquireIndexLease(root, false)
			if err != nil {
				t.Fatal(err)
			}
			if err := shared.close(); err != nil {
				t.Fatal(err)
			}
			if lease, err := acquireIndexLease(root, true); err == nil {
				_ = lease.close()
				t.Fatal("exclusive rebuild ignored another process")
			}
			if crash {
				_ = command.Process.Kill()
			} else {
				_ = input.Close()
			}
			if err := command.Wait(); err != nil && !crash {
				t.Fatal(err)
			}
			exclusive, err := acquireIndexLease(root, true)
			if err != nil {
				t.Fatalf("lock not released on child exit: %v", err)
			}
			defer exclusive.close()
			if err := exclusive.share(); err != nil {
				t.Fatal(err)
			}
			another, err := acquireIndexLease(root, false)
			if err != nil {
				t.Fatalf("rebuilt manager prevented subsequent readers: %v", err)
			}
			_ = another.close()
		})
	}
}

func TestIndexRebuildLeaseCoversBackupAndInitialization(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example\n"), 0600); err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Init(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := BackupDatabase(root); err == nil {
		t.Fatal("backup moved a live manager's database")
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("close is not idempotent: %v", err)
	}
	manager, _, err = RebuildManager(root)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	another, err := NewManager(root)
	if err != nil {
		t.Fatalf("rebuild did not downgrade ownership: %v", err)
	}
	defer another.Close()
}

func TestBackupDatabaseRestoresMovedFilesOnFailure(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".nekocode")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"index.db":     "main",
		"index.db-wal": "wal",
		"index.db-shm": "shm",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}

	originalRename := renameIndexDBFile
	defer func() { renameIndexDBFile = originalRename }()
	injected := errors.New("injected rename failure")
	calls := 0
	renameIndexDBFile = func(oldPath, newPath string) error {
		calls++
		if calls == 2 {
			return injected
		}
		return os.Rename(oldPath, newPath)
	}
	if _, err := backupDatabase(root); !errors.Is(err, injected) {
		t.Fatalf("backup error = %v", err)
	}
	if calls != 3 {
		t.Fatalf("rename calls = %d, want move, failure, restore", calls)
	}
	for name, want := range files {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("original %s was not restored: %v", name, err)
		}
		if string(data) != want {
			t.Fatalf("restored %s = %q, want %q", name, data, want)
		}
	}
}

func TestInitialBackupRollbackFailureRetainsLeaseUntilClose(t *testing.T) {
	tests := []struct {
		name string
		run  func(string) (*Manager, string, error)
	}{
		{name: "BackupDatabase", run: BackupDatabase},
		{name: "RebuildManager", run: RebuildManager},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, ".nekocode")
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example\n"), 0600); err != nil {
				t.Fatal(err)
			}
			for name, body := range map[string]string{
				"index.db": "main", "index.db-wal": "wal", "index.db-shm": "shm",
			} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}

			originalRename := renameIndexDBFile
			defer func() { renameIndexDBFile = originalRename }()
			moveFailure := errors.New("injected move failure")
			rollbackFailure := errors.New("injected rollback failure")
			calls := 0
			renameIndexDBFile = func(oldPath, newPath string) error {
				calls++
				switch calls {
				case 2:
					return moveFailure
				case 3:
					return rollbackFailure
				default:
					return os.Rename(oldPath, newPath)
				}
			}

			manager, backup, err := test.run(root)
			if manager == nil || backup == "" || !errors.Is(err, moveFailure) || !errors.Is(err, rollbackFailure) {
				t.Fatalf("operation = %v, %q, %v", manager, backup, err)
			}
			if calls != 3 {
				t.Fatalf("rename calls = %d, want move, failure, failed rollback", calls)
			}
			if _, err := os.Lstat(filepath.Join(dir, "index.db")); !os.IsNotExist(err) {
				t.Fatalf("main database unexpectedly restored: %v", err)
			}
			if data, err := os.ReadFile(filepath.Join(backup, "index.db")); err != nil || string(data) != "main" {
				t.Fatalf("preserved main database = %q, %v", data, err)
			}
			if lease, leaseErr := acquireIndexLease(root, true); leaseErr == nil {
				_ = lease.close()
				t.Fatal("incomplete initial backup rollback released the index lease")
			}
			if err := manager.Close(); err != nil {
				t.Fatalf("close lease-owning manager: %v", err)
			}
			if err := manager.Close(); err != nil {
				t.Fatalf("second close: %v", err)
			}
			lease, err := acquireIndexLease(root, true)
			if err != nil {
				t.Fatalf("lease not reacquirable after Close: %v", err)
			}
			if err := lease.close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestIndexSharedLeasesOpenConcurrently(t *testing.T) {
	root := t.TempDir()
	start := make(chan struct{})
	var ready sync.WaitGroup
	leases := make(chan *indexLease, 32)
	failures := make(chan error, 32)
	for range 32 {
		ready.Add(1)
		go func() {
			defer ready.Done()
			<-start
			lease, err := acquireIndexLease(root, false)
			if err != nil {
				failures <- err
			} else {
				leases <- lease
			}
		}()
	}
	close(start)
	ready.Wait()
	close(failures)
	close(leases)
	for lease := range leases {
		defer lease.close()
	}
	for err := range failures {
		t.Errorf("concurrent shared open failed without a rebuild: %v", err)
	}
}

func TestStatusReportReflectsWatcherState(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example\n"), 0600); err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := manager.Close(); err != nil {
			t.Error(err)
		}
	})
	report, err := manager.StatusReport()
	if err != nil || !strings.Contains(report, "Watcher: running") {
		t.Fatalf("running report = %q, %v", report, err)
	}
	if err := manager.syncer.Stop(); err != nil {
		t.Fatal(err)
	}
	report, err = manager.StatusReport()
	if err != nil || !strings.Contains(report, "Watcher: stopped") {
		t.Fatalf("stopped report = %q, %v", report, err)
	}
}

func TestRebuildManagerInitFailureRestoresBackup(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".nekocode")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example\n"), 0600); err != nil {
		t.Fatal(err)
	}
	oldFiles := map[string]string{"index.db": "old main", "index.db-wal": "old wal", "index.db-shm": "old shm"}
	for name, body := range oldFiles {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	originalInit := initializeManager
	t.Cleanup(func() { initializeManager = originalInit })
	injected := errors.New("injected init failure")
	initializeManager = func(*Manager) error { return injected }

	manager, backup, err := RebuildManager(root)
	if manager != nil || !errors.Is(err, injected) || backup == "" {
		t.Fatalf("RebuildManager() = %v, %q, %v", manager, backup, err)
	}
	for name, want := range oldFiles {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(data) != want {
			t.Fatalf("restored %s = %q, %v; want %q", name, data, err, want)
		}
	}
	quarantines, err := filepath.Glob(filepath.Join(dir, "index-backup-*", "index.db"))
	if err != nil || len(quarantines) != 1 || filepath.Dir(quarantines[0]) == backup {
		t.Fatalf("failed replacement was not quarantined: %v, %v", quarantines, err)
	}
	lease, err := acquireIndexLease(root, true)
	if err != nil {
		t.Fatalf("rebuild lease was not released after recovery: %v", err)
	}
	if err := lease.close(); err != nil {
		t.Fatal(err)
	}
}

func TestRebuildManagerInitFailureQuarantinesNewDatabaseWithoutBackup(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example\n"), 0600); err != nil {
		t.Fatal(err)
	}
	originalInit := initializeManager
	t.Cleanup(func() { initializeManager = originalInit })
	injected := errors.New("injected init failure")
	initializeManager = func(*Manager) error { return injected }

	manager, backup, err := RebuildManager(root)
	if manager != nil || !errors.Is(err, injected) || backup != "" {
		t.Fatalf("RebuildManager() = %v, %q, %v", manager, backup, err)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if _, err := os.Lstat(filepath.Join(root, ".nekocode", "index.db"+suffix)); !os.IsNotExist(err) {
			t.Fatalf("failed replacement index.db%s remains active: %v", suffix, err)
		}
	}
	quarantines, err := filepath.Glob(filepath.Join(root, ".nekocode", "index-backup-*", "index.db"))
	if err != nil || len(quarantines) != 1 {
		t.Fatalf("new database was not quarantined: %v, %v", quarantines, err)
	}
}

func TestRebuildManagerRestoreFailureJoinsErrorsAndPreservesFiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".nekocode")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example\n"), 0600); err != nil {
		t.Fatal(err)
	}
	oldFiles := map[string]string{"index.db": "old main", "index.db-wal": "old wal", "index.db-shm": "old shm"}
	for name, body := range oldFiles {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	originalInit := initializeManager
	originalRename := renameIndexDBFile
	t.Cleanup(func() {
		initializeManager = originalInit
		renameIndexDBFile = originalRename
	})
	initFailure := errors.New("injected init failure")
	restoreFailure := errors.New("injected restore failure")
	initializeManager = func(*Manager) error { return initFailure }
	backup := ""
	renameIndexDBFile = func(oldPath, newPath string) error {
		if backup == "" && filepath.Base(newPath) == "index.db" && strings.HasPrefix(filepath.Base(filepath.Dir(newPath)), "index-backup-") {
			backup = filepath.Dir(newPath)
		}
		if backup != "" && oldPath == filepath.Join(backup, "index.db-wal") && newPath == filepath.Join(dir, "index.db-wal") {
			return restoreFailure
		}
		return os.Rename(oldPath, newPath)
	}

	manager, returnedBackup, err := RebuildManager(root)
	if manager == nil || !errors.Is(err, initFailure) || !errors.Is(err, restoreFailure) {
		t.Fatalf("RebuildManager() = %v, %q, %v", manager, returnedBackup, err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	if lease, leaseErr := acquireIndexLease(root, false); leaseErr == nil {
		_ = lease.close()
		t.Fatal("incomplete database recovery released the index lease")
	}
	for name, want := range oldFiles {
		if _, err := os.Lstat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("partially restored %s remains active: %v", name, err)
		}
		data, err := os.ReadFile(filepath.Join(backup, name))
		if err != nil || string(data) != want {
			t.Fatalf("preserved backup %s = %q, %v; want %q", name, data, err, want)
		}
	}
	quarantines, err := filepath.Glob(filepath.Join(dir, "index-backup-*", "index.db"))
	if err != nil || len(quarantines) != 2 {
		t.Fatalf("backup and quarantine were not both preserved: %v, %v", quarantines, err)
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("close manager after failed recovery: %v", err)
	}
	lease, err := acquireIndexLease(root, true)
	if err != nil {
		t.Fatalf("Close did not release lease after close(false): %v", err)
	}
	if err := lease.close(); err != nil {
		t.Fatal(err)
	}
}
