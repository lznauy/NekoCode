package edit

import (
	"context"
	"fmt"
	"os"
	"strings"

	"nekocode/bot/tools"
	"nekocode/bot/tools/editdsl"
	"nekocode/bot/tools/toolhelpers"
)

type preflightResult struct {
	safePath         string
	normalizedBefore string
	result           *editdsl.ApplyResult
	hunks            []editdsl.Hunk
	lineEnding       string
	origMode         os.FileMode
	recovered        bool
}

func (t *EditTool) prepareOne(ctx context.Context, fp editdsl.FilePatch, cache *editCache, seen map[string]bool) (*preflightResult, error) {
	safePath, err := tools.ValidatePath(fp.Path)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(fp.Path, "/") && safePath == fp.Path {
		return nil, fmt.Errorf("unresolvable path %q", fp.Path)
	}

	data, err := tools.ReadSafeFile(fp.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	origMode := toolhelpers.GetFileMode(safePath)
	rawText := string(data)
	lineEnding := editdsl.DetectLineEnding(rawText)
	normalizedCurrent := tools.NormalizeText(rawText)

	result, recovered, err := t.applyPrepared(ctx, fp, safePath, normalizedCurrent, cache, seen)
	if err != nil {
		return nil, err
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

func (t *EditTool) applyPrepared(ctx context.Context, fp editdsl.FilePatch, safePath, normalizedCurrent string, cache *editCache, seen map[string]bool) (*editdsl.ApplyResult, bool, error) {
	if !seen[safePath] && cache != nil {
		seen[safePath] = true
		if cached, ok := cache.entries[safePath]; ok && cached.normalizedBefore == normalizedCurrent {
			return cached.result, false, nil
		}
	}

	currentHash := editdsl.ComputeFileHash(normalizedCurrent)
	if currentHash != fp.FileTag {
		recoveryResult, recoveryErr := editdsl.TryRecover(editdsl.RecoveryRequest{
			Path:        safePath,
			CurrentText: normalizedCurrent,
			ExpectedTag: fp.FileTag,
			Edits:       fp.Hunks,
			Snapshots:   tools.SnapshotStoreFromContext(ctx),
			Resolver:    GlobalBlockResolver,
		})
		if recoveryErr != nil {
			return nil, false, t.staleTagError(ctx, safePath, normalizedCurrent, fp, recoveryErr)
		}
		return recoveryResult, true, nil
	}

	result, err := editdsl.ApplyEdits(normalizedCurrent, fp.Hunks, GlobalBlockResolver, safePath)
	if err != nil {
		return nil, false, fmt.Errorf("apply failed: %w", err)
	}
	return result, false, nil
}

func (t *EditTool) staleTagError(ctx context.Context, path, normalizedCurrent string, fp editdsl.FilePatch, recoveryErr error) error {
	hashRecognized := false
	if store := tools.SnapshotStoreFromContext(ctx); store != nil {
		hashRecognized = store.ByHash(path, fp.FileTag) != nil
	}
	return &editdsl.MismatchError{
		Path:             path,
		ExpectedFileHash: fp.FileTag,
		ActualFileHash:   editdsl.ComputeFileHash(normalizedCurrent),
		FileLines:        strings.Split(normalizedCurrent, "\n"),
		AnchorLines:      editdsl.CollectAnchorLines(fp.Hunks),
		HashRecognized:   hashRecognized,
	}
}
