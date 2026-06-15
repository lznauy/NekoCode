// EditTool — hashline DSL-based file editing.

package builtin

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"nekocode/bot/debug"
	"nekocode/bot/tools"
	"nekocode/bot/tools/hashline"
)

// ---------------------------------------------------------------------------
// tool definition
// ---------------------------------------------------------------------------

type EditTool struct {
	WriteModeTool
}

func (t *EditTool) Name() string { return "edit" }

// snapshotUndoPath returns the pre-edit snapshot path in /tmp for the given file.
func snapshotUndoPath(safePath string) string {
	h := sha256.Sum256([]byte(safePath))
	hash := hex.EncodeToString(h[:])[:16]
	return filepath.Join("/tmp/nekocode/snapshots", hash+"_"+filepath.Base(safePath)+".pre-edit")
}

//go:embed edit_description.md
var editDescription string

func (t *EditTool) Description() string {
	return editDescription
}

func (t *EditTool) Parameters() []tools.Parameter {
	return []tools.Parameter{
		{Name: "patch", Type: "string", Required: true,
			Description: "Hashline patch DSL. See tool description for format. When revert=true, use bare file path instead."},
		{Name: "revert", Type: "boolean", Required: false,
			Description: "Set to true to revert file to its pre-edit state. Patch should be the bare file path."},
	}
}

// ---------------------------------------------------------------------------
// types
// ---------------------------------------------------------------------------

// editCache holds Preview results for Execute to reuse.
type editCache struct {
	entries map[string]editCacheEntry
}

type editCacheEntry struct {
	safePath         string
	normalizedBefore string
	result           *hashline.ApplyResult
}

// preflightResult carries the in-memory result of a prepareOne call so
// commitResult can land it on disk without re-reading or re-applying.
type preflightResult struct {
	safePath         string
	normalizedBefore string
	result           *hashline.ApplyResult
	hunks            []hashline.Hunk
	lineEnding       string
	origMode         os.FileMode
	recovered        bool
}

// ---------------------------------------------------------------------------
// preview
// ---------------------------------------------------------------------------

// Preview reads files, applies edits to copies, returns a diff for TUI.
func (t *EditTool) Preview(args map[string]any) string {
	patchStr, _ := args["patch"].(string)
	if patchStr == "" {
		return ""
	}
	// Revert mode: patch is a bare file path, not a hashline DSL.
	if rv, _ := args["revert"].(bool); rv {
		return fmt.Sprintf("(revert: %s)", filepath.Base(patchStr))
	}
	patch, err := hashline.ParsePatch(patchStr)
	if err != nil {
		return fmt.Sprintf("(parse error: %v)", err)
	}

	cache := &editCache{entries: make(map[string]editCacheEntry)}
	var sb strings.Builder
	var errs []string
	for _, fp := range patch.Files {
		safePath, err := tools.ValidatePath(fp.Path)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", filepath.Base(fp.Path), err))
			continue
		}
		oldText, err := tools.ReadNormalizedFile(safePath)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", filepath.Base(fp.Path), err))
			continue
		}
		result, err := hashline.ApplyEdits(oldText, fp.Hunks, GlobalBlockResolver, safePath)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", filepath.Base(fp.Path), err))
			continue
		}
		cache.entries[safePath] = editCacheEntry{safePath, oldText, result}
		preview := buildDiffPreview(oldText, result.Text, result.ResolvedHunks, result.OldToNew)
		if preview == "" {
			continue
		}
		sb.WriteString(preview)
		sb.WriteByte('\n')
	}
	if len(errs) > 0 {
		fmt.Fprintf(&sb, "\n(errors: %s)", strings.Join(errs, "; "))
	}
	if len(cache.entries) > 0 {
		args["_editCache"] = cache
	}
	return sb.String()
}

// ---------------------------------------------------------------------------
// execute
// ---------------------------------------------------------------------------

func (t *EditTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	patchStr, ok := args["patch"].(string)
	if !ok || patchStr == "" {
		return "", fmt.Errorf("patch parameter is required")
	}

	// Revert mode: restore file from pre-edit snapshot.
	if rv, _ := args["revert"].(bool); rv {
		safePath, err := tools.ValidatePath(patchStr)
		if err != nil {
			return "", fmt.Errorf("revert: invalid path: %w", err)
		}
		undoFile := snapshotUndoPath(safePath)
		preData, err := os.ReadFile(undoFile)
		if err != nil {
			return "", fmt.Errorf("revert: no snapshot for %s: %w", filepath.Base(safePath), err)
		}
		mode := getFileMode(safePath)
		if err := os.WriteFile(safePath, preData, mode); err != nil {
			return "", fmt.Errorf("revert: write failed: %w", err)
		}
		newTag := tools.RecordSnapshot(safePath, string(preData))
		return fmt.Sprintf("[%s#%s] Reverted to pre-edit state from %s\n", safePath, newTag, undoFile), nil
	}

	patch, err := hashline.ParsePatch(patchStr)
	if err != nil {
		return "", fmt.Errorf("patch parse error: %w", err)
	}
	var cache *editCache
	if c, ok := args["_editCache"]; ok {
		cache, _ = c.(*editCache)
	}
	seen := make(map[string]bool)

	// Preflight: validate and apply every file in memory before any write.
	var prepared []preflightResult
	for _, fp := range patch.Files {
		pe, err := t.prepareOne(fp, cache, seen)
		if err != nil {
			return "", fmt.Errorf("[%s] %w", fp.Path, err)
		}
		prepared = append(prepared, *pe)
	}

	// Commit: write every prepared result to disk.
	var results []string
	var writeErrors []string
	var writtenPaths []string
	for _, pe := range prepared {
		msg, err := t.commitResult(pe)
		if err != nil {
			writeErrors = append(writeErrors, fmt.Sprintf("%s: %v", pe.safePath, err))
		} else {
			results = append(results, msg)
			writtenPaths = append(writtenPaths, pe.safePath)
		}
	}
	if len(writeErrors) > 0 {
		var summary strings.Builder
		summary.WriteString("Partial commit: some files written, some failed.\n")
		for _, p := range writtenPaths {
			summary.WriteString("  written: ")
			summary.WriteString(p)
			summary.WriteByte('\n')
		}
		for _, e := range writeErrors {
			summary.WriteString("  failed: ")
			summary.WriteString(e)
			summary.WriteByte('\n')
		}
		if len(results) > 0 {
			summary.WriteString("\nResults from successful writes:\n")
			summary.WriteString(strings.Join(results, "\n"))
		}
		return summary.String(), fmt.Errorf("%d file(s) failed to write", len(writeErrors))
	}
	return strings.Join(results, "\n"), nil
}

// prepareOne validates, reads, and applies edits to a single file in memory.
// Returns a preflightResult for commitResult; does NOT write to disk.
func (t *EditTool) prepareOne(fp hashline.FilePatch, cache *editCache, seen map[string]bool) (*preflightResult, error) {
	safePath, err := tools.ValidatePath(fp.Path)
	if err != nil {
		return nil, err
	}

	// Accept relative paths that ValidatePath successfully resolved.
	// The LLM should use absolute paths from Read output ([ABSPATH#TAG]),
	// but resolving a relative path is safe and more forgiving.
	if !strings.HasPrefix(fp.Path, "/") && safePath == fp.Path {
		return nil, fmt.Errorf("unresolvable path %q", fp.Path)
	}

	// Read current file content.
	data, err := tools.ReadSafeFile(fp.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	origMode := getFileMode(safePath)

	rawText := string(data)
	lineEnding := hashline.DetectLineEnding(rawText)
	normalizedCurrent := tools.NormalizeText(rawText)

	// Try preview cache.
	var result *hashline.ApplyResult
	var recovered bool

	if !seen[safePath] && cache != nil {
		seen[safePath] = true
		if cached, ok := cache.entries[safePath]; ok && cached.normalizedBefore == normalizedCurrent {
			result = cached.result
		}
	}

	// No usable cache — apply directly.
	if result == nil {
		currentHash := hashline.ComputeFileHash(normalizedCurrent)
		if currentHash != fp.FileTag {
			recoveryResult, recoveryErr := hashline.TryRecover(hashline.RecoveryRequest{
				Path:        safePath,
				CurrentText: normalizedCurrent,
				ExpectedTag: fp.FileTag,
				Edits:       fp.Hunks,
				Snapshots:   tools.GetGlobalSnapshotStore(),
				Resolver:    GlobalBlockResolver,
			})
			if recoveryErr != nil {
				return nil, t.staleTagError(safePath, normalizedCurrent, fp, recoveryErr)
			}
			result = recoveryResult
			recovered = true
		} else {
			var err error
			result, err = hashline.ApplyEdits(normalizedCurrent, fp.Hunks, GlobalBlockResolver, safePath)
			if err != nil {
				return nil, fmt.Errorf("apply failed: %w", err)
			}
		}
	}

	return &preflightResult{
		safePath:         safePath,
		normalizedBefore: normalizedCurrent,
		result:           result,
		hunks:            fp.Hunks,
		lineEnding:       lineEnding,
		origMode:         origMode,
		recovered:        recovered,
	}, nil
}

// commitResult writes the apply result to disk, records snapshot, and formats output.
func (t *EditTool) commitResult(pe preflightResult) (string, error) {
	finalText := hashline.RestoreLineEndings(pe.result.Text, pe.lineEnding)

	// Save pre-edit content for undo via write tool.
	undoFile := snapshotUndoPath(pe.safePath)
	if err := os.MkdirAll(filepath.Dir(undoFile), 0755); err == nil {
		preEditContent := hashline.RestoreLineEndings(pe.normalizedBefore, pe.lineEnding)
		if err := os.WriteFile(undoFile, []byte(preEditContent), 0644); err != nil {
			debug.Log("edit: undo snapshot write failed: %v", err)
		}
	}

	if err := os.WriteFile(pe.safePath, []byte(finalText), pe.origMode); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	newTag := tools.RecordSnapshot(pe.safePath, pe.result.Text)

	msg := formatEditResult(pe.safePath, pe.normalizedBefore, pe.result.Text, pe.result.ResolvedHunks, newTag, pe.recovered, pe.result.OldToNew)

	if len(pe.result.Warnings) > 0 {
		for _, w := range pe.result.Warnings {
			msg += "\n" + w
		}
	}

	if lint := lintFile(pe.safePath); lint != "" {
		msg += "\n" + lint
	}
	return msg, nil
}

// staleTagError builds a MismatchError with anchor-line context so the
// agent can see which lines its hunk(s) reference in the current file.
func (t *EditTool) staleTagError(path, normalizedCurrent string, fp hashline.FilePatch, recoveryErr error) error {
	hashRecognized := false
	if store := tools.GetGlobalSnapshotStore(); store != nil {
		hashRecognized = store.ByHash(path, fp.FileTag) != nil
	}
	return &hashline.MismatchError{
		Path:             path,
		ExpectedFileHash: fp.FileTag,
		ActualFileHash:   hashline.ComputeFileHash(normalizedCurrent),
		FileLines:        strings.Split(normalizedCurrent, "\n"),
		AnchorLines:      hashline.CollectAnchorLines(fp.Hunks),
		HashRecognized:   hashRecognized,
	}
}

// ---------------------------------------------------------------------------
// lint
// ---------------------------------------------------------------------------

// lintFile runs gofmt on .go files and returns errors, or empty string.
func lintFile(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return lintGo(path)
	default:
		return ""
	}
}

func lintGo(path string) string {
	cmd := exec.Command("gofmt", "-e", path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg != "" {
			return fmt.Sprintf("gofmt: %s", msg)
		}
	}
	return ""
}
