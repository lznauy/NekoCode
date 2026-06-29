package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	maxPluginOutputBytes  = 4096
	pluginOutputUntrusted = "true"
)

type hookConfig struct {
	PreToolUse         []eventHook `json:"PreToolUse,omitempty"`
	PostToolUse        []eventHook `json:"PostToolUse,omitempty"`
	PostToolUseFailure []eventHook `json:"PostToolUseFailure,omitempty"`
	UserPromptSubmit   []eventHook `json:"UserPromptSubmit,omitempty"`
	SessionStart       []eventHook `json:"SessionStart,omitempty"`
	Stop               []eventHook `json:"Stop,omitempty"`
}

type eventHook struct {
	Matcher string       `json:"matcher"`
	Hooks   []hookAction `json:"hooks"`
}

type hookAction struct {
	Type         string          `json:"type"`
	Command      string          `json:"command"`
	Code         string          `json:"code,omitempty"`
	Path         string          `json:"path,omitempty"`
	Timeout      int             `json:"timeout,omitempty"`
	OutputSchema json.RawMessage `json:"output_schema,omitempty"`
}

func Load(pluginRoot, hooksPath string) ([]Hook, error) {
	data, err := os.ReadFile(filepath.Join(pluginRoot, hooksPath))
	if err != nil {
		return nil, fmt.Errorf("read hooks file: %w", err)
	}

	var cfg hookConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse hooks json: %w", err)
	}

	hooks := make([]Hook, 0)
	for _, eh := range cfg.PreToolUse {
		hooks = append(hooks, makePluginHook(PreToolUse, "PreToolUse", pluginRoot, eh, false))
	}
	for _, eh := range cfg.PostToolUse {
		hooks = append(hooks, makePluginHook(PostToolUse, "PostToolUse", pluginRoot, eh, false))
	}
	for _, eh := range cfg.PostToolUseFailure {
		hooks = append(hooks, makePluginHook(PostToolUse, "PostToolUseFailure", pluginRoot, eh, true))
	}
	for _, eh := range cfg.UserPromptSubmit {
		hooks = append(hooks, makePluginHook(UserSubmit, "UserPromptSubmit", pluginRoot, eh, false))
	}
	for _, eh := range cfg.SessionStart {
		hooks = append(hooks, makeSessionStartHook(pluginRoot, eh))
	}
	for _, eh := range cfg.Stop {
		hooks = append(hooks, makePluginHook(Stop, "Stop", pluginRoot, eh, false))
	}
	return hooks, nil
}

func makeSessionStartHook(pluginRoot string, eh eventHook) Hook {
	inner := makePluginHook(PreTurn, "SessionStart", pluginRoot, eh, false)
	inner.Once = true
	return inner
}
