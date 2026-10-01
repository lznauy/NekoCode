package attachment

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var tinyPNG = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0, 'I', 'H', 'D', 'R'}

func TestSaveImageUsesPrivateSessionDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := SaveImage("session_1", tinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	wantDir := filepath.Join(home, ".nekocode", "tmp", "images", "session_1")
	if filepath.Dir(path) != wantDir || !strings.HasSuffix(path, ".png") {
		t.Fatalf("saved path = %q, want PNG under %q", path, wantDir)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("image permissions = %o, want 600", got)
	}

	if err := DeleteSession("session_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wantDir); !os.IsNotExist(err) {
		t.Fatalf("session image directory still exists: %v", err)
	}
}

func TestSaveImageRejectsInvalidInput(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := SaveImage("../escape", tinyPNG); err == nil {
		t.Fatal("invalid session id was accepted")
	}
	if _, err := SaveImage("session", []byte("plain text")); err == nil {
		t.Fatal("non-image payload was accepted")
	}
	if _, err := SaveImage("session", make([]byte, MaxImageBytes+1)); err == nil {
		t.Fatal("oversized image was accepted")
	}
}

func TestDeleteImageRejectsPathOutsideStorage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	outside := filepath.Join(home, "keep.png")
	if err := os.WriteFile(outside, tinyPNG, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := DeleteImage(outside); err == nil {
		t.Fatal("outside path was accepted")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside file was changed: %v", err)
	}
}

func TestAttachmentPathsRejectSymlinkEscape(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".nekocode", "tmp", "images")
	outside := t.TempDir()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "session")); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveImage("session", tinyPNG); err == nil {
		t.Fatal("save followed a session directory symlink outside storage")
	}
	secret := filepath.Join(outside, "secret.png")
	if err := os.WriteFile(secret, tinyPNG, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := DeleteImage(filepath.Join(root, "session", "secret.png")); err == nil {
		t.Fatal("delete followed an intermediate symlink outside storage")
	}
	if _, err := os.Stat(secret); err != nil {
		t.Fatalf("outside file was changed: %v", err)
	}
}

func TestSessionDeleteCanRollbackAfterLaterFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := SaveImage("session_1", tinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	deletion, err := BeginSessionDelete("session_1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("attachment remained visible while deletion staged: %v", err)
	}
	if err := deletion.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("attachment was not restored: %v", err)
	}
}

func TestSessionDeleteCommitRemovesStagedAttachments(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := SaveImage("session_1", tinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	deletion, err := BeginSessionDelete("session_1")
	if err != nil {
		t.Fatal(err)
	}
	if err := deletion.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("committed attachment still exists: %v", err)
	}
}

func TestReconcileSessionImagesRemovesCommittedStaging(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := SaveImage("session_1", tinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	deletion, err := BeginSessionDelete("session_1")
	if err != nil {
		t.Fatal(err)
	}
	staged := deletion.staged
	if err := ReconcileSessionImages(func(string) (bool, error) { return false, nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatalf("staged attachment directory still exists: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("original attachment unexpectedly reappeared: %v", err)
	}
}

func TestReconcileSessionImagesRemovesOrphanedSessionImages(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	orphan, err := SaveImage("orphan_session", tinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	live, err := SaveImage("live_session", tinyPNG)
	if err != nil {
		t.Fatal(err)
	}

	if err := ReconcileSessionImages(func(id string) (bool, error) {
		return id == "live_session", nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphaned attachment survived reconciliation: %v", err)
	}
	if _, err := os.Stat(live); err != nil {
		t.Fatalf("live session attachment was removed: %v", err)
	}
}

func TestReconcileSessionImagesPreservesOrdinaryDirectoryWhenSessionStateIsUnknown(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := SaveImage("session_1", tinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("session storage unavailable")
	err = ReconcileSessionImages(func(string) (bool, error) { return false, wantErr })
	if !errors.Is(err, wantErr) {
		t.Fatalf("reconcile error = %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("unknown session state removed ordinary attachments: %v", err)
	}
}

func TestReconcileSessionImagesIgnoresUnexpectedDirectories(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".nekocode", "tmp", "images", "not.a.session")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "keep.png")
	if err := os.WriteFile(path, tinyPNG, 0o600); err != nil {
		t.Fatal(err)
	}
	checked := false
	if err := ReconcileSessionImages(func(string) (bool, error) {
		checked = true
		return false, nil
	}); err != nil {
		t.Fatal(err)
	}
	if checked {
		t.Fatal("unexpected directory was treated as a session")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("unexpected directory was removed: %v", err)
	}
}

func TestReconcileSessionImagesRollsBackPreparedStaging(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := SaveImage("session_1", tinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	deletion, err := BeginSessionDelete("session_1")
	if err != nil {
		t.Fatal(err)
	}
	staged := deletion.staged
	if err := ReconcileSessionImages(func(id string) (bool, error) { return id == "session_1", nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatalf("prepared staging directory still exists: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("prepared attachment was not restored: %v", err)
	}
}

func TestReconcileSessionImagesPreservesStagingWhenSessionStateIsUnknown(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := SaveImage("session_1", tinyPNG); err != nil {
		t.Fatal(err)
	}
	deletion, err := BeginSessionDelete("session_1")
	if err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("session storage unavailable")
	err = ReconcileSessionImages(func(string) (bool, error) { return false, wantErr })
	if !errors.Is(err, wantErr) {
		t.Fatalf("reconcile error = %v", err)
	}
	if _, err := os.Stat(deletion.staged); err != nil {
		t.Fatalf("unknown session state removed staged attachments: %v", err)
	}
}
