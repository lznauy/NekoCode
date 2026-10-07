package a2aapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
)

const (
	maxStoredTasks        = 256
	maxTaskHistoryEntries = 100
	maxRetainedTaskBytes  = 16 << 20
	maxRetainedStoreBytes = 64 << 20
)

type retainedTask struct {
	store   *taskstore.InMemory
	state   a2a.TaskState
	updated time.Time
	size    int
}

type retainedListItem struct {
	task    *a2a.Task
	updated time.Time
}

// retainedTaskStore keeps each task in its own SDK store so terminal tasks can
// be released without depending on an unbounded store implementation.
type retainedTaskStore struct {
	mu            sync.RWMutex
	limit         int
	maxBytes      int
	totalBytes    int
	authenticator taskstore.Authenticator
	tasks         map[a2a.TaskID]*retainedTask
}

func newRetainedTaskStore(limit int, authenticator taskstore.Authenticator) *retainedTaskStore {
	return newRetainedTaskStoreWithBudget(limit, maxRetainedStoreBytes, authenticator)
}

func newRetainedTaskStoreWithBudget(limit, maxBytes int, authenticator taskstore.Authenticator) *retainedTaskStore {
	if limit < 1 {
		limit = maxStoredTasks
	}
	if maxBytes < 1 {
		maxBytes = maxRetainedStoreBytes
	}
	return &retainedTaskStore{limit: limit, maxBytes: maxBytes, authenticator: authenticator, tasks: make(map[a2a.TaskID]*retainedTask)}
}

func (s *retainedTaskStore) Create(ctx context.Context, task *a2a.Task) (taskstore.TaskVersion, error) {
	if task == nil {
		return taskstore.TaskVersionMissing, a2a.ErrInvalidParams
	}
	size, err := boundRetainedTask(task)
	if err != nil {
		return taskstore.TaskVersionMissing, err
	}
	store := taskstore.NewInMemory(&taskstore.InMemoryStoreConfig{Authenticator: s.authenticator})
	version, err := store.Create(ctx, task)
	if err != nil {
		return taskstore.TaskVersionMissing, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.tasks[task.ID]; exists {
		return taskstore.TaskVersionMissing, taskstore.ErrTaskAlreadyExists
	}
	if size > s.maxBytes {
		return taskstore.TaskVersionMissing, fmt.Errorf("A2A task exceeds store byte budget: %w", a2a.ErrInvalidParams)
	}
	requiredCount := max(0, len(s.tasks)+1-s.limit)
	requiredBytes := max(0, s.totalBytes+size-s.maxBytes)
	if !s.canReclaimLocked("", requiredCount, requiredBytes) {
		return taskstore.TaskVersionMissing, fmt.Errorf("A2A task retention limit reached: %w", a2a.ErrServerError)
	}
	for len(s.tasks) >= s.limit || s.totalBytes+size > s.maxBytes {
		s.evictOldestTerminalLocked("")
	}
	s.tasks[task.ID] = &retainedTask{store: store, state: task.Status.State, updated: time.Now(), size: size}
	s.totalBytes += size
	return version, nil
}

func (s *retainedTaskStore) Update(ctx context.Context, update *taskstore.UpdateRequest) (taskstore.TaskVersion, error) {
	if update == nil || update.Task == nil {
		return taskstore.TaskVersionMissing, a2a.ErrInvalidParams
	}
	size, err := boundRetainedTask(update.Task)
	if err != nil {
		return taskstore.TaskVersionMissing, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.tasks[update.Task.ID]
	if entry == nil {
		return taskstore.TaskVersionMissing, a2a.ErrTaskNotFound
	}
	if size > s.maxBytes {
		return taskstore.TaskVersionMissing, fmt.Errorf("A2A task exceeds store byte budget: %w", a2a.ErrInvalidParams)
	}
	required := s.totalBytes - entry.size + size - s.maxBytes
	if !s.canReclaimLocked(update.Task.ID, 0, required) {
		return taskstore.TaskVersionMissing, fmt.Errorf("A2A task retention limit reached: %w", a2a.ErrServerError)
	}
	version, err := entry.store.Update(ctx, update)
	if err != nil {
		return taskstore.TaskVersionMissing, err
	}
	for s.totalBytes-entry.size+size > s.maxBytes {
		s.evictOldestTerminalLocked(update.Task.ID)
	}
	s.totalBytes = s.totalBytes - entry.size + size
	entry.size = size
	entry.state = update.Task.Status.State
	entry.updated = time.Now()
	return version, nil
}

func (s *retainedTaskStore) canReclaimLocked(exclude a2a.TaskID, requiredCount, requiredBytes int) bool {
	if requiredCount <= 0 && requiredBytes <= 0 {
		return true
	}
	reclaimableCount, reclaimableBytes := 0, 0
	for id, candidate := range s.tasks {
		if (exclude == "" || id != exclude) && candidate.state.Terminal() {
			reclaimableCount++
			reclaimableBytes += candidate.size
		}
	}
	return reclaimableCount >= requiredCount && reclaimableBytes >= requiredBytes
}

func (s *retainedTaskStore) Get(ctx context.Context, taskID a2a.TaskID) (*taskstore.StoredTask, error) {
	s.mu.RLock()
	entry := s.tasks[taskID]
	s.mu.RUnlock()
	if entry == nil {
		return nil, a2a.ErrTaskNotFound
	}
	return entry.store.Get(ctx, taskID)
}

func (s *retainedTaskStore) List(ctx context.Context, req *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	if req == nil {
		return nil, a2a.ErrInvalidParams
	}
	if user, err := s.authenticator(ctx); err != nil || user == "" {
		return nil, a2a.ErrUnauthenticated
	}
	pageSize := req.PageSize
	if pageSize == 0 {
		pageSize = 50
	} else if pageSize < 1 || pageSize > 100 {
		return nil, fmt.Errorf("page size must be between 1 and 100 inclusive: %w", a2a.ErrInvalidRequest)
	}

	s.mu.RLock()
	entries := make(map[a2a.TaskID]retainedTask, len(s.tasks))
	for id, entry := range s.tasks {
		entries[id] = *entry
	}
	s.mu.RUnlock()
	listed := make([]retainedListItem, 0, len(entries))
	for id, entry := range entries {
		stored, err := entry.store.Get(ctx, id)
		if err != nil {
			continue
		}
		task := stored.Task
		if req.ContextID != "" && task.ContextID != req.ContextID {
			continue
		}
		if req.Status != a2a.TaskStateUnspecified && task.Status.State != req.Status {
			continue
		}
		if req.StatusTimestampAfter != nil && task.Status.Timestamp != nil && task.Status.Timestamp.Before(*req.StatusTimestampAfter) {
			continue
		}
		trimListedTask(task, req)
		listed = append(listed, retainedListItem{task: task, updated: entry.updated})
	}
	sort.Slice(listed, func(i, j int) bool {
		if !listed[i].updated.Equal(listed[j].updated) {
			return listed[i].updated.After(listed[j].updated)
		}
		return listed[i].task.ID > listed[j].task.ID
	})

	start, err := listStart(listed, req.PageToken)
	if err != nil {
		return nil, err
	}
	total := len(listed)
	end := min(start+pageSize, total)
	tasks := make([]*a2a.Task, 0, end-start)
	for _, item := range listed[start:end] {
		tasks = append(tasks, item.task)
	}
	next := ""
	if end < total {
		next = encodeListCursor(listed[end-1].updated, listed[end-1].task.ID)
	}
	return &a2a.ListTasksResponse{Tasks: tasks, TotalSize: total, PageSize: pageSize, NextPageToken: next}, nil
}

func (s *retainedTaskStore) evictOldestTerminalLocked(exclude a2a.TaskID) bool {
	var oldestID a2a.TaskID
	var oldest time.Time
	for id, entry := range s.tasks {
		if id == exclude || !entry.state.Terminal() || (!oldest.IsZero() && !entry.updated.Before(oldest)) {
			continue
		}
		oldestID, oldest = id, entry.updated
	}
	if oldestID == "" {
		return false
	}
	s.totalBytes -= s.tasks[oldestID].size
	delete(s.tasks, oldestID)
	return true
}

func trimListedTask(task *a2a.Task, req *a2a.ListTasksRequest) {
	historyLength := 100
	if req.HistoryLength != nil {
		historyLength = *req.HistoryLength
	}
	if historyLength <= 0 {
		task.History = []*a2a.Message{}
	} else if len(task.History) > historyLength {
		task.History = task.History[len(task.History)-historyLength:]
	}
	if !req.IncludeArtifacts {
		task.Artifacts = nil
	}
}

func boundRetainedTask(task *a2a.Task) (int, error) {
	if len(task.History) > maxTaskHistoryEntries {
		task.History = task.History[len(task.History)-maxTaskHistoryEntries:]
	}
	for {
		data, err := json.Marshal(task)
		if err != nil {
			return 0, fmt.Errorf("encode retained A2A task: %w", err)
		}
		if len(data) <= maxRetainedTaskBytes {
			return len(data), nil
		}
		if len(task.History) <= 1 {
			return 0, fmt.Errorf("A2A task exceeds retained size limit: %w", a2a.ErrInvalidParams)
		}
		task.History = task.History[1:]
	}
}

type listCursor struct {
	Updated int64      `json:"updated"`
	TaskID  a2a.TaskID `json:"taskId"`
}

func encodeListCursor(updated time.Time, taskID a2a.TaskID) string {
	data, _ := json.Marshal(listCursor{Updated: updated.UnixNano(), TaskID: taskID})
	return base64.RawURLEncoding.EncodeToString(data)
}

func listStart(listed []retainedListItem, token string) (int, error) {
	if strings.TrimSpace(token) == "" {
		return 0, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, fmt.Errorf("invalid page token: %w", a2a.ErrInvalidRequest)
	}
	var cursor listCursor
	if json.Unmarshal(data, &cursor) != nil {
		return 0, fmt.Errorf("invalid page token: %w", a2a.ErrInvalidRequest)
	}
	for i, item := range listed {
		if item.task.ID == cursor.TaskID && item.updated.UnixNano() == cursor.Updated {
			return i + 1, nil
		}
	}
	return 0, fmt.Errorf("stale page token: %w", a2a.ErrInvalidRequest)
}

var _ taskstore.Store = (*retainedTaskStore)(nil)
