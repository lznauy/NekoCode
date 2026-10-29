package permission

import (
	"fmt"
	"nekocode/bot/extension/tool/runtime/netpolicy"
	"path/filepath"
	"sort"
	"strings"
)

// RuleSources aggregates the three rule origins for a workspace:
//
//   - builtin: the default policy (sudo deny, rm ask, ls allow, ...)
//   - declared: user/project rules from config.json "permissions" block
//   - remembered: rules the user approved at an "ask" prompt (persisted)
//
// Evaluation precedence is deny → ask → allow across ALL sources (a deny
// from any source wins; a declared allow can broaden a builtin ask).
type RuleSources struct {
	Declared   PermissionsDecl // from config.json
	Workspace  string
	Remembered []Rule // loaded from the store
}

// PermissionsDecl is the declarative rule config (mirrors config.PermissionsConfig
// without the import cycle — the caller extracts the string slices).
type PermissionsDecl struct {
	Allow   []string
	Ask     []string
	Deny    []string
	Sandbox map[string]SandboxProfile
}

type SandboxProfile struct {
	SandboxMode     string   `json:"sandbox_mode,omitempty"`
	Network         bool     `json:"network,omitempty"`
	OutboundDomains []string `json:"outbound_domains"`
	WritableRoots   []string `json:"writable_roots,omitempty"`
}

type SandboxRule struct {
	Rule    Rule
	Profile SandboxProfile
}

// LoadRules builds the full ordered rule list for the engine. Order within the
// slice does not affect precedence (the engine applies deny→ask→allow), but we
// group by source for readability of diagnostics.
func LoadRules(src RuleSources) ([]Rule, error) {
	var rules []Rule

	// 1. builtin defaults
	rules = append(rules, BuiltinRules()...)

	// 2. declared rules from config (user/project)
	for _, s := range src.Declared.Deny {
		r, err := ParseRule(s, EffectDeny, "declared")
		if err != nil {
			return nil, fmt.Errorf("permissions.deny %q: %w", s, err)
		}
		r.Tool = normalizeToolName(r.Tool)
		rules = append(rules, r)
	}
	for _, s := range src.Declared.Ask {
		r, err := ParseRule(s, EffectAsk, "declared")
		if err != nil {
			return nil, fmt.Errorf("permissions.ask %q: %w", s, err)
		}
		r.Tool = normalizeToolName(r.Tool)
		rules = append(rules, r)
	}
	for _, s := range src.Declared.Allow {
		r, err := ParseRule(s, EffectAllow, "declared")
		if err != nil {
			return nil, fmt.Errorf("permissions.allow %q: %w", s, err)
		}
		r.Tool = normalizeToolName(r.Tool)
		rules = append(rules, r)
	}

	// 3. remembered rules (user approved at an ask prompt). Bound to current
	//    project (workspace).
	for _, r := range src.Remembered {
		var err error
		r, err = canonicalRememberedRule(r)
		if err != nil {
			continue
		}
		if r.Source == "" {
			r.Source = "remembered"
		}
		// Skip remembered rules scoped to other projects (when a rule has a
		// workspace, it only applies to runs in that project).
		if r.Workspace != "" && src.Workspace != "" && r.Workspace != src.Workspace {
			continue
		}
		r.Tool = normalizeToolName(r.Tool)
		rules = append(rules, r)
	}

	return rules, nil
}

// normalizeToolName maps legacy tool names to their current equivalents so
// user-defined rules keep working after a rename (bash → shell).
func normalizeToolName(name string) string {
	if strings.EqualFold(name, "bash") {
		return "shell"
	}
	return name
}

func LoadSandboxRules(decl PermissionsDecl) ([]SandboxRule, error) {
	if len(decl.Sandbox) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(decl.Sandbox))
	for spec := range decl.Sandbox {
		keys = append(keys, spec)
	}
	sort.Strings(keys)
	rules := make([]SandboxRule, 0, len(decl.Sandbox))
	for _, spec := range keys {
		profile := decl.Sandbox[spec]
		profile.SandboxMode = strings.TrimSpace(profile.SandboxMode)
		var domainErr error
		profile.OutboundDomains, domainErr = netpolicy.Normalize(profile.OutboundDomains)
		if domainErr != nil {
			return nil, fmt.Errorf("permissions.sandbox %q: %w", spec, domainErr)
		}
		if profile.OutboundDomains != nil && profile.SandboxMode == "host" {
			return nil, fmt.Errorf("permissions.sandbox %q: %s", spec, `outbound_domains cannot be used with host execution: host mode cannot enforce domain restrictions; use sandbox_mode="workspace-write" or "read-only". If unrestricted host execution is intended, remove outbound_domains from both the tool arguments and matching permissions.sandbox rules (an empty array still requires isolation)`)
		}
		r, err := ParseRule(spec, EffectAllow, "declared")
		if err != nil {
			return nil, fmt.Errorf("permissions.sandbox %q: %w", spec, err)
		}
		if !strings.EqualFold(r.Tool, "bash") && !strings.EqualFold(r.Tool, "shell") {
			return nil, fmt.Errorf("permissions.sandbox %q: only Bash/Shell rules can define sandbox profiles", spec)
		}
		r.Tool = normalizeToolName(r.Tool)
		switch profile.SandboxMode {
		case "", "read-only", "workspace-write", "host":
		default:
			return nil, fmt.Errorf("permissions.sandbox %q: unsupported sandbox_mode %q", spec, profile.SandboxMode)
		}
		rules = append(rules, SandboxRule{Rule: r, Profile: profile})
	}
	return rules, nil
}

// NewEngineForWorkspace builds a ready engine: builtin + declared + remembered
// rules, with the standard matchers registered.
func NewEngineForWorkspace(decl PermissionsDecl, store *Store, workspace string) (*Engine, error) {
	workspace = filepath.Clean(workspace)
	var remembered []Rule
	if store != nil && workspace != "" {
		remembered = store.RememberedRules(workspace)
	}
	rules, err := LoadRules(RuleSources{
		Declared:   decl,
		Workspace:  workspace,
		Remembered: remembered,
	})
	if err != nil {
		return nil, err
	}
	sandboxRules, err := LoadSandboxRules(decl)
	if err != nil {
		return nil, err
	}
	sandboxRules = append(sandboxRules, BuiltinSandboxRules()...)
	e := NewEngine(DefaultMatchers())
	e.SetRules(rules)
	e.SetSandboxRules(sandboxRules)
	return e, nil
}

// CallInfoForBash builds the callInfo map for a bash command.
func CallInfoForBash(command string) map[string]any {
	return map[string]any{"command": command}
}

// CallInfoForPath builds the callInfo map for a file-path tool (read/edit/
// write/glob/grep/list/tree).
func CallInfoForPath(path, workspace, home string) map[string]any {
	return map[string]any{"path": path, "workspace": workspace, "home": home}
}

// CallInfoForDomain builds the callInfo map for web_fetch.
func CallInfoForDomain(domain string) map[string]any {
	return map[string]any{"domain": domain}
}
