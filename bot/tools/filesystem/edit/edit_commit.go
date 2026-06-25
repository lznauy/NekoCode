package edit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"nekocode/bot/debug"
	"nekocode/bot/tools"
	"nekocode/bot/tools/editcore"
	"nekocode/bot/tools/toolhelpers"
)

type preflightResult struct {
	safePath         string
	normalizedBefore string
	result           *editcore.ApplyResult
	lineEnding       string
	origMode         os.FileMode
}

func snapshotUndoPath(safePath string) string {
	h := sha256.Sum256([]byte(safePath))
	hash := hex.EncodeToString(h[:])[:16]
	return filepath.Join("/tmp/nekocode/snapshots", hash+"_"+filepath.Base(safePath)+".pre-edit")
}

func (t *EditTool) revertSnapshot(patchStr string) (string, error) {
	safePath, err := tools.ValidatePath(patchStr)
	if err != nil {
		return "", fmt.Errorf("revert: invalid path: %w", err)
	}
	undoFile := snapshotUndoPath(safePath)
	preData, err := os.ReadFile(undoFile)
	if err != nil {
		return "", fmt.Errorf("revert: no snapshot for %s: %w", filepath.Base(safePath), err)
	}
	mode := toolhelpers.GetFileMode(safePath)
	if err := os.WriteFile(safePath, preData, mode); err != nil {
		return "", fmt.Errorf("revert: write failed: %w", err)
	}
	newTag := tools.RecordSnapshot(safePath, string(preData))
	return fmt.Sprintf("[%s#%s] Reverted to pre-edit state from %s\n", safePath, newTag, undoFile), nil
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
