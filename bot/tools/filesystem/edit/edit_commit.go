package edit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"nekocode/bot/tools"
	"nekocode/bot/tools/diff"
	"nekocode/bot/tools/editcore"
	"nekocode/bot/tools/toolhelpers"
	"nekocode/common"
	"nekocode/common/debug"
)

type preflightResult struct {
	safePath         string
	normalizedBefore string
	lineEnding       string
}

func snapshotUndoPath(safePath string) string {
	h := sha256.Sum256([]byte(safePath))
	hash := hex.EncodeToString(h[:])[:16]
	return filepath.Join(common.NekocodeDataDir("snapshots"), hash+"_"+filepath.Base(safePath)+".pre-edit")
}

func (t *EditTool) revertSnapshot(path string) (string, error) {
	safePath, err := tools.ValidatePath(path)
	if err != nil {
		return "", fmt.Errorf("revert: invalid path: %w", err)
	}
	undoFile := snapshotUndoPath(safePath)
	preData, err := os.ReadFile(undoFile)
	if err != nil {
		return "", fmt.Errorf("revert: no snapshot for %s: %w", filepath.Base(safePath), err)
	}
	currentData, err := os.ReadFile(safePath)
	if err != nil {
		return "", fmt.Errorf("revert: read current file failed: %w", err)
	}
	mode := toolhelpers.GetFileMode(safePath)
	if err := os.WriteFile(safePath, preData, mode); err != nil {
		return "", fmt.Errorf("revert: write failed: %w", err)
	}
	newTag := tools.RecordSnapshot(safePath, string(preData))
	return renderRevertDiff(safePath, newTag, string(currentData), string(preData)), nil
}

func (t *EditTool) previewRevertSnapshot(path string) string {
	safePath, err := tools.ValidatePath(path)
	if err != nil {
		return ""
	}
	preData, err := os.ReadFile(snapshotUndoPath(safePath))
	if err != nil {
		return ""
	}
	currentData, err := os.ReadFile(safePath)
	if err != nil {
		return ""
	}
	return renderRevertDiff(safePath, "revert", string(currentData), string(preData))
}

func renderRevertDiff(path, tag, currentText, restoredText string) string {
	return diff.RenderTextChange(currentText, restoredText, diff.TextChangeOptions{
		Context:      diff.DefaultContext,
		Header:       diff.TagHeader(path, tag),
		NoChangeText: diff.NoChanges,
	})
}

func writeUndoSnapshot(pe preflightResult) {
	undoFile := snapshotUndoPath(pe.safePath)
	if err := os.MkdirAll(filepath.Dir(undoFile), 0755); err != nil {
		return
	}
	preEditContent := editcore.RestoreLineEndings(pe.normalizedBefore, pe.lineEnding)
	if err := os.WriteFile(undoFile, []byte(preEditContent), 0644); err != nil {
		debug.Log("edit: undo snapshot write failed: %v", err)
	}
}
