package runtime

import (
	"context"
	"fmt"
	"io"
	"sort"
)

type TaskSearchHit struct {
	Sequence int    `json:"sequence"`
	Preview  string `json:"preview"`
}
type TaskSearchPage struct {
	Hits    []TaskSearchHit `json:"hits"`
	Next    int             `json:"next"`
	HasMore bool            `json:"has_more"`
	Total   int             `json:"total"`
}

func (r *Runtime) allSubagents(filter SubagentFilter) (SubagentPage, error) {
	if filter.Offset < 0 || filter.Offset > 100000 || filter.Limit < 1 || filter.Limit > 1000 {
		return SubagentPage{}, fmt.Errorf("invalid task page")
	}
	if err := r.ensureOpen(); err != nil {
		return SubagentPage{}, err
	}
	if r.services.Sessions.ListSessions == nil {
		return SubagentPage{}, fmt.Errorf("session catalog unavailable")
	}
	keep := filter.Offset + filter.Limit
	items := make([]SubagentSummary, 0, keep)
	count := 0
	parents := r.services.Sessions.ListSessions()
	seen := map[string]bool{}
	for _, parent := range parents {
		if seen[parent.ID] {
			continue
		}
		seen[parent.ID] = true
		local := filter
		local.AllSessions = false
		local.SessionID = parent.ID
		local.Offset = 0
		local.Limit = 1000
		for {
			page, err := r.Subagents(local)
			if err != nil {
				return SubagentPage{}, err
			}
			count += len(page.Items)
			for _, item := range page.Items {
				index := sort.Search(len(items), func(i int) bool {
					return items[i].SessionID > item.SessionID || items[i].SessionID == item.SessionID && (items[i].CreatedAt.After(item.CreatedAt) || items[i].CreatedAt.Equal(item.CreatedAt) && items[i].ID >= item.ID)
				})
				if index >= keep {
					continue
				}
				items = append(items, SubagentSummary{})
				copy(items[index+1:], items[index:])
				items[index] = item
				if len(items) > keep {
					items = items[:keep]
				}
			}
			if !page.HasMore {
				break
			}
			local.Offset = page.NextOffset
		}
	}
	if filter.Offset > count {
		return SubagentPage{}, fmt.Errorf("offset exceeds task count")
	}
	end := min(count, keep)
	return SubagentPage{Items: items[filter.Offset:end], Total: count, NextOffset: end, HasMore: end < count}, nil
}

func (r *Runtime) DeleteSubagent(ctx context.Context, parent, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Admission and session mutation share the same gate. The storage callback
	// adds a cross-process lock against resume/open from other applications.
	r.mutationMu.Lock()
	defer r.mutationMu.Unlock()
	if err := r.ensureOpen(); err != nil {
		return err
	}
	b := r.background
	if err := b.load(parent); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	e := b.entries[id]
	if e != nil && e.SessionID != parent {
		return fmt.Errorf("task belongs to another session")
	}
	if e != nil && (e.cancel != nil || e.Status == "queued" || e.Status == "waiting_dependencies" || e.Delivery == "claimed") || b.dependedOnLocked(id) {
		return fmt.Errorf("task is active or leased; cancel and wait before deletion")
	}
	r.mu.Lock()
	busy := r.status != RunIdle
	r.mu.Unlock()
	if busy {
		return fmt.Errorf("wait for the foreground turn before deleting task history")
	}
	if r.services.Sessions.DeleteSubagent == nil {
		return fmt.Errorf("task deletion unavailable")
	}
	if err := r.services.Sessions.DeleteSubagent(parent, id); err != nil {
		return err
	}
	if e != nil {
		b.retireUsageLocked(e)
		delete(b.entries, id)
		index := -1
		for i, key := range b.order {
			if key == id {
				index = i
				break
			}
		}
		if index >= 0 {
			b.order = append(b.order[:index], b.order[index+1:]...)
		}
		if err := b.saveLocked(parent); err != nil {
			// Keep an interrupted tombstone so a retry can finish the sidecar cleanup.
			e.Status = "interrupted"
			e.Error = "history deleted; repeat deletion to finish receipt cleanup"
			e.Notified = true
			e.Delivery = "stopped"
			e.Usage = BackgroundUsage{}
			b.entries[id] = e
			b.order = append(b.order, id)
			return fmt.Errorf("history deleted; receipt cleanup failed, retry: %w", err)
		}
	}
	return nil
}

func (r *Runtime) SearchSubagentOutputs(ctx context.Context, parent, id, query string, after, limit int) (TaskSearchPage, error) {
	if err := r.ensureOpen(); err != nil {
		return TaskSearchPage{}, err
	}
	if r.services.Sessions.SearchSubagentOutputs == nil {
		return TaskSearchPage{}, fmt.Errorf("task output search unavailable")
	}
	return r.services.Sessions.SearchSubagentOutputs(ctx, parent, id, query, after, limit)
}
func (r *Runtime) ExportSubagentOutputs(ctx context.Context, parent, id string, w io.Writer) error {
	if err := r.ensureOpen(); err != nil {
		return err
	}
	if r.services.Sessions.ExportSubagentOutputs == nil {
		return fmt.Errorf("task output export unavailable")
	}
	return r.services.Sessions.ExportSubagentOutputs(ctx, parent, id, w)
}
