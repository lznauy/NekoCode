package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDefaultExportPathOverride(t *testing.T) {
	var original string = DefaultExportPath
	t.Cleanup(func() { DefaultExportPath = original })
	DefaultExportPath = filepath.Join(t.TempDir(), "custom-export.json")
	if got, err := ExportMessages(nil, ""); err != nil || got != DefaultExportPath {
		t.Fatalf("message export: got %q, err %v", got, err)
	}
	if got, _, err := New(t.TempDir()).ExportHistory(0, nil, ""); err != nil || got != DefaultExportPath {
		t.Fatalf("history export: got %q, err %v", got, err)
	}
}

func TestDefaultExportPathWithoutHome(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "plan9" {
		t.Skip("tests Unix HOME resolution")
	}
	if os.Getenv("NEKOCODE_TEST_EXPORT_HOME") == "child" {
		if DefaultExportPath != "" {
			t.Fatalf("invalid HOME resolved to %q", DefaultExportPath)
		}
		if _, err := ExportMessages(nil, ""); err == nil {
			t.Fatal("export accepted unresolved default destination")
		}
		if _, _, err := new(Manager).ExportHistory(0, nil, ""); err == nil {
			t.Fatal("history export accepted unresolved default destination")
		}
		t.Setenv("HOME", t.TempDir())
		want := filepath.Join(os.Getenv("HOME"), ".nekocode", "exports", "nekocode-context.json")
		if got, err := ExportMessages(nil, ""); err != nil || got != want {
			t.Fatalf("export after HOME restored: got %q, err %v", got, err)
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestDefaultExportPathWithoutHome$")
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(), "HOME=relative", "NEKOCODE_TEST_EXPORT_HOME=child")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if entries, err := os.ReadDir(cmd.Dir); err != nil || len(entries) != 0 {
		t.Fatalf("working directory changed: %v, %v", entries, err)
	}
}
