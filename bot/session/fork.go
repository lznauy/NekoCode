package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"nekocode/bot/policy/ledger"
	"nekocode/bot/provider/types"
	"nekocode/util/attachment"
)

// Fork persists an independent copy without changing the active identity.
// The caller must save its live context first and activate only after success.
func (m *Manager) Fork(ctx context.Context, id string) (_ *Snapshot, runErr error) {
	m.persistMu.Lock()
	defer m.persistMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	source, err := load(id)
	if err != nil {
		return nil, err
	}
	if source.Subagent != nil {
		return nil, fmt.Errorf("cannot fork a delegated task as a main session")
	}
	if len(source.Messages) == 0 && len(source.Transcript) == 0 {
		return nil, fmt.Errorf("cannot fork an empty conversation")
	}
	identity := newSnapshot(source.CWD)
	target := filepath.Join(dir(), identity.ID)
	if err = os.MkdirAll(dir(), 0700); err != nil {
		return nil, err
	}
	// Reserve the destination, never overwrite an existing session on collision.
	if err = os.Mkdir(target, 0700); err != nil {
		return nil, err
	}
	imageDir, err := attachment.ImageDir(identity.ID)
	if err != nil {
		os.RemoveAll(target)
		return nil, err
	}
	// Reserve image ownership too, including for a text-only branch. Do not
	// adopt an orphan directory or follow an existing destination symlink.
	if err = os.MkdirAll(filepath.Dir(imageDir), 0700); err != nil {
		os.RemoveAll(target)
		return nil, err
	}
	if err = os.Mkdir(imageDir, 0700); err != nil {
		os.RemoveAll(target)
		return nil, err
	}
	defer func() {
		if runErr != nil {
			runErr = errors.Join(runErr, os.RemoveAll(target), os.RemoveAll(imageDir))
		}
	}()
	fork := source.Clone()
	fork.ID, fork.CreatedAt, fork.UpdatedAt = identity.ID, identity.CreatedAt, identity.UpdatedAt
	fork.ForkedFrom = id
	fork.ForkedAtSeq = len(source.Transcript)
	if len(fork.Transcript) == 0 {
		fork.Transcript = cloneMessages(fork.Messages)
		fork.ForkedAtSeq = len(fork.Transcript)
	}
	fork.history = nil
	fork.TranscriptSeq = 0
	fork.CommittedTranscriptSeq = 0
	fork.TranscriptBase = 0
	fork.TranscriptLazy = false
	fork.CheckpointTurns = nil
	fork.CheckpointNext = 0
	fork.Ledger = ledger.Snapshot{}
	fork.PromptTokens = 0
	fork.CompletionTokens = 0
	fork.TrackerCompletion = 0
	fork.TrackerNewTokens = 0
	fork.CacheHitTokens = 0
	fork.CacheMissTokens = 0
	fork.SubCount = 0
	fork.SubTokens = 0
	fork.SubCacheHit = 0
	fork.SubCacheMiss = 0
	fork.PrefixTurn = nil
	fork.PrefixSystemHash = ""
	fork.PrefixToolsHash = ""
	oldTools, _ := ToolOutputDir(id)
	newTools, _ := ToolOutputDir(fork.ID)
	oldImages, err := attachment.ImageDir(id)
	if err != nil {
		return nil, err
	}
	copied := map[string]string{}
	copyPath := func(path string) (string, error) {
		if replacement, ok := copied[path]; ok {
			return replacement, nil
		}
		for _, pair := range [][2]string{{oldTools, newTools}, {oldImages, imageDir}} {
			rel, err := filepath.Rel(pair[0], path)
			if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
			replacement := filepath.Join(pair[1], rel)
			if err = copyForkFile(ctx, pair[0], pair[1], rel); err != nil {
				return "", fmt.Errorf("copy session attachment %s: %w", path, err)
			}
			copied[path] = replacement
			return replacement, nil
		}
		// Workspace/externally owned references retain their ordinary lifecycle.
		return path, nil
	}
	for _, messages := range [][]types.Message{fork.Messages, fork.Transcript} {
		for i := range messages {
			if path := messages[i].ToolOutput.Path; path != "" {
				replacement, err := copyPath(path)
				if err != nil {
					return nil, err
				}
				messages[i].ToolOutput.Path = replacement
			}
			for j := range messages[i].Images {
				replacement, err := copyPath(messages[i].Images[j].Path)
				if err != nil {
					return nil, err
				}
				messages[i].Images[j].Path = replacement
			}
		}
	}
	// Rendered output references and image envelopes must agree with metadata.
	replacements := make([]string, 0, len(copied)*4)
	textPaths := make(map[string]string, len(copied)*2)
	for old, target := range copied {
		textPaths[old] = target
		// Image envelopes contain JSON-escaped paths on Windows.
		oldJSON, _ := json.Marshal(old)
		targetJSON, _ := json.Marshal(target)
		textPaths[string(oldJSON)] = string(targetJSON)
	}
	keys := make([]string, 0, len(textPaths))
	for old := range textPaths {
		keys = append(keys, old)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, old := range keys {
		replacements = append(replacements, old, textPaths[old])
	}
	if len(replacements) > 0 {
		r := strings.NewReplacer(replacements...)
		fork.Archive = r.Replace(fork.Archive)
		fork.Memory = r.Replace(fork.Memory)
		fork.Skills = r.Replace(fork.Skills)
		for _, messages := range [][]types.Message{fork.Messages, fork.Transcript} {
			for i := range messages {
				messages[i].Content = r.Replace(messages[i].Content)
				messages[i].DeferredContent = r.Replace(messages[i].DeferredContent)
			}
		}
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = fork.save(); err != nil {
		return nil, err
	}
	return fork, nil
}

// os.Root prevents stored references from escaping their owning directory,
// including through symlink components. Only regular files are copied.
func copyForkFile(ctx context.Context, source, target, rel string) error {
	root, err := os.OpenRoot(source)
	if err != nil {
		return err
	}
	defer root.Close()
	info, err := root.Stat(rel)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("attachment is not a regular file")
	}
	file, err := root.Open(rel)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("attachment is not a regular file")
	}
	if err = os.MkdirAll(filepath.Join(target, filepath.Dir(rel)), 0700); err != nil {
		return err
	}
	destination, err := os.OpenRoot(target)
	if err != nil {
		return err
	}
	defer destination.Close()
	out, err := destination.OpenFile(rel, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, &forkReader{ctx: ctx, reader: file})
	if err == nil {
		err = out.Sync()
	}
	return errors.Join(err, out.Close())
}

type forkReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *forkReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
