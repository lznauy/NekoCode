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

// PatchTodos overlays explicitly named steps without dropping unrelated work.
// New steps omit ID and still require content/status. Settled transitions keep
// ReconcileTodos' requirement for fresh completion/reopening evidence.
func PatchTodos(previous, updates []TodoItem) ([]TodoItem, error) {
	old := EnsureTodoIDs(previous)
	next := append([]TodoItem(nil), old...)
	seen := map[string]bool{}
	for _, item := range updates {
		if item.ID == "" {
			next = append(next, item)
			continue
		}
		if seen[item.ID] {
			return nil, fmt.Errorf("duplicate todo id %q", item.ID)
		}
		seen[item.ID] = true
		found := false
		for i, prior := range next {
			if prior.ID != item.ID {
				continue
			}
			found = true
			if item.Content == "" {
				item.Content = prior.Content
			}
			if item.Status == "" {
				item.Status = prior.Status
			}
			next[i] = item
			break
		}
		if !found {
			return nil, fmt.Errorf("unknown todo id %q; use a returned ID or omit id for a new step", item.ID)
		}
	}
	return ReconcileTodos(old, next)
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
