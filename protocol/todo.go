package protocol

import (
	"crypto/rand"
	"fmt"
	"strings"
)

const MaxTodoItems = 64

func TodoActionable(item TodoItem) bool {
	return item.Status != "completed" && item.Status != "blocked" && item.Status != "cancelled"
}

// ReconcileTodos validates one replacement without losing unfinished steps.
// Missing IDs remain compatible with older callers when content matches exactly.
func ReconcileTodos(previous, proposed []TodoItem) ([]TodoItem, error) {
	if len(proposed) > MaxTodoItems {
		return nil, fmt.Errorf("at most %d todo items are allowed", MaxTodoItems)
	}
	old := EnsureTodoIDs(previous)
	used := make(map[string]bool, len(proposed))
	next := make([]TodoItem, 0, len(proposed))
	active := 0
	for _, item := range proposed {
		item.Content = strings.TrimSpace(item.Content)
		item.Result = strings.TrimSpace(item.Result)
		item.Reason = strings.TrimSpace(item.Reason)
		if item.Content == "" || len(item.Content) > 4096 || len(item.Result) > 4096 || len(item.Reason) > 2048 {
			return nil, fmt.Errorf("todo content must be nonempty and within limits (content/result 4096, reason 2048 bytes)")
		}
		var prior *TodoItem
		for i := range old {
			matches := item.ID != "" && item.ID == old[i].ID || item.ID == "" && item.Content == old[i].Content && !used[old[i].ID]
			if matches {
				if prior != nil {
					return nil, fmt.Errorf("ambiguous todo %q; activate the plan with plan_id and no todos to obtain stable IDs first", item.Content)
				}
				prior = &old[i]
			}
		}
		if prior == nil {
			if item.ID != "" {
				return nil, fmt.Errorf("unknown todo id %q; omit id to create a new step", item.ID)
			}
			item.ID = "todo_" + rand.Text()
		} else {
			item.ID = prior.ID
			if prior.Status == item.Status {
				if item.Result == "" {
					item.Result = prior.Result
				}
				if item.Reason == "" {
					item.Reason = prior.Reason
				}
			}
			if (prior.Status == "completed" || prior.Status == "cancelled") && item.Status != prior.Status && item.Reason == "" {
				return nil, fmt.Errorf("reopening todo %s requires a reason", item.ID)
			}
		}
		if used[item.ID] {
			return nil, fmt.Errorf("duplicate todo id %q", item.ID)
		}
		used[item.ID] = true
		switch item.Status {
		case "pending":
		case "in_progress":
			active++
		case "completed":
			if item.Result == "" && (prior == nil || prior.Status != "completed") {
				return nil, fmt.Errorf("completed todo %s requires a brief result", item.ID)
			}
		case "blocked", "cancelled":
			if item.Reason == "" {
				return nil, fmt.Errorf("%s todo %s requires a reason", item.Status, item.ID)
			}
		default:
			return nil, fmt.Errorf("invalid todo status %q", item.Status)
		}
		next = append(next, item)
	}
	if active > 1 {
		return nil, fmt.Errorf("at most one todo may be in_progress")
	}
	for _, item := range old {
		if !used[item.ID] && item.Status != "completed" && item.Status != "cancelled" {
			return nil, fmt.Errorf("unfinished todo %s (%s) cannot disappear; retain it or cancel it with a reason", item.ID, item.Content)
		}
	}
	return next, nil
}

// EnsureTodoIDs upgrades legacy records before returning them to a model.
func EnsureTodoIDs(items []TodoItem) []TodoItem {
	out := append([]TodoItem(nil), items...)
	for i := range out {
		if out[i].ID == "" {
			out[i].ID = "todo_" + rand.Text()
		}
	}
	return out
}
