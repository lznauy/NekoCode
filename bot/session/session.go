package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"nekocode/llm/types"
)

type Snapshot struct {
	ID        string `json:"id"`
	CWD       string `json:"cwd"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`

	SystemPrompt    string        `json:"system_prompt"`
	Skills          string        `json:"skills"`
	Memory          string        `json:"memory"`
	Archive         string        `json:"archive"`
	Messages        []types.Message `json:"messages"`
	CompactBoundary int           `json:"compact_boundary"`

	ContextWindow      int `json:"context_window"`
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
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".nekocode", "sessions")
}

func New(cwd string) (*Snapshot, error) {
	now := time.Now()
	s := &Snapshot{
		ID:        now.UTC().Format("20060102T150405"),
		CWD:       cwd,
		CreatedAt: now.Unix(),
		UpdatedAt: now.Unix(),
	}
	d := filepath.Join(dir(), s.ID)
	if err := os.MkdirAll(d, 0755); err != nil {
		return nil, err
	}
	return s, s.Save()
}

func Load(id string) (*Snapshot, error) {
	data, err := os.ReadFile(filepath.Join(dir(), id, "session.json"))
	if err != nil {
		return nil, err
	}
	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (s *Snapshot) Save() error {
	s.UpdatedAt = time.Now().Unix()
	d := filepath.Join(dir(), s.ID)
	if err := os.MkdirAll(d, 0755); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(s, "", "  ")
	return os.WriteFile(filepath.Join(d, "session.json"), data, 0644)
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
		s, err := Load(e.Name())
		if err != nil {
			continue
		}
		out = append(out, Meta{
			ID: s.ID, CWD: s.CWD,
			CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
			MsgCount: len(s.Messages),
		})
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
