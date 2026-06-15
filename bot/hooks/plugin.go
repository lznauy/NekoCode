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

// ---------------------------------------------------------------------------
// hooks.json types
// ---------------------------------------------------------------------------

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
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

// ---------------------------------------------------------------------------
// LoadPluginHooks
// ---------------------------------------------------------------------------

func LoadPluginHooks(pluginRoot, hooksPath string) ([]Hook, error) {
	data, err := os.ReadFile(filepath.Join(pluginRoot, hooksPath))
	if err != nil {
		return nil, fmt.Errorf("read hooks file: %w", err)
	}
	var cfg hookConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse hooks json: %w", err)
	}

	var hooks []Hook

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
		// SessionStart hooks fire only once per session. We use a store
		// flag ("session:started") as a one-shot guard so the hook body
		// executes on the first PreTurn and is skipped thereafter.
		inner := makePluginHook(PreTurn, "SessionStart", pluginRoot, eh, false)
		origOn := inner.On
		inner.On = func(s *Snapshot) *Result {
			if s.flag("session:started") {
				return nil
			}
			s.set("session:started", 1)
			return origOn(s)
		}
		hooks = append(hooks, inner)
	}
	for _, eh := range cfg.Stop {
		hooks = append(hooks, makePluginHook(Stop, "Stop", pluginRoot, eh, false))
	}

	return hooks, nil
}

func makePluginHook(point HookPoint, eventType, pluginRoot string, eh eventHook, requireError bool) Hook {
	return Hook{
		Name:  fmt.Sprintf("plugin:%s:%s", eventType, eh.Matcher),
		Point: point,
		On: func(s *Snapshot) *Result {
			if !matchTool(eh.Matcher, s.Tool) {
				return nil
			}
			if requireError && !s.Error {
				return nil
			}
			for _, ha := range eh.Hooks {
				if ha.Type != "command" {
					continue
				}
				output, err := runPluginCommand(pluginRoot, ha)
				if err != nil {
					return &Result{Hint: &Hint{Type: "hook_error", Severity: "warning",
						Content: fmt.Sprintf("Hook %q failed: %v", ha.Command, err)}}
				}
				if strings.TrimSpace(output) != "" {
					return &Result{Hint: &Hint{Type: "hook_output", Severity: "info",
						Content: fmt.Sprintf("[%s] %s", eventType, strings.TrimSpace(output))}}
				}
			}
			return nil
		},
	}
}

func runPluginCommand(pluginRoot string, ha hookAction) (string, error) {
	cmd := ha.Command
	cmd = strings.ReplaceAll(cmd, "${CLAUDE_PLUGIN_ROOT}", pluginRoot)
	cmd = strings.ReplaceAll(cmd, "${PLUGIN_ROOT}", pluginRoot)
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

var matcherCache sync.Map // string → *regexp.Regexp

func matchTool(matcher, toolName string) bool {
	if matcher == "" || matcher == ".*" {
		return true
	}
	for alt := range strings.SplitSeq(matcher, "|") {
		alt = strings.TrimSpace(alt)
		if alt == toolName {
			return true
		}
		re := getOrCompileMatcher(alt)
		if re != nil && re.MatchString(toolName) {
			return true
		}
	}
	return false
}

func getOrCompileMatcher(pattern string) *regexp.Regexp {
	if v, ok := matcherCache.Load(pattern); ok {
		return v.(*regexp.Regexp)
	}
	re, err := regexp.Compile("^" + pattern + "$")
	if err != nil {
		return nil
	}
	matcherCache.Store(pattern, re)
	return re
}
