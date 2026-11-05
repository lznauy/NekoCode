package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/util/attachment"
	utilfs "nekocode/util/fs"
)

// TestAdoptImageAttachments verifies the connector→session-storage adoption:
// temp-dir downloads move into the session image dir (and the source copy is
// removed), files already in session storage pass through, and a missing
// session leaves everything untouched.
func TestAdoptImageAttachments(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sessionID := "20261015T000000-000000001"
	r := &Runtime{}
	r.services.Sessions.CurrentSessionID = func() string { return sessionID }
	dir, err := attachment.ImageDir(sessionID)
	if err != nil {
		t.Fatal(err)
	}

	input := Input{
		Text:   "[Image #7]",
		Images: []ImageAttachment{{Label: "[Image #7]", Path: "/nonexistent/missing.png"}},
	}

	// Unreadable source passes through untouched.
	got := mustAdoptImages(t, r, input)
	if got.Images[0].Path != "/nonexistent/missing.png" {
		t.Fatalf("unreadable source should pass through, got %q", got.Images[0].Path)
	}

	// A temp-dir download is adopted into session storage and the source
	// file is removed.
	src, err := os.CreateTemp("", "nekocode-feishu-*.png")
	if err != nil {
		t.Fatal(err)
	}
	// A minimal PNG header so attachment.SaveImage accepts it.
	if _, err := src.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'}); err != nil {
		t.Fatal(err)
	}
	srcPath := src.Name()
	t.Cleanup(func() { _ = os.Remove(srcPath) })
	_ = src.Close()

	input.Images = []ImageAttachment{{Label: "[Image #7]", Path: srcPath, Release: func() { _ = os.Remove(srcPath) }}}
	got = mustAdoptImages(t, r, input)
	stored := got.Images[0].Path
	if !strings.HasPrefix(stored, dir+string(os.PathSeparator)) {
		t.Fatalf("adopted path = %q, want under %s", stored, dir)
	}
	if got.Images[0].Release != nil {
		t.Fatal("adopted image retained source cleanup ownership")
	}
	if _, err := os.Stat(stored); err != nil {
		t.Fatalf("adopted file unreadable: %v", err)
	}
	if _, err := os.Stat(srcPath); !os.IsNotExist(err) {
		t.Fatalf("temp source should be removed after adoption, stat err = %v", err)
	}

	// Files already inside session storage pass through unchanged.
	existing := filepath.Join(dir, "existing.png")
	got = mustAdoptImages(t, r, Input{
		Text:   "[Image #7]",
		Images: []ImageAttachment{{Label: "[Image #7]", Path: existing}},
	})
	if got.Images[0].Path != existing {
		t.Fatalf("in-storage file should pass through, got %q", got.Images[0].Path)
	}

	// Without a session nothing is adopted (vision-native delivery still
	// reads the original path).
	r.services.Sessions.CurrentSessionID = func() string { return "" }
	got = mustAdoptImages(t, r, Input{
		Text:   "[Image #7]",
		Images: []ImageAttachment{{Label: "[Image #7]", Path: "/tmp/whatever.png"}},
	})
	if got.Images[0].Path != "/tmp/whatever.png" {
		t.Fatalf("no-session adoption should be a no-op, got %q", got.Images[0].Path)
	}
}

func TestAdoptPreservesUserWorkspaceImage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	source := filepath.Join(t.TempDir(), "logo.png")
	if err := os.WriteFile(source, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'}, 0600); err != nil {
		t.Fatal(err)
	}
	r := &Runtime{}
	r.services.Sessions.CurrentSessionID = func() string { return "session_1" }
	got := mustAdoptImages(t, r, Input{Text: "[Image #1]", Images: []ImageAttachment{{Label: "[Image #1]", Path: source}}})
	if got.Images[0].Path == source {
		t.Fatal("source was not adopted")
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("user source removed: %v", err)
	}
}

func TestAdoptRetainsSourceOwnershipOnFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	source := filepath.Join(t.TempDir(), "invalid.png")
	if err := os.WriteFile(source, []byte("not an image"), 0600); err != nil {
		t.Fatal(err)
	}
	released := false
	input := Input{Text: "[Image #1]", Images: []ImageAttachment{{Label: "[Image #1]", Path: source, Release: func() { released = true }}}}
	r := &Runtime{}
	for _, session := range []string{"", "session_1"} {
		r.services.Sessions.CurrentSessionID = func() string { return session }
		got := mustAdoptImages(t, r, input)
		if got.Images[0].Path != source || got.Images[0].Release == nil || released {
			t.Fatal("unsuccessful adoption released ownership")
		}
	}
}

func TestImageCleanupOwnershipDoesNotSurviveJSON(t *testing.T) {
	original := ImageAttachment{Label: "image", Path: "/tmp/source.png", Release: func() { t.Fatal("unexpected release") }}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var replay ImageAttachment
	if err := json.Unmarshal(data, &replay); err != nil {
		t.Fatal(err)
	}
	if replay.Release != nil {
		t.Fatal("replay acquired cleanup ownership")
	}
	if string(data) != `{"label":"image","path":"/tmp/source.png"}` {
		t.Fatalf("unexpected wire image: %s", data)
	}
}

func mustAdoptImages(t *testing.T, r *Runtime, input Input) Input {
	t.Helper()
	adopted, err := r.adoptImageAttachments(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	return adopted
}

func TestAdoptRejectsOversizedSourceBeforeCopying(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	source := filepath.Join(t.TempDir(), "large.png")
	file, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(attachment.MaxImageBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	_ = file.Close()
	r := &Runtime{}
	r.services.Sessions.CurrentSessionID = func() string { return "session_1" }
	released := false
	_, err = r.adoptImageAttachments(t.Context(), Input{Images: []ImageAttachment{{Label: "image", Path: source, Release: func() { released = true }}}})
	if !errors.Is(err, utilfs.ErrFileTooLarge) || released {
		t.Fatalf("oversized source: %v, released=%v", err, released)
	}
}
