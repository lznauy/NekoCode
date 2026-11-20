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
	ID              string                       `json:"id"`
	Title           string                       `json:"title"`
	Status          string                       `json:"status"`
	AttemptID       string                       `json:"attempt_id,omitempty"`
	AttemptSeq      int                          `json:"attempt_seq,omitempty"`
	Isolation       string                       `json:"isolation,omitempty"`
	Error           string                       `json:"error,omitempty"`
	Result          string                       `json:"result,omitempty"`
	ResultTruncated bool                         `json:"result_truncated,omitempty"`
	Review          *protocol.TaskReview         `json:"review,omitempty"`
	ReviewComplete  bool                         `json:"review_complete"`
	ResultComplete  bool                         `json:"result_complete"`
	ReadResult      *backgroundReviewReadRequest `json:"read_result,omitempty"`
}

type backgroundReviewReadRequest struct {
	Ref       string `json:"result_ref,omitempty"`
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
	GroupID        string                   `json:"plan_id,omitempty"`
	Results        []backgroundReviewResult `json:"results"`
	GroupTasks     []backgroundReviewTask   `json:"group_tasks"`
	PendingTasks   int                      `json:"pending_tasks"`
	PendingReviews int                      `json:"pending_reviews"`
}

// Caller holds b.mu. Include the entire demand's inventory, not just the
// workers whose completion happens to have triggered this coordinator turn.
func (b *backgroundTasks) reviewLocked(sessionID, group string, ids []string) backgroundReview {
	review := backgroundReview{GroupID: group, Results: []backgroundReviewResult{}, GroupTasks: []backgroundReviewTask{}}
	included := map[string]bool{}
	budget := backgroundReviewResultBudget
	for _, id := range ids {
		included[id] = true
		task := b.entries[id].BackgroundTask
		result := backgroundReviewResult{ID: task.ID, Title: task.Title, Status: task.Status, AttemptID: task.AttemptID, AttemptSeq: task.AttemptSeq, Isolation: task.Isolation, Error: task.Error, ResultTruncated: task.ResultTruncated}
		if task.Review != nil {
			// Do not repeat the formatted report when an exact structured handoff
			// fits. Larger handoffs use their exact canonical result reference.
			copy, complete := boundedTaskReview(*task.Review)
			dataSize := taskReviewSize(copy)
			if dataSize <= budget {
				result.Review, result.ReviewComplete = copy, complete && task.ReviewComplete
				budget -= dataSize
			}
		}
		if task.Review == nil {
			result.Result = backgroundTextPrefix(task.Result, budget)
			result.ResultComplete = len(result.Result) == len(task.Result) && !task.ResultTruncated && !strings.HasSuffix(task.Result, "\n[truncated; inspect saved task output]")
			budget -= len(result.Result)
		}
		if !result.ResultComplete && !result.ReviewComplete {
			result.ReadResult = &backgroundReviewReadRequest{Ref: task.ResultRef, ID: task.ID, AttemptID: task.AttemptID, Limit: 2000}
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
		if pendingBackgroundStatus(task.Status) {
			review.PendingTasks++
		} else if !included[id] && !task.Notified && task.Delivery != "stopped" {
			review.PendingReviews++
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
