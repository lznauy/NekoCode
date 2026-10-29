package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	ctxmgr "nekocode/bot/contextmgr"
)

type SubagentAttemptUsage struct {
	TotalTokens     int   `json:"total_tokens,omitempty"`
	CacheHitTokens  int   `json:"cache_hit_tokens,omitempty"`
	CacheMissTokens int   `json:"cache_miss_tokens,omitempty"`
	ToolUseCount    int   `json:"tool_use_count,omitempty"`
	DurationMs      int64 `json:"duration_ms,omitempty"`
	ContextTokens   int   `json:"context_tokens,omitempty"`
	ContextWindow   int   `json:"context_window,omitempty"`
}

// SubagentAttempt is one admitted execution of a durable task. TranscriptStart
// and TranscriptEnd are inclusive sequence numbers; an empty range has start=end+1.
type SubagentAttempt struct {
	ID              string `json:"id"`
	Seq             int    `json:"seq"`
	Prompt          string `json:"prompt"`
	Status          string `json:"status"`
	Result          string `json:"result,omitempty"`
	StartedAt       int64  `json:"started_at"`
	EndedAt         int64  `json:"ended_at,omitempty"`
	TranscriptStart int    `json:"transcript_start"`
	TranscriptEnd   int    `json:"transcript_end"`
	SubagentAttemptUsage
}

// SubagentMetadata belongs to a durable task, rather than an execution slot.
type SubagentMetadata struct {
	Isolation        string            `json:"isolation,omitempty"`
	ParentSessionID  string            `json:"parent_session_id"`
	Title            string            `json:"title,omitempty"`
	Profile          string            `json:"profile"`
	Skills           []string          `json:"skills,omitempty"`
	Prompt           string            `json:"prompt"`
	Status           string            `json:"status"`
	RunCount         int               `json:"run_count"`
	Result           string            `json:"result,omitempty"`
	CurrentAttemptID string            `json:"current_attempt_id,omitempty"`
	Attempts         []SubagentAttempt `json:"attempts,omitempty"`
}

func (m *SubagentMetadata) CurrentAttempt() *SubagentAttempt {
	if m == nil || m.CurrentAttemptID == "" {
		return nil
	}
	for i := len(m.Attempts) - 1; i >= 0; i-- {
		if m.Attempts[i].ID == m.CurrentAttemptID {
			return &m.Attempts[i]
		}
	}
	return nil
}

// SubagentSession holds an exclusive OS lock for the duration of an execution.
// A crashed process releases the lock; its persisted running status is then
// interpreted as interrupted, never as permission to replay a tool call.
type SubagentSession struct {
	root     string
	lock     *os.File
	snapshot *Snapshot
}

func subagentRoot(parentID string) (string, error) {
	if err := validateID(parentID); err != nil {
		return "", err
	}
	return filepath.Join(dir(), parentID, "subagents"), nil
}

// OpenSubagent acquires the task before loading or repairing its transcript.
// resume=false refuses to overwrite an existing task.
func OpenSubagent(parentID, id, cwd string, resume bool) (*SubagentSession, error) {
	root, err := subagentRoot(parentID)
	if err != nil {
		return nil, err
	}
	if err := validateID(id); err != nil {
		return nil, err
	}
	d := filepath.Join(root, id)
	if err := os.MkdirAll(d, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(d, ".active.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	// Observation probes briefly acquire this lock to detect orphaned tasks.
	// Give those probes time to release without admitting a second executor.
	deadline := time.Now().Add(100 * time.Millisecond)
	ok, err := trySubagentLock(f)
	for err == nil && !ok && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		ok, err = trySubagentLock(f)
	}
	if err != nil || !ok {
		_ = f.Close()
		if err == nil {
			err = fmt.Errorf("subagent %s is already running", id)
		}
		return nil, err
	}
	fail := func(err error) (*SubagentSession, error) { _ = f.Close(); return nil, err }
	var snapshot *Snapshot
	if resume {
		if err := validateSubagentIdentity(filepath.Join(d, "session.json"), parentID, id, cwd); err != nil {
			return fail(err)
		}
		snapshot, err = loadModeAt(root, id, true)
		if err != nil {
			return fail(err)
		}
		meta := snapshot.Subagent
		if meta.Status == "running" {
			meta.Status = "interrupted"
			if attempt := meta.CurrentAttempt(); attempt != nil && attempt.Status == "running" {
				attempt.Status = "interrupted"
				attempt.EndedAt = time.Now().Unix()
				// loadModeAt exposes a valid uncommitted tail for recovery. Keep it
				// in the interrupted attempt instead of dropping tool-call evidence.
				attempt.TranscriptEnd = snapshot.TranscriptSeq
			}
		}
		if snapshot.TranscriptSeq > snapshot.CommittedTranscriptSeq && (meta.CurrentAttempt() == nil || meta.CurrentAttempt().Status != "interrupted" || meta.CurrentAttempt().TranscriptEnd != snapshot.TranscriptSeq) {
			// A first checkpoint can append the next attempt's transcript before
			// its metadata commits. The old task status is then already terminal.
			// Preserve that tail as its own recovered attempt instead of leaving
			// it between the previous and next attempt ranges.
			now := time.Now().Unix()
			meta.RunCount++
			attempt := SubagentAttempt{
				ID: uuid.NewString(), Seq: meta.RunCount, Status: "interrupted",
				StartedAt: now, EndedAt: now, TranscriptStart: snapshot.CommittedTranscriptSeq + 1,
				TranscriptEnd: snapshot.TranscriptSeq,
			}
			meta.Attempts = append(meta.Attempts, attempt)
			meta.CurrentAttemptID = attempt.ID
			meta.Status, meta.Result = "interrupted", ""
		}
	} else {
		if _, err := os.Stat(filepath.Join(d, "session.json")); !os.IsNotExist(err) {
			if err == nil {
				err = fmt.Errorf("subagent %s already exists", id)
			}
			return fail(err)
		}
		snapshot = newSnapshot(cwd)
		snapshot.ID = id
		snapshot.Subagent = &SubagentMetadata{ParentSessionID: parentID, Status: "created"}
	}
	return &SubagentSession{root: root, lock: f, snapshot: snapshot}, nil
}

// SetIsolation is immutable once the first attempt is recorded.
func (s *SubagentSession) SetIsolation(mode string) error {
	if mode != "shared" && mode != "worktree" {
		return fmt.Errorf("invalid isolation mode")
	}
	saved := s.snapshot.Subagent.Isolation
	if saved == "" {
		saved = "shared"
	}
	if s.snapshot.Subagent.RunCount > 0 && saved != mode {
		return fmt.Errorf("cannot change task isolation on resume")
	}
	s.snapshot.Subagent.Isolation = mode
	return nil
}

func (s *SubagentSession) Snapshot() *Snapshot { return s.snapshot.Clone() }

func (s *SubagentSession) Begin(profile, prompt string, skills []string, title ...string) error {
	meta := s.snapshot.Subagent
	if meta.RunCount > 0 && meta.Profile != profile {
		return fmt.Errorf("cannot change subagent profile on resume")
	}
	meta.Profile, meta.Status, meta.Result = profile, "running", ""
	meta.Skills = append([]string(nil), skills...)
	if meta.RunCount == 0 {
		meta.Prompt = prompt
		if len(title) > 0 {
			meta.Title = title[0]
		}
	}
	meta.RunCount++
	attempt := SubagentAttempt{
		ID: uuid.NewString(), Seq: meta.RunCount, Prompt: prompt, Status: "running",
		StartedAt: time.Now().Unix(), TranscriptStart: s.snapshot.TranscriptSeq + 1,
		TranscriptEnd: s.snapshot.TranscriptSeq,
	}
	meta.Attempts = append(meta.Attempts, attempt)
	meta.CurrentAttemptID = attempt.ID
	return nil
}

// Checkpoint commits transcript and context using the same watermark protocol
// as parent sessions. Callers acknowledge the transcript only after success.
func (s *SubagentSession) Checkpoint(context ctxmgr.ManagerSnapshot, status, result string, usage ...SubagentAttemptUsage) error {
	if s.lock == nil {
		return fmt.Errorf("subagent session is closed")
	}
	meta := s.snapshot.Subagent
	attempt := meta.CurrentAttempt()
	if attempt == nil {
		return fmt.Errorf("subagent attempt was not initialized")
	}
	s.snapshot.CaptureContext(context, 0, 0, nil)
	s.snapshot.TranscriptLazy = true
	meta.Status, meta.Result = status, result
	attempt.Status, attempt.Result = status, result
	if status == "running" {
		attempt.EndedAt = 0
	} else if attempt.EndedAt == 0 {
		attempt.EndedAt = time.Now().Unix()
	}
	if len(usage) > 0 {
		attempt.SubagentAttemptUsage = usage[0]
	}
	attempt.TranscriptEnd = s.snapshot.TranscriptBase + len(s.snapshot.Transcript)
	if err := s.snapshot.saveAt(s.root); err != nil {
		return err
	}
	s.writeProjection()
	return nil
}

func (s *SubagentSession) Close() error {
	if s.lock == nil {
		return nil
	}
	f := s.lock
	s.lock = nil
	return f.Close()
}

// ReadSubagentMetadata does not load or repair the transcript and is safe for
// observers while another process owns the execution lock.
func ReadSubagentMetadata(parentID, id string) (SubagentInfo, error) {
	root, err := subagentRoot(parentID)
	if err != nil {
		return SubagentInfo{}, err
	}
	if err := validateID(id); err != nil {
		return SubagentInfo{}, err
	}
	return readSubagentInfo(root, parentID, id)
}

func validateSubagentIdentity(path, parentID, id, cwd string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var header struct {
		ID       string            `json:"id"`
		CWD      string            `json:"cwd"`
		Subagent *SubagentMetadata `json:"subagent"`
	}
	if err := json.NewDecoder(f).Decode(&header); err != nil {
		return err
	}
	if header.ID != id || header.Subagent == nil || header.Subagent.ParentSessionID != parentID || header.CWD != cwd {
		return fmt.Errorf("subagent identity or workspace mismatch")
	}
	return nil
}
