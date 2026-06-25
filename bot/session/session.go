package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"nekocode/common"
	"nekocode/llm/types"
)

type Snapshot struct {
	ID        string `json:"id"`
	CWD       string `json:"cwd"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`

	SystemPrompt    string          `json:"system_prompt"`
	Skills          string          `json:"skills"`
	Memory          string          `json:"memory"`
	Archive         string          `json:"archive"`
	Messages        []types.Message `json:"messages"`
	CompactBoundary int             `json:"compact_boundary"`

	ContextWindow    int `json:"context_window"`
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`

	LoadedSkills []string `json:"loaded_skills"`
}

type Meta struct {
	ID        string `json:"id"`
	CWD       string `json:"cwd"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
	MsgCount  int    `json:"msg_count"`
}

func dir() string {
	return filepath.Join(common.NekocodeHome(), "sessions")
}

func New(cwd string) (*Snapshot, error) {
	now := time.Now()
	return &Snapshot{
		ID:        now.UTC().Format("20060102T150405"),
		CWD:       cwd,
		CreatedAt: now.Unix(),
		UpdatedAt: now.Unix(),
	}, nil
}

func Load(id string) (*Snapshot, error) {
	return common.ReadJSONFile[*Snapshot](filepath.Join(dir(), id, "session.json"))
}

// Delete removes a session directory and all its contents.
func Delete(id string) error {
	return os.RemoveAll(filepath.Join(dir(), id))
}

func (s *Snapshot) Save() error {
	s.UpdatedAt = time.Now().Unix()
	d := filepath.Join(dir(), s.ID)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	return common.WriteFileWithDir(filepath.Join(d, "session.json"), data, 0o644)
}

// sessionMeta is a lightweight struct for deserializing only metadata from
// session.json, avoiding the cost of unmarshaling the full Messages array.
type sessionMeta struct {
	ID        string     `json:"id"`
	CWD       string     `json:"cwd"`
	CreatedAt int64      `json:"created_at"`
	UpdatedAt int64      `json:"updated_at"`
	Messages  []struct{} `json:"messages"` // only need len, not content
}

func loadMeta(id string) (Meta, error) {
	var sm sessionMeta
	path := filepath.Join(dir(), id, "session.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return Meta{}, err
	}
	if err := json.Unmarshal(data, &sm); err != nil {
		return Meta{}, err
	}
	return Meta{
		ID: sm.ID, CWD: sm.CWD,
		CreatedAt: sm.CreatedAt, UpdatedAt: sm.UpdatedAt,
		MsgCount: len(sm.Messages),
	}, nil
}

func List() []Meta {
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

func (m Meta) Age() string {
	d := time.Since(time.Unix(m.UpdatedAt, 0))
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
