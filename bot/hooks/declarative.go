package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// HookConfig maps the hooks.json file format.
type HookConfig struct {
	PostToolUse        []EventHook `json:"PostToolUse,omitempty"`
	PostToolUseFailure []EventHook `json:"PostToolUseFailure,omitempty"`
	PreToolUse         []EventHook `json:"PreToolUse,omitempty"`
	UserPromptSubmit   []EventHook `json:"UserPromptSubmit,omitempty"`
	SessionStart       []EventHook `json:"SessionStart,omitempty"`
	Stop               []EventHook `json:"Stop,omitempty"`
}

// EventHook is one event with a matcher and list of actions.
type EventHook struct {
	Matcher string       `json:"matcher"` // "Write|Edit", "Bash", ".*", or "tool_name"
	Hooks   []HookAction `json:"hooks"`
}

// HookAction is a single hook action.
type HookAction struct {
	Type    string `json:"type"`    // "command" (for now)
	Command string `json:"command"` // shell command, supports ${CLAUDE_PLUGIN_ROOT}
	Timeout int    `json:"timeout,omitempty"` // ms, default 5000
}

// DeclarativeRegistry stores parsed hooks.json configs and executes matching hooks.
type DeclarativeRegistry struct {
	mu      sync.RWMutex
	configs []pluginHooks // hooks per plugin
}

type pluginHooks struct {
	PluginRoot string
	Config     HookConfig
}

// NewDeclarativeRegistry creates an empty registry.
func NewDeclarativeRegistry() *DeclarativeRegistry {
	return &DeclarativeRegistry{}
}

// ParseAndAdd parses a hooks.json file and adds it to the registry.
func (r *DeclarativeRegistry) ParseAndAdd(pluginRoot string, hooksPath string) error {
	data, err := os.ReadFile(filepath.Join(pluginRoot, hooksPath))
	if err != nil {
		return fmt.Errorf("read hooks file: %w", err)
	}
	var cfg HookConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse hooks json: %w", err)
	}
	r.mu.Lock()
	r.configs = append(r.configs, pluginHooks{PluginRoot: pluginRoot, Config: cfg})
	r.mu.Unlock()
	return nil
}

// RemoveConfigForPlugin removes all hooks registered for the given plugin root.
func (r *DeclarativeRegistry) RemoveConfigForPlugin(pluginRoot string) {
	r.mu.Lock()
	filtered := make([]pluginHooks, 0, len(r.configs))
	for _, ph := range r.configs {
		if ph.PluginRoot != pluginRoot {
			filtered = append(filtered, ph)
		}
	}
	r.configs = filtered
	r.mu.Unlock()
}

func (r *DeclarativeRegistry) PreToolUse(toolName string) []Hint {
	return r.runHooks("PreToolUse", toolName)
}

func (r *DeclarativeRegistry) PostToolUse(toolName string, success bool) []Hint {
	if success {
		return r.runHooks("PostToolUse", toolName)
	}
	return r.runHooks("PostToolUseFailure", toolName)
}

func (r *DeclarativeRegistry) runHooks(eventType, toolName string) []Hint {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var hints []Hint
	for _, ph := range r.configs {
		var eventHooks []EventHook
		switch eventType {
		case "PreToolUse":
			eventHooks = ph.Config.PreToolUse
		case "PostToolUse":
			eventHooks = ph.Config.PostToolUse
		case "PostToolUseFailure":
			eventHooks = ph.Config.PostToolUseFailure
		case "UserPromptSubmit":
			eventHooks = ph.Config.UserPromptSubmit
		case "SessionStart":
			eventHooks = ph.Config.SessionStart
		case "Stop":
			eventHooks = ph.Config.Stop
		}

		for _, eh := range eventHooks {
			if !matchTool(eh.Matcher, toolName) {
				continue
			}
			for _, ha := range eh.Hooks {
				if ha.Type == "command" {
					output, err := r.runCommand(ph.PluginRoot, ha)
					if err != nil {
						hints = append(hints, Hint{
							Type:     "hook_error",
							Severity: "warning",
							Content:  fmt.Sprintf("Hook %q failed: %v", ha.Command, err),
						})
					} else if strings.TrimSpace(output) != "" {
						hints = append(hints, Hint{
							Type:     "hook_output",
							Severity: "info",
							Content:  fmt.Sprintf("[Hook: %s] %s", eventType, strings.TrimSpace(output)),
						})
					}
				}
			}
		}
	}
	return hints
}

func (r *DeclarativeRegistry) runCommand(pluginRoot string, ha HookAction) (string, error) {
	cmd := expandVars(ha.Command, pluginRoot)
	timeout := ha.Timeout
	if timeout <= 0 {
		timeout = 5000
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Millisecond)
	defer cancel()

	c := exec.CommandContext(ctx, "sh", "-c", cmd)
	c.Dir = pluginRoot
	out, err := c.CombinedOutput()
	return string(out), err
}

func expandVars(cmd, pluginRoot string) string {
	// Support ${CLAUDE_PLUGIN_ROOT} (and future ${NEKOCODE_PLUGIN_ROOT}).
	s := strings.ReplaceAll(cmd, "${CLAUDE_PLUGIN_ROOT}", pluginRoot)
	s = strings.ReplaceAll(s, "${PLUGIN_ROOT}", pluginRoot)
	return s
}

func matchTool(matcher, toolName string) bool {
	if matcher == "" || matcher == ".*" {
		return true
	}
	// Split on | for alternation: "Write|Edit" matches Write or Edit.
	for _, alt := range strings.Split(matcher, "|") {
		alt = strings.TrimSpace(alt)
		if alt == toolName {
			return true
		}
		// Try as glob/regex.
		if matched, err := regexp.MatchString("^"+alt+"$", toolName); err == nil && matched {
			return true
		}
	}
	return false
}
