package runtime

import (
	"strings"
	"unicode/utf8"

	"nekocode/protocol"
)

// Bound the combined reports, rather than silently reducing every report to a
// short preview. Ordinary reports arrive intact even when workers finish apart.
const backgroundReviewResultBudget = 64 << 10

type backgroundReviewResult struct {
	BackgroundTask
	ResultComplete bool                         `json:"result_complete"`
	ReadResult     *backgroundReviewReadRequest `json:"read_result,omitempty"`
}

type backgroundReviewReadRequest struct {
	ID        string `json:"id"`
	AttemptID string `json:"attempt_id,omitempty"`
	Limit     int    `json:"limit"`
}

type backgroundReviewTask struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	AttemptID string `json:"attempt_id,omitempty"`
	Delivery  string `json:"delivery,omitempty"`
}

type backgroundReview struct {
	Results      []backgroundReviewResult `json:"results"`
	GroupTasks   []backgroundReviewTask   `json:"group_tasks"`
	PendingTasks int                      `json:"pending_tasks"`
}

// Caller holds b.mu. Include the entire demand's inventory, not just the
// workers whose completion happens to have triggered this coordinator turn.
func (b *backgroundTasks) reviewLocked(sessionID, group string, ids []string) backgroundReview {
	review := backgroundReview{Results: []backgroundReviewResult{}, GroupTasks: []backgroundReviewTask{}}
	budget := backgroundReviewResultBudget
	for _, id := range ids {
		task := b.entries[id].BackgroundTask
		task.Prompt = protocol.SubagentTextPreview(task.Prompt, 1024)
		text := backgroundTextPrefix(task.Result, budget)
		complete := len(text) == len(task.Result) && !task.ResultTruncated &&
			!strings.HasSuffix(task.Result, "\n[truncated; inspect saved task output]")
		task.Result = text
		budget -= len(text)
		result := backgroundReviewResult{BackgroundTask: task, ResultComplete: complete}
		if !complete {
			result.ReadResult = &backgroundReviewReadRequest{ID: task.ID, AttemptID: task.AttemptID, Limit: 2000}
		}
		review.Results = append(review.Results, result)
	}
	for _, id := range b.order {
		task := b.entries[id]
		if task == nil || task.SessionID != sessionID || task.GroupID != group {
			continue
		}
		review.GroupTasks = append(review.GroupTasks, backgroundReviewTask{
			ID: task.ID, Title: task.Title, Status: task.Status, AttemptID: task.AttemptID, Delivery: task.Delivery,
		})
		switch task.Status {
		case "queued", "running", "waiting_approval", "waiting_question", "waiting_dependencies":
			review.PendingTasks++
		}
	}
	return review
}

func backgroundTextPrefix(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(text) <= limit {
		return text
	}
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	return text[:limit]
}
