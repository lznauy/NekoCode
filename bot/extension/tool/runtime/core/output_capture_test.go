package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestOutputCaptureSpillsAndPreservesOriginal(t *testing.T) {
	store := NewOutputStore(filepath.Join(t.TempDir(), "session", "tool-results"))
	original := strings.Repeat("匹配内容🙂\n", 4000)
	capture := NewOutputCapture(store, MaxCapturedOutputBytes, nil)
	for start := 0; start < len(original); {
		end := min(start+113, len(original))
		if _, err := capture.Write([]byte(original[start:end])); err != nil {
			t.Fatal(err)
		}
		start = end
	}
	output, err := capture.Finish()
	if err != nil || len(output.Render()) > MaxOutputBytes || !utf8.ValidString(output.Render()) {
		t.Fatalf("invalid preview: length=%d err=%v", len(output.Render()), err)
	}
	path := output.Artifact.Path
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != original {
		t.Fatalf("original changed: %v", err)
	}
	// A recreated store can still address the same session's files.
	reopened := NewOutputStore(filepath.Dir(path))
	dir, err := reopened.Directory()
	if err != nil || dir != filepath.Dir(path) {
		t.Fatal("session store changed across restart")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("file is not private")
	}
	info, err = os.Stat(dir)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("directory is not private")
	}
}

func TestOutputCaptureBoundsMemoryAndDisk(t *testing.T) {
	const limit = 64 * 1024
	stops := 0
	capture := NewOutputCapture(NewOutputStore(t.TempDir()), limit, func() { stops++ })
	chunk := []byte(strings.Repeat("x", 2*limit))
	n, err := capture.Write(chunk)
	if n != limit || !errors.Is(err, ErrOutputLimit) || !capture.Limited() || stops != 1 {
		t.Fatalf("limit not enforced: n=%d err=%v stops=%d", n, err, stops)
	}
	if _, err := capture.Write(chunk); !errors.Is(err, ErrOutputLimit) || stops != 1 {
		t.Fatal("producer was not stopped once")
	}
	if len(capture.small) > MaxOutputBytes || len(capture.head) > MaxOutputBytes/2 || len(capture.tail) > MaxOutputBytes/2 {
		t.Fatal("unbounded memory buffer")
	}
	output, err := capture.Finish()
	if err != nil || len(output.Render()) > MaxOutputBytes || !strings.Contains(output.Render(), "incomplete") || !strings.Contains(output.Render(), "Saved prefix:") {
		t.Fatalf("missing incomplete status: %v", err)
	}
	info, err := os.Stat(output.Artifact.Path)
	if err != nil || info.Size() != limit {
		t.Fatalf("disk limit not enforced: %v", err)
	}
	again, err := capture.Finish()
	if err != nil || again != output {
		t.Fatal("Finish is not idempotent")
	}
}

func TestBoundOutputKeepsArtifactAndCompleteness(t *testing.T) {
	store := NewOutputStore(t.TempDir())
	capture := NewOutputCapture(store, 32768, nil)
	_, _ = capture.Write([]byte(strings.Repeat("evidence", 10000)))
	output, err := capture.Finish()
	if err != nil || !output.Artifact.Incomplete {
		t.Fatalf("capture status lost: %+v %v", output.Artifact, err)
	}
	narrowed, err := store.BoundOutput(output, 1024)
	if err != nil || narrowed.Artifact != output.Artifact || len(narrowed.Render()) > 1024 {
		t.Fatalf("rebudget changed the artifact: %+v %v", narrowed.Artifact, err)
	}

}

func TestSmallCaptureDoesNotCreateArtifact(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	capture := NewOutputCapture(NewOutputStore(dir), MaxCapturedOutputBytes, nil)
	capture.Write([]byte("one match\n"))
	output, err := capture.Finish()
	if err != nil || output.Render() != "one match\n" {
		t.Fatal(output.Render(), err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("small output created a store")
	}
}

func TestChildOutputStoresUseDistinctNamespaces(t *testing.T) {
	parent := NewOutputStore(t.TempDir())
	first, _ := parent.Child("agent-one").Directory()
	second, _ := parent.Child("agent-two").Directory()
	if first == second || filepath.Base(first) != "agent-one" || filepath.Base(second) != "agent-two" {
		t.Fatal("actor namespaces overlap")
	}
	if _, err := parent.Child("../escape").Directory(); err == nil {
		t.Fatal("accepted path traversal")
	}
}
