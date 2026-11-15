package runtime

import (
	"encoding/json"
	"slices"
	"strings"

	"nekocode/protocol"
)

const backgroundStructuredReviewBudget = 8 << 10

func taskReviewSize(review *protocol.TaskReview) int {
	data, _ := json.Marshal(review)
	return len(data)
}

// Build a notification projection only. Complete evidence is committed separately
// in the immutable runtime attempt record before this preview is published.
func boundedTaskReview(review protocol.TaskReview) (*protocol.TaskReview, bool) {
	data, err := json.Marshal(review)
	if err != nil || len(data) > backgroundStructuredReviewBudget {
		preview := &protocol.TaskReview{TaskReport: protocol.TaskReport{Summary: backgroundTextPrefix(review.Summary, 1024), Outcome: review.Outcome, NextAction: backgroundTextPrefix(review.NextAction, 1024)}, WorkspaceKind: review.WorkspaceKind, WorkspacePath: backgroundTextPrefix(review.WorkspacePath, 1024), WorkspaceBase: backgroundTextPrefix(review.WorkspaceBase, 256), SourceHead: backgroundTextPrefix(review.SourceHead, 256)}
		if len(review.Unfinished) > 0 {
			preview.Unfinished = []string{"Unfinished work recorded; see full canonical report."}
		}
		if len(review.VerificationGaps) > 0 {
			preview.VerificationGaps = []string{"Verification gaps recorded; see full canonical report."}
		}
		return preview, false
	}
	return cloneTaskReview(&review), true
}

func cloneTaskReview(review *protocol.TaskReview) *protocol.TaskReview {
	if review == nil {
		return nil
	}
	copy := *review
	copy.Evidence = slices.Clone(review.Evidence)
	copy.EvidenceIDs = slices.Clone(review.EvidenceIDs)
	copy.Files = slices.Clone(review.Files)
	copy.Unfinished = slices.Clone(review.Unfinished)
	copy.VerificationGaps = slices.Clone(review.VerificationGaps)
	copy.Risks = slices.Clone(review.Risks)
	copy.Checks = slices.Clone(review.Checks)
	for i := range copy.Checks {
		copy.Checks[i].Limitations = slices.Clone(copy.Checks[i].Limitations)
	}
	return &copy
}

func pendingBackgroundStatus(status string) bool {
	switch status {
	case "queued", "running", "waiting_dependencies", "waiting_approval", "waiting_question":
		return true
	}
	return false
}

func reviewReady(e *backgroundEntry, session string) bool {
	return e != nil && e.SessionID == session && e.cancel == nil && !pendingBackgroundStatus(e.Status) && !e.Notified && e.Delivery == ""
}

// Runtime dispatch and UI updates stay immediate. Explicit complete/continue
// handoffs can wait for their batch; errors and blocked/decision requests flush
// ready results. Legacy reports are delivered immediately. User inputs bypass it.
func (b *backgroundTasks) reviewIDsLocked(session string) ([]string, string) {
	pending, urgent := map[string]bool{}, map[string]bool{}
	for _, e := range b.entries {
		if e.SessionID != session {
			continue
		}
		if e.ReviewPolicy == "batch" && pendingBackgroundStatus(e.Status) {
			pending[e.GroupID] = true
		}
		if e.Status == "waiting_approval" || e.Status == "waiting_question" {
			urgent[e.GroupID] = true
		}
		if reviewReady(e, session) && (e.ReviewPolicy != "batch" || !batchableHandoff(e.BackgroundTask)) {
			urgent[e.GroupID] = true
		}
	}
	var ids []string
	group := ""
	for _, id := range b.order {
		e := b.entries[id]
		if !reviewReady(e, session) {
			continue
		}
		if e.ReviewPolicy == "batch" && batchableHandoff(e.BackgroundTask) && pending[e.GroupID] && !urgent[e.GroupID] {
			continue
		}
		if len(ids) > 0 && group != e.GroupID {
			continue
		}
		group = e.GroupID
		ids = append(ids, id)
		if len(ids) == 16 {
			break
		}
	}
	return ids, group
}

// Explicit scheduling metadata replaces inference from the report's domain
// content. Reports without an explicit outcome are delivered immediately.
func batchableHandoff(task BackgroundTask) bool {
	if task.Error != "" || task.Review == nil {
		return false
	}
	r := task.Review
	switch r.Outcome {
	case "continue":
		return task.Status == "partial" && strings.TrimSpace(r.NextAction) != ""
	case "complete":
		return task.Status == "completed"
	case "blocked", "decision":
		return false
	default:
		return false
	}
}
