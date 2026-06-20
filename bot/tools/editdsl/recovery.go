package editdsl

import (
	"fmt"
	"strings"

	diff "github.com/sergi/go-diff/diffmatchpatch"
)

// RecoveryRequest holds the parameters for 3-way merge recovery.
type RecoveryRequest struct {
	Path        string
	CurrentText string // current file content on disk
	ExpectedTag string // tag the LLM's patch was based on
	Edits       []Hunk // the LLM's intended edits
	Snapshots   *SnapshotStore
	Resolver    BlockResolver // for resolving block hunks during replay
}

// TryRecover attempts to recover from a stale tag using 3-way merge.
// Returns nil if recovery is not possible.
func TryRecover(req RecoveryRequest) (*ApplyResult, error) {
	snap := req.Snapshots.ByHash(req.Path, req.ExpectedTag)
	if snap == nil {
		return nil, fmt.Errorf("no snapshot found for tag %s", req.ExpectedTag)
	}

	// Strategy 1: Snapshot replay + 3-way merge.
	result, err := snapshotReplay(snap, req.CurrentText, req.Edits, req.Resolver, req.Path)
	if err == nil {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("recovered from stale tag %s via 3-way merge", req.ExpectedTag))
		return result, nil
	}

	// Strategy 2: Direct session-chain replay (if line count matches).
	result, err2 := sessionChainReplay(snap, req.CurrentText, req.Edits, req.Resolver, req.Path)
	if err2 == nil {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("recovered from stale tag %s via session-chain replay", req.ExpectedTag))
		return result, nil
	}

	return nil, fmt.Errorf("recovery failed: 3-way merge: %v; session replay: %v", err, err2)
}

// snapshotReplay applies edits to the cached snapshot, then 3-way merges
// the result onto the current file content.
func snapshotReplay(snap *Snapshot, currentText string, edits []Hunk, resolver BlockResolver, path string) (*ApplyResult, error) {
	// Apply edits to the snapshot.
	edited, err := ApplyEdits(snap.Text, edits, resolver, path)
	if err != nil {
		return nil, fmt.Errorf("failed to apply edits to snapshot: %w", err)
	}
	if edited.Text == snap.Text {
		return nil, fmt.Errorf("edits produce no change on snapshot")
	}

	// Compute diff from snapshot to edited version.
	dmp := diff.New()
	diffs := dmp.DiffMain(snap.Text, edited.Text, false)
	patches := dmp.PatchMake(snap.Text, diffs)

	// Apply patch to current content.
	results, applied := dmp.PatchApply(patches, currentText)
	if !allApplied(applied) {
		return nil, fmt.Errorf("patch application incomplete: %d/%d hunks applied", countTrue(applied), len(applied))
	}

	if results == currentText {
		// Already in desired state — return success.
		return &ApplyResult{
			Text:             results,
			FirstChangedLine: findFirstChangedLine(currentText, results),
			Warnings:         []string{"file already in desired state after merge"},
		}, nil
	}

	// Find first changed line.
	firstChanged := findFirstChangedLine(currentText, results)

	return &ApplyResult{
		Text:             results,
		FirstChangedLine: firstChanged,
	}, nil
}

// sessionChainReplay directly applies edits to the current text when
// line counts match and anchor content is consistent.
func sessionChainReplay(snap *Snapshot, currentText string, edits []Hunk, resolver BlockResolver, path string) (*ApplyResult, error) {
	snapLines := strings.Split(NormalizeToLF(snap.Text), "\n")
	currLines := strings.Split(NormalizeToLF(currentText), "\n")

	// Guard 1: line counts must match.
	if len(snapLines) != len(currLines) {
		return nil, fmt.Errorf("line count mismatch: snapshot=%d current=%d", len(snapLines), len(currLines))
	}

	// Guard 2: anchor content must match.
	for _, h := range edits {
		if h.Kind == HunkInsert && (h.Cursor == CursorHead || h.Cursor == CursorTail) {
			continue
		}
		if !verifyAnchor(snapLines, currLines, h) {
			return nil, fmt.Errorf("anchor content mismatch at line %d", h.Start)
		}
	}

	// Apply edits directly to current content.
	return ApplyEdits(currentText, edits, resolver, path)
}

// verifyAnchor checks that the anchor lines in snapshot and current have
// the same content.
func verifyAnchor(snapLines, currLines []string, h Hunk) bool {
	if h.Start < 1 || h.Start > len(snapLines) {
		return false
	}
	if h.Kind == HunkInsert {
		// For insert, only check the anchor line.
		return strings.TrimSpace(snapLines[h.Start-1]) == strings.TrimSpace(currLines[h.Start-1])
	}
	// For replace/delete, check start and end.
	if h.End > len(snapLines) {
		return false
	}
	startOK := strings.TrimSpace(snapLines[h.Start-1]) == strings.TrimSpace(currLines[h.Start-1])
	endOK := strings.TrimSpace(snapLines[h.End-1]) == strings.TrimSpace(currLines[h.End-1])
	return startOK && endOK
}

// findFirstChangedLine compares two texts line by line and returns the
// 1-based line number of the first difference.
func findFirstChangedLine(old, new_ string) int {
	oldLines := strings.Split(old, "\n")
	newLines := strings.Split(new_, "\n")
	maxLen := len(oldLines)
	if len(newLines) > maxLen {
		maxLen = len(newLines)
	}
	for i := 0; i < maxLen; i++ {
		var o, n string
		if i < len(oldLines) {
			o = oldLines[i]
		}
		if i < len(newLines) {
			n = newLines[i]
		}
		if o != n {
			return i + 1
		}
	}
	return maxLen
}

func allApplied(applied []bool) bool {
	for _, a := range applied {
		if !a {
			return false
		}
	}
	return true
}

func countTrue(applied []bool) int {
	n := 0
	for _, a := range applied {
		if a {
			n++
		}
	}
	return n
}
