package permission

import (
	"fmt"
	"strings"
	"time"

	"mvdan.cc/sh/v3/syntax"
)

// RememberRule persists a rule (typically an allow rule created when the user
// approved an "ask" prompt) into the project's trusted permission store. The
// workspace argument is kept for signature compatibility but ignored — the
// store is already bound to a project root.
func (s *Store) RememberRule(_ string, rule Rule) error {
	if s.root == "" || rule.Tool == "" {
		return nil
	}
	return s.RememberRules(s.root, []Rule{rule})
}

// RememberRules validates the complete approval before atomically saving it.
// A long script must not leave only some of its approved units persisted.
// Session-scoped grants are rejected here: they must never reach the file,
// because entries are append-only and would outlive the session that made
// them meaningful.
func (s *Store) RememberRules(_ string, rules []Rule) error {
	if s.root == "" || len(rules) == 0 {
		return nil
	}
	canonical := make([]Rule, 0, len(rules))
	for _, rule := range rules {
		if !rule.Persistent() {
			continue
		}
		rule, err := canonicalRememberedRule(rule)
		if err != nil {
			return err
		}
		rule.Source = "remembered"
		canonical = append(canonical, rule)
	}
	if len(canonical) == 0 {
		return nil
	}
	type ruleKey struct {
		tool, specifier, literal string
		effect                   Effect
	}
	key := func(r Rule) ruleKey { return ruleKey{r.Tool, r.Specifier, r.Literal, r.Effect} }
	return s.mutate(func(f *permissionFile) bool {
		p := f.Projects[s.root]
		seen := map[ruleKey]bool{}
		for _, existing := range p.Rules {
			existing, err := canonicalRememberedRule(existing)
			if err != nil {
				continue
			}
			seen[key(existing)] = true
		}
		changed := false
		now := time.Now().UTC()
		for _, rule := range canonical {
			if seen[key(rule)] {
				continue
			}
			seen[key(rule)] = true
			rule.CreatedAt = now
			p.Rules = append(p.Rules, rule)
			changed = true
		}
		if changed {
			f.Projects[s.root] = p
		}
		return changed
	})
}

func canonicalRememberedRule(rule Rule) (Rule, error) {
	rule.Tool = normalizeToolName(strings.ToLower(strings.TrimSpace(rule.Tool)))
	rule.Specifier = strings.TrimSpace(rule.Specifier)
	rule.Literal = strings.TrimSpace(rule.Literal)
	if rule.Tool == "" {
		return Rule{}, fmt.Errorf("remembered rule requires a tool")
	}
	if rule.Specifier == "" && rule.Literal == "" {
		return Rule{}, fmt.Errorf("remembered rule for %s requires a specifier or literal", rule.Tool)
	}
	if rule.Literal != "" {
		if rule.Specifier == "" {
			// Older binaries ignore Literal. Mirroring the exact command into
			// Specifier makes rollback exact-match it instead of interpreting an
			// empty specifier as a bare allow-all rule.
			rule.Specifier = rule.Literal
		} else if rule.Specifier != rule.Literal {
			return Rule{}, fmt.Errorf("remembered literal for %s requires an equal compatibility specifier", rule.Tool)
		}
	}
	if rule.Effect != EffectAllow {
		return Rule{}, fmt.Errorf("remembered rule for %s must be allow", rule.Tool)
	}
	if rule.Tool == "bash" || rule.Tool == "shell" {
		if rule.Literal != "" {
			if _, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(rule.Literal), ""); err != nil {
				return Rule{}, fmt.Errorf("remembered bash literal must be parseable: %w", err)
			}
		} else if err := validateRememberedBashSpecifier(rule.Specifier); err != nil {
			return Rule{}, err
		}
	}
	return rule, nil
}

func validateRememberedBashSpecifier(spec string) error {
	if strings.HasSuffix(spec, ":*") {
		return fmt.Errorf("remembered bash rule must be exact, got %q", spec)
	}
	if strings.HasSuffix(spec, " *") && !isCommandWildcardSpecifier(spec) {
		return fmt.Errorf("remembered bash wildcard must be command-scoped, got %q", spec)
	}
	if _, err := syntax.NewParser().Parse(strings.NewReader(spec), ""); err != nil {
		return fmt.Errorf("remembered bash rule must be parseable: %w", err)
	}
	return nil
}

func isCommandWildcardSpecifier(spec string) bool {
	fields := strings.Fields(spec)
	if len(fields) != 2 || fields[1] != "*" {
		return false
	}
	name := fields[0]
	if name == "" {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' || r == '/' {
			continue
		}
		return false
	}
	return true
}

// RememberedRules returns the rules persisted in the project's permissions
// file. Exact-statement (Literal) grants are session-scoped now, so entries
// written by older binaries and session grants are inert. Reading never rewrites
// the store; legacy entries may remain on disk without granting authority.
func (s *Store) RememberedRules(_ string) []Rule {
	if s.root == "" {
		return nil
	}
	f, err := s.load()
	if err != nil {
		return nil
	}
	p, ok := f.Projects[s.root]
	if !ok {
		return nil
	}
	rules := make([]Rule, 0, len(p.Rules))
	for _, r := range p.Rules {
		if r.Literal != "" || !r.Persistent() {
			continue
		}
		rules = append(rules, r)
	}
	return rules
}
