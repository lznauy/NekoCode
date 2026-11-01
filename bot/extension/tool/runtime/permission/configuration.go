package permission

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
)

type ruleDeclaration struct {
	Tool      string `json:"tool"`
	Specifier string `json:"specifier"`
	Literal   string `json:"literal"`
	Effect    Effect `json:"effect"`
	Source    string `json:"source"`
	Workspace string `json:"workspace"`
}

func declareRule(rule Rule) ruleDeclaration {
	return ruleDeclaration{rule.Tool, rule.Specifier, rule.Literal, rule.Effect, rule.Source, rule.Workspace}
}

// RulesHash covers ordered declarations and the actual path anchors supplied
// to evaluation. Matchers and external judge implementations are not inferred.
// Like Evaluate, this reads an engine configured before publication.
func (e *Engine) RulesHash(workspace, home string) string {
	if e == nil {
		return ""
	}
	rules := []ruleDeclaration{}
	for _, rule := range e.rules {
		rules = append(rules, declareRule(rule))
	}
	type sandboxDeclaration struct {
		Rule          ruleDeclaration `json:"rule"`
		Mode          string          `json:"sandbox_mode"`
		Network       bool            `json:"network"`
		Domains       []string        `json:"outbound_domains"`
		WritableRoots []string        `json:"writable_roots"`
	}
	sandbox := []sandboxDeclaration{}
	for _, rule := range e.sandboxRules {
		sandbox = append(sandbox, sandboxDeclaration{declareRule(rule.Rule), rule.Profile.SandboxMode, rule.Profile.Network, canonicalSet(rule.Profile.OutboundDomains), canonicalSet(rule.Profile.WritableRoots)})
	}
	return declarationHash(struct {
		Schema    int                  `json:"schema"`
		Workspace string               `json:"workspace"`
		Home      string               `json:"home"`
		Rules     []ruleDeclaration    `json:"rules"`
		Sandbox   []sandboxDeclaration `json:"sandbox"`
	}{1, workspace, home, rules, sandbox})
}

// GrantsHash reads the same project store used by capability checks. No cache
// or revision map can hide updates made through another Store instance.
// A load failure is observational only; callers must not turn it into approval.
func (s *Store) GrantsHash() (string, error) {
	if s == nil {
		return "", nil
	}
	storeMu.Lock()
	file, err := s.load()
	storeMu.Unlock()
	if err != nil {
		return "", err
	}
	type grantDeclaration struct {
		Effect       string   `json:"effect"`
		Tool         string   `json:"tool"`
		Capabilities []string `json:"capabilities"`
		WritePaths   []string `json:"write_paths"`
		Domains      []string `json:"outbound_domains"`
		Workspace    string   `json:"workspace"`
	}
	grants := []grantDeclaration{}
	for _, grant := range file.Projects[s.root].Grants {
		grants = append(grants, grantDeclaration{grant.Effect, grant.Tool, canonicalSet(grant.Capabilities), canonicalSet(grant.WritePaths), canonicalSet(grant.OutboundDomains), grant.Workspace})
	}
	return declarationHash(struct {
		Schema  int                `json:"schema"`
		Project string             `json:"project"`
		Grants  []grantDeclaration `json:"grants"`
	}{1, s.root, grants}), nil
}

// Preserve nil versus empty: an absent network ceiling is not an empty one.
func canonicalSet(values []string) []string {
	if values == nil {
		return nil
	}
	out := append([]string{}, values...)
	slices.Sort(out)
	return slices.Compact(out)
}

// Every caller supplies an explicit primitive whitelist, never whole config.
func declarationHash(value any) string {
	data, _ := json.Marshal(value)
	digest := sha256.Sum256(data)
	return "v1:" + hex.EncodeToString(digest[:])
}
