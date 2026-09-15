package extension

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"sort"
	"strings"

	"nekocode/bot/extension/agentprofile"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/toolutil"
)

// AgentInfo exposes resolved capabilities without publishing system prompts.
type AgentInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Plugin      string   `json:"plugin,omitempty"`
	Description string   `json:"description"`
	Tools       []string `json:"tools"`
	Skills      []string `json:"skills,omitempty"`
	MaxSteps    int      `json:"max_steps"`
}

// AgentProfile resolves exact IDs first, then unique short names. Builtin short
// names remain stable; plugin names never replace them. Ownership lives in
// activePlugin, so closing one manager cannot unregister another's profiles.
func (m *Manager) AgentProfile(name string) (agentprofile.Profile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	profiles := m.agentProfilesLocked()
	for _, p := range profiles {
		if p.Name == name {
			return p, nil
		}
	}
	var matches []agentprofile.Profile
	for _, p := range profiles {
		if _, short, ok := strings.Cut(p.Name, "/"); ok && short == name {
			matches = append(matches, p)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		ids := make([]string, len(matches))
		for i, p := range matches {
			ids[i] = p.Name
		}
		return agentprofile.Profile{}, fmt.Errorf("ambiguous agent profile %q; use %s", name, strings.Join(ids, " or "))
	}
	return agentprofile.Profile{}, fmt.Errorf("unknown agent profile %q; use agent_profiles to list available profiles and load errors", name)
}

func (m *Manager) agentProfilesLocked() []agentprofile.Profile {
	profiles := agentprofile.Builtins()
	for owner, state := range m.active {
		for _, p := range state.agents {
			p = agentprofile.Clone(p)
			p.Name = owner + "/" + p.Name
			profiles = append(profiles, p)
		}
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Name < profiles[j].Name })
	return profiles
}

func (m *Manager) agentInfosLocked() []AgentInfo {
	var out []AgentInfo
	for _, p := range m.agentProfilesLocked() {
		info := AgentInfo{ID: p.Name, Name: p.Name, Description: p.Description, Tools: p.Tools, Skills: p.Skills, MaxSteps: p.MaxSteps}
		if owner, short, ok := strings.Cut(p.Name, "/"); ok {
			info.Plugin, info.Name = owner, short
		}
		if info.MaxSteps == 0 {
			info.MaxSteps = agentprofile.MaxSteps
		}
		out = append(out, info)
	}
	return out
}

func (m *Manager) agentErrorsLocked() map[string]string { return maps.Clone(m.agentErrors) }

// Preserve plugin ordering while excluding failed activations from the skill
// and command catalog, just as they are excluded from agents, hooks and MCP.
func (m *Manager) activeSkillDirsLocked() []string {
	var dirs []string
	for _, p := range m.plugins.ListPlugins() {
		if state, ok := m.active[p.Name]; ok {
			dirs = append(dirs, state.plugin.SkillDirs()...)
		}
	}
	return dirs
}

type agentCatalogTool struct {
	toolutil.SafeReadOnlyTool
	manager *Manager
}

func (*agentCatalogTool) Name() string { return "agent_profiles" }
func (*agentCatalogTool) Description() string {
	return "List available sub-agent profile IDs, descriptions, exact tool ceilings, default skills, step limits and plugin load errors. Consult this catalog before choosing a custom profile for task."
}
func (*agentCatalogTool) Parameters() []core.Parameter { return nil }
func (t *agentCatalogTool) Execute(ctx context.Context, _ map[string]any) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	t.manager.mu.Lock()
	defer t.manager.mu.Unlock()
	data, err := json.Marshal(struct {
		Profiles []AgentInfo       `json:"profiles"`
		Errors   map[string]string `json:"errors,omitempty"`
	}{t.manager.agentInfosLocked(), t.manager.agentErrorsLocked()})
	return string(data), err
}
