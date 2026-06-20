// EditTool — hashline DSL-based file editing.

package edit

import (
	"context"
	_ "embed"
	"fmt"
	"path/filepath"
	"strings"

	"nekocode/bot/tools"
	"nekocode/bot/tools/editdsl"
	"nekocode/bot/tools/toolhelpers"
)

// ---------------------------------------------------------------------------
// tool definition
// ---------------------------------------------------------------------------

type EditTool struct {
	toolhelpers.WriteModeTool
}

func (t *EditTool) Name() string { return "edit" }

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
	result           *editdsl.ApplyResult
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
	patch, err := editdsl.ParsePatch(patchStr)
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
		result, err := editdsl.ApplyEdits(oldText, fp.Hunks, GlobalBlockResolver, safePath)
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
		return t.revertSnapshot(patchStr)
	}

	patch, err := editdsl.ParsePatch(patchStr)
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
		pe, err := t.prepareOne(ctx, fp, cache, seen)
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
		msg, err := t.commitResult(ctx, pe)
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
