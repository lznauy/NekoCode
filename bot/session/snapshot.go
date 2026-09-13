package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/contextmgr/token"
	"nekocode/bot/policy/ledger"
	"nekocode/bot/provider/types"
	"nekocode/util/fs"
)

type Snapshot struct {
	FormatVersion int    `json:"format_version"`
	ID            string `json:"id"`
	CWD           string `json:"cwd"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`

	SystemPrompt    string          `json:"system_prompt"`
	Skills          string          `json:"skills"`
	Memory          string          `json:"memory"`
	Archive         string          `json:"archive"`
	Messages        []types.Message `json:"messages"`
	Transcript      []types.Message `json:"-"`
	TranscriptSeq   int             `json:"transcript_seq,omitempty"`
	CompactBoundary int             `json:"compact_boundary"`

	ContextWindow     int `json:"context_window"`
	PromptTokens      int `json:"prompt_tokens"`
	CompletionTokens  int `json:"completion_tokens"`
	TrackerPrompt     int `json:"tracker_prompt_tokens,omitempty"`
	TrackerCompletion int `json:"tracker_completion_tokens,omitempty"`
	TrackerNewTokens  int `json:"tracker_new_tokens,omitempty"`
	CacheHitTokens    int `json:"cache_hit_tokens,omitempty"`
	CacheMissTokens   int `json:"cache_miss_tokens,omitempty"`
	SubCount          int `json:"sub_count,omitempty"`
	SubTokens         int `json:"sub_tokens,omitempty"`
	SubCacheHit       int `json:"sub_cache_hit,omitempty"`
	SubCacheMiss      int `json:"sub_cache_miss,omitempty"`

	LoadedSkills    []string        `json:"loaded_skills"`
	CheckpointTurns []string        `json:"checkpoint_turns,omitempty"`
	CheckpointNext  int             `json:"checkpoint_next,omitempty"`
	Ledger          ledger.Snapshot `json:"ledger"`

	// Last request's cache-relevant head (Layer 0-1 and tools). Restoring it
	// lets the next process attribute its first cache miss to a real prefix
	// change instead of reporting cold-start.
	PrefixSystemHash string `json:"prefix_system_hash,omitempty"`
	PrefixToolsHash  string `json:"prefix_tools_hash,omitempty"`

	// PrefixTurn is the last turn's cache summary (one slot, no history), kept
	// so /context still shows the previous turn right after a reload.
	PrefixTurn *ctxmgr.PrefixTurnStats `json:"prefix_turn,omitempty"`

	// Archive bookkeeping: how many compactions produced the restored archive
	// and how many messages they trimmed. Cumulative, like the cache counters.
	CompactCount int `json:"compact_count,omitempty"`
	TrimCount    int `json:"trim_count,omitempty"`
}

type Meta struct {
	ID        string `json:"id"`
	CWD       string `json:"cwd"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
	MsgCount  int    `json:"msg_count"`
}

func dir() string {
	return filepath.Join(fs.NekocodeHome(), "sessions")
}

func newSnapshot(cwd string) *Snapshot {
	now := time.Now()
	return &Snapshot{
		FormatVersion: 2,
		ID:            fmt.Sprintf("%s-%09d", now.UTC().Format("20060102T150405"), now.Nanosecond()),
		CWD:           cwd,
		CreatedAt:     now.Unix(),
		UpdatedAt:     now.Unix(),
	}
}

func load(id string) (*Snapshot, error) {
	if err := validateID(id); err != nil {
		return nil, err
	}
	path := filepath.Join(dir(), id, "session.json")
	snapshot, err := fs.ReadJSONFile[*Snapshot](path)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	if snapshot == nil || snapshot.ID != id {
		found := ""
		if snapshot != nil {
			found = snapshot.ID
		}
		return nil, fmt.Errorf("session id mismatch: requested %q, file contains %q", id, found)
	}
	if snapshot.FormatVersion < 2 {
		return nil, fmt.Errorf("legacy session format (v%d); run `go run ./util/session-migrate --session %s` to convert it", snapshot.FormatVersion, id)
	}
	persistedSeq := snapshot.TranscriptSeq
	if persistedSeq < 0 {
		return nil, fmt.Errorf("invalid negative transcript watermark %d", persistedSeq)
	}
	transcript, err := loadTranscript(filepath.Dir(path), persistedSeq)
	if err != nil {
		return nil, fmt.Errorf("load transcript: %w", err)
	}
	if transcript == nil && persistedSeq > 0 {
		return nil, fmt.Errorf("transcript.jsonl is missing while the session records %d messages; restore it from a backup or re-run session-migrate", persistedSeq)
	}
	if persistedSeq > len(transcript) {
		return nil, fmt.Errorf("transcript watermark %d exceeds %d records", persistedSeq, len(transcript))
	}
	if persistedSeq < len(transcript) {
		snapshot.Messages = append(snapshot.Messages, transcript[persistedSeq:]...)
	}
	snapshot.Transcript = transcript
	snapshot.TranscriptSeq = len(transcript)
	return snapshot, nil
}

// Delete removes a session directory and all its contents.
func deleteSnapshot(id string) error {
	if err := validateID(id); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(dir(), id))
}

func validateID(id string) error {
	if strings.TrimSpace(id) == "" || id == "." || id == ".." ||
		filepath.Base(id) != id || strings.ContainsAny(id, `/\`) {
		return fmt.Errorf("invalid session id: %q", id)
	}
	return nil
}

func (s *Snapshot) save() error {
	if s == nil {
		return fmt.Errorf("cannot save nil session")
	}
	if err := validateID(s.ID); err != nil {
		return err
	}
	s.UpdatedAt = time.Now().Unix()
	d := filepath.Join(dir(), s.ID)
	watermark, err := appendTranscript(d, s.TranscriptSeq, s.Transcript)
	s.TranscriptSeq = watermark
	if err != nil {
		return fmt.Errorf("persist transcript: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	return writeAtomic(filepath.Join(d, "session.json"), data, 0o600)
}

// sessionMeta is a lightweight struct for deserializing only metadata from
// session.json, avoiding the cost of unmarshaling the full Messages array.
type sessionMeta struct {
	ID            string     `json:"id"`
	CWD           string     `json:"cwd"`
	CreatedAt     int64      `json:"created_at"`
	UpdatedAt     int64      `json:"updated_at"`
	Messages      []struct{} `json:"messages"` // only need len, not content
	TranscriptSeq int        `json:"transcript_seq,omitempty"`
}

func loadMeta(id string) (Meta, error) {
	var sm sessionMeta
	path := filepath.Join(dir(), id, "session.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return Meta{}, err
	}
	_ = os.Chmod(path, 0o600)
	if err := json.Unmarshal(data, &sm); err != nil {
		return Meta{}, err
	}
	return Meta{
		ID: sm.ID, CWD: sm.CWD,
		CreatedAt: sm.CreatedAt, UpdatedAt: sm.UpdatedAt,
		MsgCount: max(len(sm.Messages), sm.TranscriptSeq),
	}, nil
}

func list() []Meta {
	entries, err := os.ReadDir(dir())
	if err != nil {
		return nil
	}
	var out []Meta
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		m, err := loadMeta(e.Name())
		if err != nil {
			continue
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out
}

// Age reports how long ago the session was created, so listings always
// answer "when did this session start" regardless of recent activity.
func (m Meta) Age() string {
	d := time.Since(time.Unix(m.CreatedAt, 0))
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// CaptureContext stores context, usage, and loaded-skill state in the session.
func (s *Snapshot) CaptureContext(snap ctxmgr.ManagerSnapshot, promptTokens, completionTokens int, loaded map[string]bool) {
	if s == nil {
		return
	}
	s.SystemPrompt = snap.SystemPrompt
	s.Skills = snap.Skills
	s.Memory = snap.Memory
	s.Archive = snap.Archive
	s.Messages = snap.Messages
	s.Transcript = snap.Transcript
	s.CompactBoundary = 0
	s.ContextWindow = snap.Budget
	s.PromptTokens = promptTokens
	s.CompletionTokens = completionTokens
	s.TrackerPrompt = snap.Tracker.LastPromptTokens
	// Kept in the JSON struct for backward-compatible reads only. Completion
	// usage is owned by the agent-level PromptTokens/CompletionTokens fields.
	s.TrackerCompletion = 0
	s.TrackerNewTokens = snap.Tracker.NewMessageTokens
	s.CacheHitTokens = snap.Tracker.CacheHitTokens
	s.CacheMissTokens = snap.Tracker.CacheMissTokens
	s.SubCount = snap.Tracker.Sub.Count
	s.SubTokens = snap.Tracker.Sub.TotalTokens
	s.SubCacheHit = snap.Tracker.Sub.CacheHitTokens
	s.SubCacheMiss = snap.Tracker.Sub.CacheMissTokens
	s.PrefixSystemHash = snap.PrefixBaseline.System
	s.PrefixToolsHash = snap.PrefixBaseline.Tools
	s.PrefixTurn = snap.PrefixTurn
	s.CompactCount = snap.CompactCount
	s.TrimCount = snap.TrimCount
	s.LoadedSkills = loadedSkillNames(loaded)
}

// ContextSnapshot restores the context-manager state stored in the session.
func (s *Snapshot) ContextSnapshot() ctxmgr.ManagerSnapshot {
	if s == nil {
		return ctxmgr.ManagerSnapshot{}
	}
	messages := s.Messages
	if s.CompactBoundary > 0 && s.CompactBoundary <= len(messages) {
		messages = messages[s.CompactBoundary:]
	}
	return ctxmgr.ManagerSnapshot{
		SystemPrompt: s.SystemPrompt,
		Skills:       s.Skills,
		Archive:      s.Archive,
		Memory:       s.Memory,
		Messages:     append([]types.Message(nil), messages...),
		Transcript:   append([]types.Message(nil), s.Transcript...),
		Budget:       s.ContextWindow,
		Tracker: token.State{
			LastPromptTokens: s.TrackerPrompt,
			NewMessageTokens: s.TrackerNewTokens,
			CacheHitTokens:   s.CacheHitTokens,
			CacheMissTokens:  s.CacheMissTokens,
			Sub: token.SubStats{
				Count:           s.SubCount,
				TotalTokens:     s.SubTokens,
				CacheHitTokens:  s.SubCacheHit,
				CacheMissTokens: s.SubCacheMiss,
			},
		},
		PrefixBaseline: ctxmgr.PrefixBaseline{System: s.PrefixSystemHash, Tools: s.PrefixToolsHash},
		PrefixTurn:     s.PrefixTurn,
		CompactCount:   s.CompactCount,
		TrimCount:      s.TrimCount,
	}
}

func loadedSkillNames(loaded map[string]bool) []string {
	names := make([]string, 0, len(loaded))
	for name, ok := range loaded {
		if ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}
