package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"nekocode/protocol"
)

const backgroundResultRefPrefix = "background-result.v1."
const maxBackgroundResultBytes = 8 << 20

// One immutable record per task attempt. Mutable scheduling state contains only
// a reference and delivery projection; notification limits never discard evidence.
type backgroundResultRecord struct {
	SessionID  string                    `json:"session_id"`
	TaskID     string                    `json:"task_id"`
	AttemptID  string                    `json:"attempt_id"`
	AttemptSeq int                       `json:"attempt_seq,omitempty"`
	Error      string                    `json:"error,omitempty"`
	Status     string                    `json:"status"`
	Isolation  string                    `json:"isolation,omitempty"`
	Result     protocol.BackgroundResult `json:"result"`
}

func backgroundResultText(result protocol.BackgroundResult) string {
	if result.Review == nil {
		return result.Text
	}
	data, _ := json.Marshal(result.Review)
	return string(data)
}

func backgroundResultKey(id string) string {
	digest := sha256.Sum256([]byte(id))
	return hex.EncodeToString(digest[:])
}

func (b *backgroundTasks) resultDirectory(session, taskKey string) string {
	if path := b.path(session); path != "" {
		return filepath.Join(filepath.Dir(path), "background-results", taskKey)
	}
	return ""
}

func (b *backgroundTasks) resultPath(session, taskKey, attemptKey string) string {
	if directory := b.resultDirectory(session, taskKey); directory != "" {
		return filepath.Join(directory, attemptKey+".json")
	}
	return ""
}

// Caller holds mutationMu and mu. Commit evidence before exposing a terminal
// receipt, releasing the state lock for encoding and storage.
func (b *backgroundTasks) commitResultLocked(e *backgroundEntry, result protocol.BackgroundResult, status string, executionErr error) (BackgroundTask, error) {
	if result.Review != nil {
		if err := protocol.ValidateHandoff(result.Review.Outcome, result.Review.NextAction, result.Review.Unfinished); err != nil {
			return BackgroundTask{}, err
		}
		result.Review = cloneTaskReview(result.Review)
		result.Text = "" // Render the authoritative structured data on read.
	}
	attempt := e.AttemptID
	if attempt == "" {
		return BackgroundTask{}, fmt.Errorf("background attempt was not admitted")
	}
	record := backgroundResultRecord{SessionID: e.SessionID, TaskID: e.ID, AttemptID: attempt, AttemptSeq: e.AttemptSeq, Status: status, Isolation: e.Isolation, Result: result}
	if executionErr != nil {
		record.Error = boundedBackgroundText(executionErr.Error())
	}
	b.mu.Unlock()
	digest, err := b.writeResult(record)
	b.mu.Lock()
	if err != nil {
		return BackgroundTask{}, err
	}
	next := e.BackgroundTask
	applyBackgroundResult(&next, record, digest)
	return next, nil
}

// Persistent mutations remain serialized by mutationMu. The memory-only store
// additionally takes mu because result readers can run during this write.
func (b *backgroundTasks) writeResult(record backgroundResultRecord) (string, error) {
	data, err := json.Marshal(record)
	if err != nil {
		return "", err
	}
	if len(data) > maxBackgroundResultBytes {
		return "", fmt.Errorf("background result exceeds %d bytes", maxBackgroundResultBytes)
	}
	taskKey, attemptKey := backgroundResultKey(record.TaskID), backgroundResultKey(record.AttemptID)
	if path := b.resultPath(record.SessionID, taskKey, attemptKey); path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return "", err
		}
		file, err := os.CreateTemp(filepath.Dir(path), ".result-*")
		if err != nil {
			return "", err
		}
		defer os.Remove(file.Name())
		if err = file.Chmod(0600); err == nil {
			_, err = file.Write(data)
		}
		if err == nil {
			err = file.Sync()
		}
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
		// Link publishes the finished record without overwriting an existing attempt.
		if err == nil {
			err = os.Link(file.Name(), path)
		}
		if errors.Is(err, os.ErrExist) {
			existing, readErr := os.ReadFile(path)
			if readErr != nil {
				return "", readErr
			}
			if !bytes.Equal(existing, data) {
				return "", fmt.Errorf("attempt result is already committed")
			}
			err = nil
		}
		if err != nil {
			return "", err
		}
	} else {
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.results == nil {
			b.results = map[string][]byte{}
		}
		key := record.SessionID + "/" + taskKey + "." + attemptKey
		if existing := b.results[key]; existing != nil && !bytes.Equal(existing, data) {
			return "", fmt.Errorf("attempt result is already committed")
		}
		b.results[key] = data
	}
	return backgroundResultKey(string(data)), nil
}

// This is a trusted assembly interface; parent is supplied by the Bot, never
// model arguments. Shared-result readers additionally restrict explicit refs.
func (h runHost) ReadBackgroundResult(ctx context.Context, parent string, q protocol.ResultQuery) (protocol.ResultPage, error) {
	return h.runtime.readBackgroundResult(ctx, parent, q)
}

func (r *Runtime) readBackgroundResult(ctx context.Context, session string, q protocol.ResultQuery) (protocol.ResultPage, error) {
	if err := ctx.Err(); err != nil {
		return protocol.ResultPage{}, err
	}
	if q.Offset < 0 || q.Limit < 1 || q.Limit > 2000 || q.Offset > 0 && q.Ref == "" {
		return protocol.ResultPage{}, fmt.Errorf("invalid result pagination")
	}
	b := r.background
	if err := b.load(session); err != nil {
		return protocol.ResultPage{}, err
	}
	// Only scheduling-state selection runs under b.mu. Disk I/O, decoding and
	// pagination must not block cancellation or other workers' progress callbacks.
	b.mu.Lock()
	ref := q.Ref
	if ref == "" {
		e := b.entries[q.ID]
		if e != nil && e.SessionID != session {
			b.mu.Unlock()
			return protocol.ResultPage{}, fmt.Errorf("task belongs to another session")
		}
		if e != nil && (q.AttemptID == "" || q.AttemptID == e.AttemptID) {
			if pendingBackgroundStatus(e.Status) {
				err := &protocol.ResultNotReadyError{Status: e.Status}
				b.mu.Unlock()
				return protocol.ResultPage{}, err
			}
			if e.ResultVersion > 0 && e.ResultRef == "" {
				err := fmt.Errorf("background result was not committed: %s", e.Error)
				b.mu.Unlock()
				return protocol.ResultPage{}, err
			}
			ref = e.ResultRef
		}
	}
	b.mu.Unlock()
	var taskKey, attemptKey, digest string
	if ref != "" {
		if !strings.HasPrefix(ref, backgroundResultRefPrefix) {
			return protocol.ResultPage{}, protocol.ErrBackgroundResultNotFound
		}
		parts := strings.Split(strings.TrimPrefix(ref, backgroundResultRefPrefix), ".")
		if len(parts) != 3 {
			return protocol.ResultPage{}, fmt.Errorf("invalid background result reference")
		}
		for _, part := range parts {
			decoded, err := hex.DecodeString(part)
			if err != nil || len(decoded) != sha256.Size {
				return protocol.ResultPage{}, fmt.Errorf("invalid background result reference")
			}
		}
		taskKey, attemptKey, digest = parts[0], parts[1], parts[2]
	} else {
		// Exact historical attempts remain addressable after cursor eviction or
		// scheduler history pruning, without any child-session fallback.
		if q.ID == "" || q.AttemptID == "" {
			return protocol.ResultPage{}, protocol.ErrBackgroundResultNotFound
		}
		taskKey, attemptKey = backgroundResultKey(q.ID), backgroundResultKey(q.AttemptID)
	}
	var data []byte
	if path := b.resultPath(session, taskKey, attemptKey); path != "" {
		var err error
		data, err = readBackgroundResultFile(path)
		if err != nil {
			if ref == "" && errors.Is(err, os.ErrNotExist) {
				return protocol.ResultPage{}, protocol.ErrBackgroundResultNotFound
			}
			return protocol.ResultPage{}, err
		}
	} else {
		b.mu.Lock()
		data = b.results[session+"/"+taskKey+"."+attemptKey]
		b.mu.Unlock()
		if data == nil && ref == "" {
			return protocol.ResultPage{}, protocol.ErrBackgroundResultNotFound
		}
	}
	record, actualDigest, err := decodeBackgroundResult(data, session, taskKey, attemptKey, digest)
	if err != nil {
		return protocol.ResultPage{}, err
	}
	if q.ID != "" && q.ID != record.TaskID || q.AttemptID != "" && q.AttemptID != record.AttemptID {
		return protocol.ResultPage{}, fmt.Errorf("result reference does not match this parent/task/attempt")
	}
	if err := ctx.Err(); err != nil {
		return protocol.ResultPage{}, err
	}
	text := []rune(backgroundResultText(record.Result))
	if q.Offset > len(text) {
		return protocol.ResultPage{}, fmt.Errorf("result offset exceeds total")
	}
	end := q.Offset + min(q.Limit, len(text)-q.Offset)
	ref = backgroundResultRefPrefix + taskKey + "." + attemptKey + "." + actualDigest
	page := protocol.ResultPage{ID: record.TaskID, AttemptID: record.AttemptID, Ref: ref, Status: record.Status, Content: string(text[q.Offset:end]), Offset: q.Offset, NextOffset: end, Total: len(text), HasMore: end < len(text), Isolation: record.Isolation}
	if review := record.Result.Review; review != nil {
		page.WorkspaceBase, page.SourceHead, page.WorkspaceKind = review.WorkspaceBase, review.SourceHead, review.WorkspaceKind
	}
	return page, nil
}

// Both ordinary reads and recovery use the same bounded record validation.
func readBackgroundResultFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, maxBackgroundResultBytes+1))
}

func decodeBackgroundResult(data []byte, session, taskKey, attemptKey, expectedDigest string) (backgroundResultRecord, string, error) {
	var record backgroundResultRecord
	if len(data) > maxBackgroundResultBytes {
		return record, "", fmt.Errorf("background result exceeds size limit")
	}
	digest := backgroundResultKey(string(data))
	if expectedDigest != "" && expectedDigest != digest {
		return record, "", fmt.Errorf("background result content changed")
	}
	if err := json.Unmarshal(data, &record); err != nil {
		return record, "", err
	}
	if record.TaskID == "" || record.AttemptID == "" || record.SessionID != session || backgroundResultKey(record.TaskID) != taskKey || backgroundResultKey(record.AttemptID) != attemptKey {
		return record, "", fmt.Errorf("result record does not match this parent/task/attempt")
	}
	switch record.Status {
	case "completed", "partial", "failed", "interrupted":
	default:
		return record, "", fmt.Errorf("result record is not terminal")
	}
	if review := record.Result.Review; review != nil {
		if err := protocol.ValidateHandoff(review.Outcome, review.NextAction, review.Unfinished); err != nil {
			return record, "", err
		}
		if record.Status == "completed" && protocol.HandoffIncomplete(review.Outcome, review.Unfinished) {
			return record, "", fmt.Errorf("completed result contains unfinished work")
		}
	}
	return record, digest, nil
}

// The mutable delivery projection is derived identically at commit and restart.
func applyBackgroundResult(task *BackgroundTask, record backgroundResultRecord, digest string) {
	task.ResultVersion = 1
	task.AttemptID = record.AttemptID
	if record.AttemptSeq > 0 {
		task.AttemptSeq = record.AttemptSeq
	}
	task.Status, task.Error, task.Isolation = record.Status, record.Error, record.Isolation
	task.ResultRef = backgroundResultRefPrefix + backgroundResultKey(record.TaskID) + "." + backgroundResultKey(record.AttemptID) + "." + digest
	task.Review, task.ReviewComplete = nil, false
	if review := record.Result.Review; review != nil {
		task.Review, task.ReviewComplete = boundedTaskReview(*review)
		task.Result, task.ResultTruncated = backgroundTextPrefix(review.Summary, 1024), true
	} else {
		task.Result = boundedBackgroundText(record.Result.Text)
		task.ResultTruncated = len(record.Result.Text) > backgroundResultLimit
	}
}

// Only the recorded attempt may repair an unfinished receipt;
// another attempt's report can never certify completion or restart execution.
func (b *backgroundTasks) recoverResult(task *BackgroundTask) error {
	if task.ResultVersion == 0 || task.AttemptID == "" || !pendingBackgroundStatus(task.Status) && task.ResultRef != "" {
		return nil
	}
	taskKey, attemptKey := backgroundResultKey(task.ID), backgroundResultKey(task.AttemptID)
	path := b.resultPath(task.SessionID, taskKey, attemptKey)
	if path == "" {
		return nil
	}
	data, err := readBackgroundResultFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	record, digest, err := decodeBackgroundResult(data, task.SessionID, taskKey, attemptKey, "")
	if err != nil {
		return err
	}
	applyBackgroundResult(task, record, digest)
	return nil
}

// Caller holds mutationMu and mu. Explicit deletion removes all attempts, while scheduler
// history eviction leaves records intact so issued evidence refs keep working.
func (b *backgroundTasks) deleteResultsLocked(session, task string) error {
	key := backgroundResultKey(task)
	b.mu.Unlock()
	directory := b.resultDirectory(session, key)
	var err error
	if directory != "" {
		err = os.RemoveAll(directory)
	}
	b.mu.Lock()
	if directory != "" {
		return err
	}
	for ref := range b.results {
		if strings.HasPrefix(ref, session+"/"+key+".") {
			delete(b.results, ref)
		}
	}
	return nil
}

var _ protocol.BackgroundResultReader = runHost{}
