package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/util/attachment"
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
	got := r.adoptImageAttachments(input)
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
	_ = src.Close()

	input.Images = []ImageAttachment{{Label: "[Image #7]", Path: srcPath}}
	got = r.adoptImageAttachments(input)
	stored := got.Images[0].Path
	if !strings.HasPrefix(stored, dir+string(os.PathSeparator)) {
		t.Fatalf("adopted path = %q, want under %s", stored, dir)
	}
	if _, err := os.Stat(stored); err != nil {
		t.Fatalf("adopted file unreadable: %v", err)
	}
	if _, err := os.Stat(srcPath); !os.IsNotExist(err) {
		t.Fatalf("temp source should be removed after adoption, stat err = %v", err)
	}

	// Files already inside session storage pass through unchanged.
	existing := filepath.Join(dir, "existing.png")
	got = r.adoptImageAttachments(Input{
		Text:   "[Image #7]",
		Images: []ImageAttachment{{Label: "[Image #7]", Path: existing}},
	})
	if got.Images[0].Path != existing {
		t.Fatalf("in-storage file should pass through, got %q", got.Images[0].Path)
	}

	// Without a session nothing is adopted (vision-native delivery still
	// reads the original path).
	r.services.Sessions.CurrentSessionID = func() string { return "" }
	got = r.adoptImageAttachments(Input{
		Text:   "[Image #7]",
		Images: []ImageAttachment{{Label: "[Image #7]", Path: "/tmp/whatever.png"}},
	})
	if got.Images[0].Path != "/tmp/whatever.png" {
		t.Fatalf("no-session adoption should be a no-op, got %q", got.Images[0].Path)
	}
}
