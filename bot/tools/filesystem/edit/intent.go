package edit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"nekocode/bot/tools"
	"nekocode/bot/tools/editcore"
	"nekocode/bot/tools/toolhelpers"
)

type editIntent struct {
	Path         string     `json:"path"`
	BaseRevision string     `json:"base_revision"`
	Ops          []intentOp `json:"ops"`
}

type intentOp struct {
	Op      string       `json:"op"`
	Target  intentTarget `json:"target"`
	Content string       `json:"content"`
}

type intentTarget struct {
	WindowID  string `json:"window_id"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

type resolvedIntent struct {
	Ops       []intentOp
	Relocated bool
}

var fileLocks sync.Map

func isJSONIntent(patch string) bool {
	return strings.HasPrefix(strings.TrimSpace(patch), "{")
}

func parseIntent(patch string) (*editIntent, error) {
	var intent editIntent
	if err := json.Unmarshal([]byte(patch), &intent); err != nil {
		return nil, err
	}
	if intent.Path == "" {
		return nil, fmt.Errorf("path is required")
	}
	if len(intent.Ops) == 0 {
		return nil, fmt.Errorf("ops must contain at least one edit operation")
	}
	for i, op := range intent.Ops {
		if op.Target.WindowID == "" {
			return nil, fmt.Errorf("ops[%d].target.window_id is required", i)
		}
		if op.Target.StartLine < 1 {
			return nil, fmt.Errorf("ops[%d].target.start_line must be >= 1", i)
		}
		if op.Target.EndLine == 0 {
			op.Target.EndLine = op.Target.StartLine
			intent.Ops[i] = op
		}
		if op.Target.EndLine < op.Target.StartLine {
			return nil, fmt.Errorf("ops[%d] target end_line precedes start_line", i)
		}
		switch op.Op {
		case "replace", "delete", "insert_before", "insert_after":
		default:
			return nil, fmt.Errorf("ops[%d].op must be replace, delete, insert_before, or insert_after", i)
		}
		if op.Op != "delete" && op.Content == "" {
			return nil, fmt.Errorf("ops[%d].content is required for %s", i, op.Op)
		}
	}
	return &intent, nil
}

func (t *EditTool) previewIntent(patch string) string {
	intent, err := parseIntent(patch)
	if err != nil {
		return fmt.Sprintf("(intent parse error: %v)", err)
	}
	safePath, err := tools.ValidatePath(intent.Path)
	if err != nil {
		return fmt.Sprintf("(%s: %v)", filepath.Base(intent.Path), err)
	}
	oldText, err := tools.ReadNormalizedFile(safePath)
	if err != nil {
		return fmt.Sprintf("(%s: %v)", filepath.Base(safePath), err)
	}
	if err := validateNonOverlapping(intent.Ops); err != nil {
		return fmt.Sprintf("(%s: %v)", filepath.Base(safePath), err)
	}
	hunks, err := intentHunks(intent.Ops)
	if err != nil {
		return fmt.Sprintf("(%s: %v)", filepath.Base(safePath), err)
	}
	result, err := editcore.ApplyEdits(oldText, hunks, GlobalBlockResolver, safePath)
	if err != nil {
		return fmt.Sprintf("(%s: %v)", filepath.Base(safePath), err)
	}
	return appendStructuredDiff(buildDiffPreview(oldText, result.Text, result.ResolvedHunks, result.OldToNew), safePath)
}

func (t *EditTool) executeIntent(ctx context.Context, patch string) (string, error) {
	intent, err := parseIntent(patch)
	if err != nil {
		return "", fmt.Errorf("intent parse error: %w", err)
	}
	safePath, err := tools.ValidatePath(intent.Path)
	if err != nil {
		return "", err
	}

	lock := lockForPath(safePath)
	lock.Lock()
	defer lock.Unlock()

	data, err := tools.ReadSafeFile(safePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}
	origMode := toolhelpers.GetFileMode(safePath)
	rawText := string(data)
	lineEnding := editcore.DetectLineEnding(rawText)
	normalizedBefore := tools.NormalizeText(rawText)
	currentRevision := editcore.ComputeFileHash(normalizedBefore)
	rebased := intent.BaseRevision != "" && currentRevision != intent.BaseRevision

	lines := strings.Split(normalizedBefore, "\n")
	resolved, err := resolveIntent(ctx, *intent, safePath, lines)
	if err != nil {
		if rebased {
			return "", fmt.Errorf("[%s] conflict: base_revision %s is stale and target range cannot be safely replayed on current revision %s: %w", safePath, intent.BaseRevision, currentRevision, err)
		}
		return "", err
	}
	hunks, err := intentHunks(resolved.Ops)
	if err != nil {
		return "", err
	}
	result, err := editcore.ApplyEdits(normalizedBefore, hunks, GlobalBlockResolver, safePath)
	if err != nil {
		return "", fmt.Errorf("apply failed: %w", err)
	}

	pe := preflightResult{
		safePath:         safePath,
		normalizedBefore: normalizedBefore,
		result:           result,
		lineEnding:       lineEnding,
		origMode:         origMode,
	}
	writeUndoSnapshot(pe)
	finalText := editcore.RestoreLineEndings(result.Text, lineEnding)
	if err := os.WriteFile(safePath, []byte(finalText), origMode); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}
	if cache := tools.FileCacheFromContext(ctx); cache != nil {
		cache.Invalidate(safePath)
	}
	newTag := tools.RecordSnapshotInContext(ctx, safePath, result.Text)
	msg := formatEditResult(safePath, normalizedBefore, result.Text, result.ResolvedHunks, newTag, false, result.OldToNew)
	if rebased {
		msg += "\n(rebased: base revision changed, target lines were unchanged)"
	}
	if resolved.Relocated {
		msg += "\n(relocated: target content moved, uniquely re-anchored)"
	}
	if store := tools.ViewStoreFromContext(ctx); store != nil {
		newLines := strings.Split(result.Text, "\n")
		view := store.Register(safePath, newLines, 1, len(newLines))
		msg += fmt.Sprintf("\nVIEW rev=%s window=%s lines=%d..%d total=%d", view.Revision, view.WindowID, view.StartLine, view.EndLine, view.TotalLines)
	}
	for _, w := range result.Warnings {
		msg += "\n" + w
	}
	if lint := lintFile(safePath); lint != "" {
		msg += "\n" + lint
	}
	return msg, nil
}

func resolveIntent(ctx context.Context, intent editIntent, safePath string, lines []string) (resolvedIntent, error) {
	if err := validateNonOverlapping(intent.Ops); err != nil {
		return resolvedIntent{}, err
	}
	store := tools.ViewStoreFromContext(ctx)
	resolved := resolvedIntent{Ops: make([]intentOp, len(intent.Ops))}
	for i, op := range intent.Ops {
		r, err := store.ResolveRange(op.Target.WindowID, safePath, intent.BaseRevision, op.Target.StartLine, op.Target.EndLine, lines)
		if err != nil {
			return resolvedIntent{}, fmt.Errorf("ops[%d]: %w", i, err)
		}
		op.Target.StartLine = r.StartLine
		op.Target.EndLine = r.EndLine
		resolved.Ops[i] = op
		resolved.Relocated = resolved.Relocated || r.Relocated
	}
	if err := validateNonOverlapping(resolved.Ops); err != nil {
		return resolvedIntent{}, fmt.Errorf("resolved ops overlap after relocation: %w", err)
	}
	return resolved, nil
}

func validateNonOverlapping(ops []intentOp) error {
	type rg struct {
		start int
		end   int
		idx   int
	}
	ranges := make([]rg, 0, len(ops))
	for i, op := range ops {
		ranges = append(ranges, rg{start: op.Target.StartLine, end: op.Target.EndLine, idx: i})
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].start < ranges[j].start })
	for i := 1; i < len(ranges); i++ {
		if ranges[i].start <= ranges[i-1].end {
			return fmt.Errorf("ops[%d] overlaps ops[%d]; split or use one wider replace", ranges[i].idx, ranges[i-1].idx)
		}
	}
	return nil
}

func intentHunks(ops []intentOp) ([]editcore.Hunk, error) {
	hunks := make([]editcore.Hunk, 0, len(ops))
	for _, op := range ops {
		payload := splitIntentContent(op.Content)
		switch op.Op {
		case "replace":
			hunks = append(hunks, editcore.Hunk{
				Kind:    editcore.HunkReplace,
				Start:   op.Target.StartLine,
				End:     op.Target.EndLine,
				Payload: payload,
			})
		case "delete":
			hunks = append(hunks, editcore.Hunk{
				Kind:  editcore.HunkDelete,
				Start: op.Target.StartLine,
				End:   op.Target.EndLine,
			})
		case "insert_before":
			hunks = append(hunks, editcore.Hunk{
				Kind:    editcore.HunkInsert,
				Cursor:  editcore.CursorBefore,
				Start:   op.Target.StartLine,
				End:     op.Target.StartLine,
				Payload: payload,
			})
		case "insert_after":
			hunks = append(hunks, editcore.Hunk{
				Kind:    editcore.HunkInsert,
				Cursor:  editcore.CursorAfter,
				Start:   op.Target.EndLine,
				End:     op.Target.EndLine,
				Payload: payload,
			})
		}
	}
	return hunks, nil
}

func splitIntentContent(content string) []string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	content = strings.TrimSuffix(content, "\n")
	if content == "" {
		return nil
	}
	return strings.Split(content, "\n")
}

func lockForPath(path string) *sync.Mutex {
	v, _ := fileLocks.LoadOrStore(path, &sync.Mutex{})
	return v.(*sync.Mutex)
}
