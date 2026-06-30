package session

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/contextmgr/token"
	"nekocode/bot/llm/types"
	"nekocode/common"
)

// ContextStore is the context-manager surface required by session persistence.
type ContextStore interface {
	Snapshot() ctxmgr.ManagerSnapshot
	Restore(ctxmgr.ManagerSnapshot)
	Build(includeSystem bool) []types.Message
	Clear()
}

// Manager owns the current session lifecycle and hides session snapshot wiring
// from app-level orchestration.
type Manager struct {
	mu   sync.Mutex
	cwd  string
	sess *Snapshot
	ctx  ContextStore

	tokenUsage      func() (int, int)
	addTokens       func(prompt, completion int)
	loadedSkills    func() map[string]bool
	markSkillLoaded func(string)
}

type ManagerOptions struct {
	CWD             string
	Context         ContextStore
	TokenUsage      func() (prompt, completion int)
	AddTokens       func(prompt, completion int)
	LoadedSkills    func() map[string]bool
	MarkSkillLoaded func(string)
}

// DefaultExportPath is the default context-export destination under ~/.nekocode/exports.
var DefaultExportPath = filepath.Join(common.NekocodeDataDir("exports"), "nekocode-context.json")

func NewManager(opts ManagerOptions) *Manager {
	return &Manager{
		cwd:             opts.CWD,
		ctx:             opts.Context,
		tokenUsage:      opts.TokenUsage,
		addTokens:       opts.AddTokens,
		loadedSkills:    opts.LoadedSkills,
		markSkillLoaded: opts.MarkSkillLoaded,
	}
}

func (m *Manager) Init() error {
	sess, err := New(m.cwd)
	if err != nil {
		return err
	}
	m.Set(sess)
	return nil
}

func (m *Manager) CWD() string {
	return m.cwd
}

func (m *Manager) Current() *Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sess
}

func (m *Manager) CurrentID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sess == nil {
		return ""
	}
	return m.sess.ID
}

func (m *Manager) Set(sess *Snapshot) {
	m.mu.Lock()
	m.sess = sess
	m.mu.Unlock()
}

func (m *Manager) ClearContext() {
	if m.ctx != nil {
		m.ctx.Clear()
	}
}

func (m *Manager) DisplayMessages() []common.DisplayMessage {
	if m.ctx == nil {
		return nil
	}
	snap := m.ctx.Snapshot()
	return DisplayMessages(snap.Messages, snap.CompactBoundary)
}

func (m *Manager) Save() error {
	sess, err := m.ensureCurrent()
	if err != nil {
		return err
	}
	if m.ctx == nil {
		return fmt.Errorf("session: context unavailable")
	}
	promptTokens, completionTokens := 0, 0
	if m.tokenUsage != nil {
		promptTokens, completionTokens = m.tokenUsage()
	}
	var loaded map[string]bool
	if m.loadedSkills != nil {
		loaded = m.loadedSkills()
	}
	ApplyContextSnapshot(sess, m.ctx.Snapshot(), promptTokens, completionTokens, loaded)
	return sess.Save()
}

func (m *Manager) Resume(id string) (*Snapshot, error) {
	sess, err := Load(id)
	if err != nil {
		return nil, fmt.Errorf("session: load: %w", err)
	}
	if m.ctx != nil {
		m.ctx.Restore(ManagerSnapshot(sess))
	}
	if m.addTokens != nil {
		m.addTokens(sess.PromptTokens, sess.CompletionTokens)
	}
	if m.markSkillLoaded != nil {
		for _, name := range sess.LoadedSkills {
			m.markSkillLoaded(name)
		}
	}
	m.Set(sess)
	return sess, nil
}

func (m *Manager) Export(path string) (string, int, error) {
	if m.ctx == nil {
		return "", 0, fmt.Errorf("session: context unavailable")
	}
	msgs := m.ctx.Build(false)
	path, err := ExportMessages(msgs, path)
	return path, len(msgs), err
}

func (m *Manager) ensureCurrent() (*Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sess != nil {
		return m.sess, nil
	}
	sess, err := New(m.cwd)
	if err != nil {
		return nil, err
	}
	m.sess = sess
	return sess, nil
}

func FormatSessionList(sessions []Meta) string {
	if len(sessions) == 0 {
		return "No saved sessions."
	}
	var sb strings.Builder
	sb.WriteString("Saved sessions:\n")
	for _, s := range sessions {
		fmt.Fprintf(&sb, "  %s  %s  %d msgs  %s\n", s.ID, s.Age(), s.MsgCount, s.CWD)
	}
	sb.WriteString("\n/sessions <id> to resume")
	return sb.String()
}

func ResumeFailed(id string, err error) string {
	return fmt.Sprintf("Failed to resume session %s: %v", id, err)
}

func ResumeSuccess(id string, msgCount int) string {
	return fmt.Sprintf("Resumed session %s (%d messages restored).", id, msgCount)
}

func ExportMessages(msgs []types.Message, path string) (string, error) {
	if path == "" {
		path = DefaultExportPath
	}
	data, err := json.MarshalIndent(msgs, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal context: %w", err)
	}
	if err := common.WriteFileWithDir(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	return path, nil
}

func ExportFailed(err error) string {
	return fmt.Sprintf("Failed to %v", err)
}

func ExportSuccess(path string, msgCount int) string {
	return fmt.Sprintf("Context exported to %s (%d messages)", path, msgCount)
}

func ApplyContextSnapshot(sess *Snapshot, snap ctxmgr.ManagerSnapshot, promptTokens, completionTokens int, loaded map[string]bool) {
	if sess == nil {
		return
	}
	sess.SystemPrompt = snap.SystemPrompt
	sess.Skills = snap.Skills
	sess.Memory = snap.Memory
	sess.Archive = snap.Archive
	sess.Messages = snap.Messages
	sess.CompactBoundary = snap.CompactBoundary
	sess.ContextWindow = snap.Budget
	sess.PromptTokens = promptTokens
	sess.CompletionTokens = completionTokens
	sess.TrackerPrompt = snap.Tracker.LastPromptTokens
	sess.TrackerCompletion = snap.Tracker.LastCompTokens
	sess.TrackerNewTokens = snap.Tracker.NewMessageTokens
	sess.CacheHitTokens = snap.Tracker.CacheHitTokens
	sess.CacheMissTokens = snap.Tracker.CacheMissTokens
	sess.SubCount = snap.Tracker.Sub.Count
	sess.SubTokens = snap.Tracker.Sub.TotalTokens
	sess.SubCacheHit = snap.Tracker.Sub.CacheHitTokens
	sess.SubCacheMiss = snap.Tracker.Sub.CacheMissTokens
	sess.LoadedSkills = LoadedSkillNames(loaded)
}

func ManagerSnapshot(sess *Snapshot) ctxmgr.ManagerSnapshot {
	if sess == nil {
		return ctxmgr.ManagerSnapshot{}
	}
	return ctxmgr.ManagerSnapshot{
		SystemPrompt:    sess.SystemPrompt,
		Skills:          sess.Skills,
		Archive:         sess.Archive,
		Memory:          sess.Memory,
		CompactBoundary: sess.CompactBoundary,
		Messages:        sess.Messages,
		Budget:          sess.ContextWindow,
		Tracker: token.State{
			LastPromptTokens: sess.TrackerPrompt,
			LastCompTokens:   sess.TrackerCompletion,
			NewMessageTokens: sess.TrackerNewTokens,
			CacheHitTokens:   sess.CacheHitTokens,
			CacheMissTokens:  sess.CacheMissTokens,
			Sub: token.SubStats{
				Count:           sess.SubCount,
				TotalTokens:     sess.SubTokens,
				CacheHitTokens:  sess.SubCacheHit,
				CacheMissTokens: sess.SubCacheMiss,
			},
		},
	}
}

func LoadedSkillNames(loaded map[string]bool) []string {
	names := make([]string, 0, len(loaded))
	for name, ok := range loaded {
		if ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}
