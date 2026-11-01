package core

import (
	"context"

	"nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool/runtime/taskbridge"
)

// Reuse the same metadata projection as task_list, including its attempt merge.
// No transcript bodies or additional persisted index are needed for task refs.
func (b *Bot) taskReferences(ctx context.Context, sessionID string) contextmgr.TaskReferences {
	b.mu.Lock()
	list := b.taskLister
	b.mu.Unlock()
	if list == nil || sessionID == "" {
		return contextmgr.TaskReferences{}
	}
	refs := contextmgr.TaskReferences{SessionID: sessionID}
	page, err := list(ctx, taskbridge.TaskListFilter{SessionID: sessionID, PrioritizeActive: true, Limit: contextmgr.MaxTaskReferences})
	if err != nil || page.SessionID != sessionID || b.sess.CurrentID() != sessionID {
		refs.Unavailable = true
		return refs
	}
	refs.Total = page.Total
	for _, item := range page.Items[:min(len(page.Items), contextmgr.MaxTaskReferences)] {
		ref := contextmgr.TaskReference{ID: item.ID, Recorded: item.Recorded, Profile: item.Profile, Title: item.Title, Status: item.Status}
		if attempt := item.LatestAttempt; attempt != nil {
			ref.AttemptID, ref.AttemptSeq = attempt.ID, attempt.Seq
		}
		refs.Items = append(refs.Items, ref)
	}
	return refs
}
