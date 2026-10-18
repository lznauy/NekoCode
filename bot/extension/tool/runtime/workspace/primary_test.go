package workspace

import (
	"context"
	"path/filepath"
	"testing"
)

func TestPrimaryIsIndependentOfSortedAndSessionRoots(t *testing.T) {
	base := t.TempDir()
	primary, extra := filepath.Join(base, "z-project"), filepath.Join(base, "a-extra")
	m := New(primary, []Root{{Path: extra, Access: AccessReadWrite}})
	if _, err := m.AddSessionRoot(base, AccessReadWrite); err != nil {
		t.Fatal(err)
	}
	ctx := WithManager(context.Background(), m)
	got, err := WorkingDirectory(ctx)
	if err != nil || got != primary {
		t.Fatalf("primary=%q err=%v", got, err)
	}
}
