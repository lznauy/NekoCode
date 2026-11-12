package session

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The parent lock serializes opening and deleting task directories. Active
// execution retains its per-task OS lock after this short transaction ends.
// openTaskManagementRoot anchors destructive operations to a parent session.
// Root methods prevent paths swapped concurrently to escaping symlinks from
// reaching files outside the session; explicit directory checks also reject
// aliases into other task namespaces inside the session.
func openTaskManagementRoot(root string) (*os.Root, error) {
	rel, err := filepath.Rel(dir(), root)
	if err != nil || filepath.Dir(rel) == "." || filepath.Base(rel) != "subagents" || validateID(filepath.Dir(rel)) != nil {
		return nil, fmt.Errorf("invalid task management root")
	}
	if err := os.MkdirAll(dir(), 0700); err != nil {
		return nil, err
	}
	sessions, err := os.OpenRoot(dir())
	if err != nil {
		return nil, err
	}
	defer sessions.Close()
	parent := filepath.Dir(rel)
	if err := taskManagementDirectory(sessions, parent, true); err != nil {
		return nil, err
	}
	sessionRoot, err := sessions.OpenRoot(parent)
	if err != nil {
		return nil, err
	}
	if err := taskManagementDirectory(sessionRoot, "subagents", true); err != nil {
		sessionRoot.Close()
		return nil, err
	}
	return sessionRoot, nil
}

func taskManagementDirectory(root *os.Root, path string, create bool) error {
	var current string
	for _, part := range strings.Split(filepath.Clean(path), string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := root.Lstat(current)
		if os.IsNotExist(err) {
			if !create {
				return nil
			}
			if err = root.Mkdir(current, 0700); err != nil && !os.IsExist(err) {
				return err
			}
			info, err = root.Lstat(current)
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("task directory %s is not a real directory", current)
		}
	}
	return nil
}

func lockSubagentManagement(root string) (*os.File, error) {
	sessionRoot, err := openTaskManagementRoot(root)
	if err != nil {
		return nil, err
	}
	defer sessionRoot.Close()
	path := filepath.Join("subagents", ".management.lock")
	// Exclusive creation never follows a pre-existing symlink. An existing lock
	// is opened without truncation and verified against its regular-file identity.
	f, err := openTaskLockFile(sessionRoot, path)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(time.Second)
	for {
		ok, err := trySubagentLock(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		if ok {
			return f, nil
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil, fmt.Errorf("task management busy; retry")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// openTaskLockFile refuses aliases and verifies the identity of existing locks.
func openTaskLockFile(root *os.Root, path string) (*os.File, error) {
	f, err := root.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if os.IsExist(err) {
		info, statErr := root.Lstat(path)
		if statErr != nil {
			return nil, statErr
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("task lock is not a regular file")
		}
		f, err = root.OpenFile(path, os.O_RDWR, 0600)
		if err == nil {
			opened, statErr := f.Stat()
			if statErr != nil || !os.SameFile(info, opened) {
				f.Close()
				return nil, fmt.Errorf("task lock changed while opening")
			}
		}
	}
	if err != nil {
		return nil, err
	}
	return f, nil
}

// DeleteSubagent only removes the task's own directory and output namespace.
// Repeating deletion safely finishes cleanup after a partial filesystem failure.
func DeleteSubagent(parentID, id string) error {
	root, err := subagentRoot(parentID)
	if err != nil {
		return err
	}
	if err := validateID(id); err != nil {
		return err
	}
	guard, err := lockSubagentManagement(root)
	if err != nil {
		return err
	}
	defer guard.Close()

	sessionRoot, err := openTaskManagementRoot(root)
	if err != nil {
		return err
	}
	defer sessionRoot.Close()
	d := filepath.Join("subagents", id)
	for _, path := range []string{d, "tool-results", filepath.Join("tool-results", "subagents"), filepath.Join("subagents", ".deleted")} {
		if err := taskManagementDirectory(sessionRoot, path, false); err != nil {
			return err
		}
	}
	f, err := sessionRoot.OpenFile(filepath.Join(d, ".active.lock"), os.O_RDWR, 0600)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if f != nil {
		defer f.Close()
		ok, err := trySubagentLock(f)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("task is executing; cancel and wait before deletion")
		}
	}
	deleted := filepath.Join("subagents", ".deleted")
	if err := taskManagementDirectory(sessionRoot, deleted, true); err != nil {
		return err
	}
	tombstone := filepath.Join(deleted, id)
	marker, err := sessionRoot.OpenFile(tombstone, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if os.IsExist(err) {
		info, statErr := sessionRoot.Lstat(tombstone)
		if statErr != nil {
			return statErr
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("deleted-task marker is not a regular file")
		}
	} else if err != nil {
		return err
	} else if err := marker.Close(); err != nil {
		return err
	}
	if err := sessionRoot.RemoveAll(filepath.Join("tool-results", "subagents", id)); err != nil {
		return fmt.Errorf("remove task outputs: %w", err)
	}
	if err := sessionRoot.RemoveAll(d); err != nil {
		return fmt.Errorf("remove task history: %w", err)
	}

	return nil
}

type SubagentSearchHit struct {
	Sequence int    `json:"sequence"`
	Preview  string `json:"preview"`
}
type SubagentSearchPage struct {
	Hits    []SubagentSearchHit `json:"hits"`
	Next    int                 `json:"next"`
	HasMore bool                `json:"has_more"`
	Total   int                 `json:"total"`
}

// WalkSubagentOutputs streams only the committed prefix. The callback receives
// original output pages, so large artifacts never accumulate in process memory.
func walkSubagentOutputs(ctx context.Context, parentID, id string, after int, visit func(int, string) (bool, error)) (int, error) {
	root, err := subagentRoot(parentID)
	if err != nil {
		return 0, err
	}
	if err := validateID(id); err != nil {
		return 0, err
	}
	info, err := readSubagentInfo(root, parentID, id)
	if err != nil {
		return 0, err
	}
	f, err := os.Open(filepath.Join(root, id, transcriptFileName))
	if err != nil {
		return 0, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64<<10), 16<<20)
	seq := 0
	for seq < info.TranscriptSeq && scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return info.TranscriptSeq, err
		}
		var record transcriptRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil || record.Seq != seq+1 {
			return info.TranscriptSeq, fmt.Errorf("invalid committed task record")
		}
		seq++
		if seq <= after || record.Message.Role != "tool" {
			continue
		}
		if record.Message.ToolOutput.Path == "" {
			stop, err := visit(seq, record.Message.Content)
			if err != nil || stop {
				return info.TranscriptSeq, err
			}
			continue
		}
		for offset := int64(0); ; {
			if err := ctx.Err(); err != nil {
				return info.TranscriptSeq, err
			}
			page, err := SubagentToolOutput(parentID, id, seq, offset)
			if err != nil {
				return info.TranscriptSeq, fmt.Errorf("output %d unavailable: %w", seq, err)
			}
			stop, err := visit(seq, page.Content)
			if err != nil || stop {
				return info.TranscriptSeq, err
			}
			if !page.HasMore {
				break
			}
			offset = page.NextOffset
		}
	}
	if err := scanner.Err(); err != nil {
		return info.TranscriptSeq, err
	}
	if seq < info.TranscriptSeq {
		return info.TranscriptSeq, fmt.Errorf("committed task output is incomplete")
	}
	return info.TranscriptSeq, nil
}

func SearchSubagentOutputs(ctx context.Context, parentID, id, query string, after, limit int) (SubagentSearchPage, error) {
	if strings.TrimSpace(query) == "" || len(query) > 256 || after < 0 || limit < 1 || limit > 100 {
		return SubagentSearchPage{}, fmt.Errorf("invalid search query or cursor")
	}
	out := SubagentSearchPage{Hits: []SubagentSearchHit{}, Next: after}
	lastHit := 0
	tail := ""
	current := 0
	total, err := walkSubagentOutputs(ctx, parentID, id, after, func(seq int, text string) (bool, error) {
		if current != seq {
			tail = ""
			current = seq
		}
		combined := tail + text
		index := strings.Index(combined, query)
		if index >= 0 && lastHit != seq {
			out.Hits = append(out.Hits, SubagentSearchHit{Sequence: seq, Preview: string([]rune(combined[max(0, index-80):min(len(combined), index+len(query)+200)]))})
			lastHit = seq
			if len(out.Hits) >= limit {
				out.Next = seq
				out.HasMore = true
				return true, nil
			}
		}
		tail = combined[max(0, len(combined)-len(query)+1):]
		out.Next = seq
		return false, nil
	})
	if err != nil {
		return SubagentSearchPage{}, err
	}
	out.Total = total
	if out.Next >= total {
		out.HasMore = false
	}
	if err == nil && !out.HasMore {
		out.Next = total
	}
	return out, err
}

func ExportSubagentOutputs(ctx context.Context, parentID, id string, w io.Writer) error {
	previous := 0
	_, err := walkSubagentOutputs(ctx, parentID, id, 0, func(seq int, text string) (bool, error) {
		if seq != previous {
			if _, err := fmt.Fprintf(w, "\n[Tool output %d]\n", seq); err != nil {
				return true, err
			}
			previous = seq
		}
		_, err := io.WriteString(w, text)
		return err != nil, err
	})
	return err
}

func SubagentDeleted(parentID, id string) bool {
	root, err := subagentRoot(parentID)
	if err != nil || validateID(id) != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(root, ".deleted", id))
	return err == nil
}
