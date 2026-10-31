package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceFilesystemRootAccess(t *testing.T) {
	child := t.TempDir()
	root := filepath.VolumeName(child) + string(filepath.Separator)
	manager := New(root, nil)
	for name, check := range map[string]func(string) (string, Root, bool, error){
		"read": manager.CheckRead, "write": manager.CheckWrite, "lexical read": manager.CheckReadLexical,
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, ok, err := check(child); err != nil || !ok {
				t.Fatalf("filesystem root must cover child: ok=%v err=%v", ok, err)
			}
		})
	}
	if insideRoot(child+"-sibling", child) {
		t.Fatal("root must not cover a sibling with the same prefix")
	}
}

func TestWorkspaceReadWriteAccess(t *testing.T) {
	primary := t.TempDir()
	docs := t.TempDir()
	manager := New(primary, []Root{{Path: docs, Access: AccessReadOnly}})

	if _, _, ok, err := manager.CheckRead(filepath.Join(primary, "a.go")); err != nil || !ok {
		t.Fatalf("primary should be readable: ok=%v err=%v", ok, err)
	}
	if _, _, ok, err := manager.CheckWrite(filepath.Join(primary, "a.go")); err != nil || !ok {
		t.Fatalf("primary should be writable: ok=%v err=%v", ok, err)
	}
	if _, _, ok, err := manager.CheckRead(filepath.Join(docs, "note.md")); err != nil || !ok {
		t.Fatalf("read-only root should be readable: ok=%v err=%v", ok, err)
	}
	if _, _, ok, err := manager.CheckWrite(filepath.Join(docs, "note.md")); err != nil || ok {
		t.Fatalf("read-only root should not be writable: ok=%v err=%v", ok, err)
	}

	outside := filepath.Join(t.TempDir(), "evil.txt")
	if _, _, ok, err := manager.CheckRead(outside); err != nil || ok {
		t.Fatalf("outside root should not be readable: ok=%v err=%v", ok, err)
	}
	if _, _, ok, err := manager.CheckWrite(outside); err != nil || ok {
		t.Fatalf("outside root should not be writable: ok=%v err=%v", ok, err)
	}
}

func TestWorkspaceAddSessionRootUpgradesAccess(t *testing.T) {
	primary := t.TempDir()
	extra := t.TempDir()
	manager := New(primary, []Root{{Path: extra, Access: AccessReadOnly}})

	if _, err := manager.AddSessionRoot(extra, AccessReadWrite); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err := manager.CheckWrite(filepath.Join(extra, "out.txt")); err != nil || !ok {
		t.Fatalf("upgraded root should be writable: ok=%v err=%v", ok, err)
	}
}

func TestWorkspaceExtraReadWriteRoot(t *testing.T) {
	primary := t.TempDir()
	extra := t.TempDir()
	manager := New(primary, []Root{{Path: extra, Access: AccessReadWrite}})

	if _, _, ok, err := manager.CheckWrite(filepath.Join(extra, "cache.txt")); err != nil || !ok {
		t.Fatalf("extra read-write root should be writable: ok=%v err=%v", ok, err)
	}
}

func TestConfigurePreservesSessionRoots(t *testing.T) {
	primary := t.TempDir()
	sessionRoot := t.TempDir()

	manager := New(primary, nil)
	if _, err := manager.AddSessionRoot(sessionRoot, AccessReadOnly); err != nil {
		t.Fatal(err)
	}

	// Simulate a hot reload (ApplyConfig -> reinit -> Configure): the
	// session root added during the session must survive.
	manager.Configure(primary, nil)

	if _, _, ok, err := manager.CheckRead(filepath.Join(sessionRoot, "note.md")); err != nil || !ok {
		t.Fatalf("session root should survive Configure: ok=%v err=%v", ok, err)
	}
}

func TestSessionRootsAreIsolated(t *testing.T) {
	manager := New(t.TempDir(), nil)
	extra := t.TempDir()
	manager.SetSession("one")
	if _, err := manager.AddSessionRoot(extra, AccessReadOnly); err != nil {
		t.Fatal(err)
	}
	manager.SetSession("two")
	if _, _, ok, err := manager.CheckRead(filepath.Join(extra, "note.md")); err != nil || ok {
		t.Fatalf("session root leaked: ok=%v err=%v", ok, err)
	}
	manager.SetSession("one")
	if _, _, ok, err := manager.CheckRead(filepath.Join(extra, "note.md")); err != nil || !ok {
		t.Fatalf("session root was not restored: ok=%v err=%v", ok, err)
	}
	manager.DropSession("one")
	manager.SetSession("one")
	if _, _, ok, err := manager.CheckRead(filepath.Join(extra, "note.md")); err != nil || ok {
		t.Fatalf("dropped session root remained authorized: ok=%v err=%v", ok, err)
	}
}

func TestManagersDoNotShareAuthority(t *testing.T) {
	primary := t.TempDir()
	extra := t.TempDir()
	one := New(primary, nil)
	two := New(primary, nil)
	if _, err := one.AddSessionRoot(extra, AccessReadWrite); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err := two.CheckWrite(filepath.Join(extra, "private.txt")); err != nil || ok {
		t.Fatalf("workspace authority leaked between managers: ok=%v err=%v", ok, err)
	}
}

func TestResolveExpandsHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home directory available")
	}
	if real, err := filepath.EvalSymlinks(home); err == nil {
		home = real
	}
	resolved, err := Resolve("~/nekocode-test-file")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "nekocode-test-file")
	if resolved != want {
		t.Fatalf("Resolve(~/) = %q, want %q", resolved, want)
	}
}
