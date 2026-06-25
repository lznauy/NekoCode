package editcore

import (
	"strings"
	"testing"
)

func TestRecovery_SessionChainReplay(t *testing.T) {
	store := NewSnapshotStore()
	original := "line1\nline2\nline3\nline4\nline5"
	hash := store.Record("/test/file.go", original)

	edits := []Hunk{{
		Kind:    HunkReplace,
		Start:   3,
		End:     3,
		Payload: []string{"new line 3"},
	}}

	result, err := TryRecover(RecoveryRequest{
		Path:        "/test/file.go",
		CurrentText: original,
		ExpectedTag: hash,
		Edits:       edits,
		Snapshots:   store,
	})
	if err != nil {
		t.Fatalf("recovery failed: %v", err)
	}
	if !strings.Contains(result.Text, "new line 3") {
		t.Fatalf("expected 'new line 3' in result, got %q", result.Text)
	}
}

func TestRecovery_3WayMerge(t *testing.T) {
	store := NewSnapshotStore()
	original := "line1\nline2\nline3\nline4\nline5"
	hash := store.Record("/test/file.go", original)

	current := "line1\nline2\nline3\nline4\nline5_modified"

	edits := []Hunk{{
		Kind:    HunkReplace,
		Start:   3,
		End:     3,
		Payload: []string{"new line 3"},
	}}

	result, err := TryRecover(RecoveryRequest{
		Path:        "/test/file.go",
		CurrentText: current,
		ExpectedTag: hash,
		Edits:       edits,
		Snapshots:   store,
	})
	if err != nil {
		t.Fatalf("recovery failed: %v", err)
	}
	if !strings.Contains(result.Text, "new line 3") {
		t.Fatalf("expected 'new line 3' in result, got %q", result.Text)
	}
	if !strings.Contains(result.Text, "line5_modified") {
		t.Fatalf("expected 'line5_modified' in result, got %q", result.Text)
	}
}

func TestRecovery_NoSnapshot(t *testing.T) {
	store := NewSnapshotStore()
	_, err := TryRecover(RecoveryRequest{
		Path:        "/test/file.go",
		CurrentText: "content",
		ExpectedTag: "AAAAAAAA",
		Edits:       []Hunk{{Kind: HunkReplace, Start: 1, End: 1, Payload: []string{"new"}}},
		Snapshots:   store,
	})
	if err == nil {
		t.Fatal("expected error for missing snapshot")
	}
}

// ---------------------------------------------------------------------------
// block hunk tests
// ---------------------------------------------------------------------------
