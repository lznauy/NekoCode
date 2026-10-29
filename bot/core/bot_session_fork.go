package core

import (
	"context"
	"fmt"

	"nekocode/bot/extension/tool/runtime/execution"
	"nekocode/bot/session"
)

// ForkSession must run within the caller's idle mutation lifecycle, like resume.
func (b *Bot) ForkSession(ctx context.Context) (*session.Snapshot, error) {
	b.historyMu.Lock()
	defer b.historyMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	snap := b.ctxMgr.Snapshot()
	if len(snap.Messages) == 0 && len(snap.Transcript) == 0 && snap.TranscriptBase == 0 {
		return nil, fmt.Errorf("cannot fork an empty conversation")
	}
	if err := b.saveSession(); err != nil {
		return nil, fmt.Errorf("save source session: %w", err)
	}
	fork, err := b.sess.Fork(ctx, b.sess.CurrentID())
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, fmt.Errorf("fork %s saved but not activated: %w", fork.ID, err)
	}
	restored, err := b.resumeSessionLocked(fork.ID)
	if err != nil {
		return restored, fmt.Errorf("fork %s saved; session switch failed: %w", fork.ID, err)
	}
	if ag := b.getAgent(); ag != nil {
		state := ag.ToolExecutionState()
		state.FileCache = execution.NewFileStateCache()
		state.SnapshotStore = execution.NewSnapshotStore()
	}
	return restored, nil
}
