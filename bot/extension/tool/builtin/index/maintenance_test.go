package index

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"nekocode/bot/extension/tool/runtime/workspace"
)

func TestMaintenanceRecoversCorruptDatabase(t *testing.T) {
	root := t.TempDir()
	for path, body := range map[string]string{"go.mod": "module example\n", "main.go": "package example\nfunc Hello() {}\n", ".nekocode/index.db": "broken sqlite"} {
		target := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	ctx := workspace.WithManager(t.Context(), workspace.New(root, nil))
	tool := NewIndexTool()
	t.Cleanup(func() {
		if err := tool.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := tool.Maintain(ctx, "status"); err == nil {
		t.Fatal("corrupt index reported healthy")
	}
	for range 2 {
		report, err := tool.Maintain(ctx, "rebuild")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(report, "Files: 1") || !strings.Contains(report, "go: 1") || !strings.Contains(report, "Previous database backup:") {
			t.Fatal(report)
		}
		result, err := tool.Execute(ctx, map[string]any{"query": "symbol:Hello"})
		if err != nil || !strings.Contains(result.Text, "Hello") {
			t.Fatalf("%+v %v", result, err)
		}
	}
	backups, err := filepath.Glob(filepath.Join(root, ".nekocode", "index-backup-*", "index.db"))
	if err != nil || len(backups) != 2 {
		t.Fatalf("backups %v: %v", backups, err)
	}
	preserved := false
	for _, path := range backups {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) == "broken sqlite" {
			preserved = true
		}
	}
	if !preserved {
		t.Fatal("corrupt original was not preserved")
	}
	var wg sync.WaitGroup
	for n := range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			action := "status"
			if n%2 == 0 {
				action = "rebuild"
			}
			if _, err := tool.Maintain(ctx, action); err != nil {
				t.Error(err)
			}
			if _, err := tool.Execute(ctx, map[string]any{"query": "skeleton"}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	// Verify live watcher synchronization appears in database-backed status.
	if err := os.WriteFile(filepath.Join(root, "extra.go"), []byte("package example\nfunc Extra() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		report, err := tool.Maintain(ctx, "status")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(report, "Files: 2") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("watcher did not update: %s", report)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestMaintenanceUnavailableAndClosed(t *testing.T) {
	ctx := workspace.WithManager(t.Context(), workspace.New(t.TempDir(), nil))
	tool := NewIndexTool()
	report, err := tool.Maintain(ctx, "status")
	if err != nil || !strings.Contains(report, "unavailable") {
		t.Fatalf("%s %v", report, err)
	}
	if _, err := tool.Maintain(ctx, "rebuild"); err == nil {
		t.Fatal("rebuild without root accepted")
	}
	if err := tool.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Maintain(ctx, "status"); err == nil {
		t.Fatal("closed tool accepted maintenance")
	}
}

func TestMaintenanceCannotMoveAnotherInstancesDatabase(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package example\nfunc Hello(){}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx := workspace.WithManager(t.Context(), workspace.New(root, nil))
	first, second := NewIndexTool(), NewIndexTool()
	defer first.Close()
	defer second.Close()
	for _, tool := range []*IndexTool{first, second} {
		if _, err := tool.Maintain(ctx, "status"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := first.Maintain(ctx, "rebuild"); err == nil {
		t.Fatal("rebuild moved a database still open in another instance")
	}
	backups, err := filepath.Glob(filepath.Join(root, ".nekocode", "index-backup-*"))
	if err != nil || len(backups) != 0 {
		t.Fatalf("busy rebuild created backups: %v %v", backups, err)
	}
	if _, err := first.Maintain(ctx, "status"); err != nil {
		t.Fatalf("busy rebuild disabled queries: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	report, err := first.Maintain(ctx, "rebuild")
	if err != nil || !strings.Contains(report, "Previous database backup:") {
		t.Fatalf("retry failed: %s %v", report, err)
	}

	backupFiles, err := filepath.Glob(filepath.Join(root, ".nekocode", "index-backup-*", "index.db"))
	if err != nil || len(backupFiles) != 1 {
		t.Fatalf("backups=%v err=%v", backupFiles, err)
	}
	before, err := os.ReadFile(backupFiles[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "later.go"), []byte("package example\nfunc Later(){}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		status, err := first.Maintain(ctx, "status")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(status, "Files: 2") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("replacement watcher did not update: %s", status)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(backupFiles[0])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("backup changed after replacement index update")
	}
}
