// Package permission provides a unified, declarative permission rule engine
// for all tools — inspired by Claude Code's `allow`/`ask`/`deny` model.
//
// Rules follow the shape  Tool(specifier)  where the specifier is matched by
// a tool-specific SpecifierMatcher (bash parses command globs, edit matches
// gitignore path patterns, web_fetch matches domains, ...). The engine itself
// is tool-agnostic: it only knows Effect (deny/ask/allow) and the evaluation
// order deny → ask → allow, first match wins.
//
// Sandbox (OS isolation) is a separate, bash-only layer; this permission
// layer is the universal "can the tool run at all / should we prompt" gate
// that applies to every tool call before execution.

package permission

import (
	"context"
	"fmt"
	"nekocode/bot/extension/tool/runtime/netpolicy"
	"strings"
	"time"
)

// Effect is the outcome of a permission rule.
type Effect int

const (
	EffectDeny  Effect = iota // block the call outright
	EffectAsk                 // prompt the user; remember on approval
	EffectAllow               // run without prompting
)

func (e Effect) String() string {
	switch e {
	case EffectDeny:
		return "deny"
	case EffectAsk:
		return "ask"
	case EffectAllow:
		return "allow"
	}
	return "unknown"
}

// RuleLifetime decides how long a remembered grant survives. Rules that can
// be generalized to a reusable prefix are persistent; grants for one-off
// commands (dynamic shell structures, exact statements) live only for the
// current session and never reach the permissions file.
type RuleLifetime string

const (
	// LifetimePersistent rules are written to the trusted permission store.
	LifetimePersistent RuleLifetime = "persistent"
	// LifetimeSession rules are held in memory by the executor and dropped
	// when the session ends.
	LifetimeSession RuleLifetime = "session"
)

// Persistent reports whether the rule may be written to the permissions file.
// The zero value is persistent so declared and builtin rules keep their
// existing semantics.
func (r Rule) Persistent() bool {
	return r.Lifetime != LifetimeSession
}

// Rule is a single permission rule: "tool(specifier)" with an effect.
// An empty specifier matches every call of the tool (bare "Tool" form).
type Rule struct {
	Tool      string       `json:"tool"`
	Specifier string       `json:"specifier,omitempty"` // raw specifier text (e.g. "npm run *", "/src/**", "domain:github.com")
	Literal   string       `json:"literal,omitempty"`   // exact call text; never persisted (session-scoped only)
	Effect    Effect       `json:"effect"`
	Source    string       `json:"source,omitempty"`    // "builtin" | "user" | "project" | "remembered" | "session"
	Lifetime  RuleLifetime `json:"lifetime,omitempty"`  // empty → persistent
	Workspace string       `json:"workspace,omitempty"` // project root scope; empty → global (legacy)
	CreatedAt time.Time    `json:"createdAt,omitempty"` // set when persisted
}

// SpecifierMatcher decodes a rule's specifier and decides whether a concrete
// tool call matches it. Each tool registers one matcher; the engine looks it
// up by tool name. The call descriptor is tool-specific (command string for
// bash, path for edit, domain for web_fetch, ...).
type SpecifierMatcher interface {
	// Match reports whether the call (described by callInfo) matches the rule
	// specifier. callInfo keys are tool-defined (e.g. {"command":"rm -rf /"}).
	Match(specifier string, callInfo map[string]any) (bool, error)
}

// LiteralMatcher is implemented by matchers that can compare an exact,
// non-pattern call identity. Remembered dynamic shell approvals use this path
// so they can never be broadened into command-prefix wildcards.
type LiteralMatcher interface {
	MatchLiteral(literal string, callInfo map[string]any) bool
}

// EffectAwareMatcher is an optional SpecifierMatcher extension for matchers
// whose matching semantics depend on the rule's effect. The engine probes
// for it in ruleApplies; matchers that don't implement it get plain Match
// for every effect. (Bash implements it: allow rules must cover every
// subcommand of a compound command, deny/ask rules fire on any matching
// subcommand.)
type EffectAwareMatcher interface {
	SpecifierMatcher
	MatchForEffect(specifier string, callInfo map[string]any, effect Effect) (bool, error)
}

// JudgeVerdict is an authorization result, independent of display text.
type JudgeVerdict uint8

const (
	JudgeNotRun JudgeVerdict = iota
	JudgeSafe
	JudgeDangerous
	JudgeFailed
)

// CallAssessment is tool-specific risk metadata produced by a matcher before
// the engine finalizes its decision. It lets the engine remain the sole
// allow/ask/deny authority while still supporting structured command analysis.
type CallAssessment struct {
	Verdict JudgeVerdict
	Reason  string
	Signals []string
}

func (a CallAssessment) RequiresApproval() bool { return len(a.Signals) > 0 }

// CallAssessor is implemented by matchers that can identify risks not safely
// expressible as a flat rule specifier, such as indirect shell execution.
type CallAssessor interface {
	Assess(callInfo map[string]any) CallAssessment
}

// Engine evaluates permission rules for tool calls.
type Engine struct {
	matchers     map[string]SpecifierMatcher
	rules        []Rule // ordered; evaluation is deny→ask→allow within the matched tool
	sandboxRules []SandboxRule
	// autoJudge can allow shell commands after deny and explicit user ask
	// checks. A failed or dangerous verdict retains normal rule evaluation.
	autoJudge        func(ctx context.Context, command string) (allow bool, decided bool)
	autoJudgeEnabled func() bool
}

// SetAutoJudge registers the optional safety judge for shell calls.
// decided=false retains normal rule evaluation.
// ctx is the calling tool's context, so a slow remote judge honors
// cancellation instead of outliving the call it is deciding for.
func (e *Engine) SetAutoJudge(fn func(ctx context.Context, command string) (allow bool, decided bool)) {
	e.autoJudge = fn
}

// SetAutoJudgeEnabled distinguishes an inactive judge from a failed judgment.
// Without a predicate, a registered judge is considered active.
func (e *Engine) SetAutoJudgeEnabled(fn func() bool) { e.autoJudgeEnabled = fn }

// NewEngine creates an engine with the given matchers (tool name → matcher).
func NewEngine(matchers map[string]SpecifierMatcher) *Engine {
	return &Engine{matchers: matchers}
}

// SetRules replaces the engine's rule set. Rules are kept in source order;
// Evaluate applies the deny→ask→allow precedence across them.
func (e *Engine) SetRules(rules []Rule) {
	e.rules = rules
}

// Rules returns a copy of the engine's current rule set. Callers that layer
// extra grants on top of a rebuilt engine (for example session-scoped
// approvals) use it so SetRules never silently drops existing rules.
func (e *Engine) Rules() []Rule {
	out := make([]Rule, len(e.rules))
	copy(out, e.rules)
	return out
}

// SetSandboxRules replaces the sandbox profile rule set. Sandbox rules never
// decide allow/ask/deny; callers use them only to attach an explicit sandbox
// request to a matching shell command before normal permission evaluation.
func (e *Engine) SetSandboxRules(rules []SandboxRule) {
	e.sandboxRules = rules
}

// SandboxFor returns the most specific sandbox profile matching a tool call,
// intersected with the domain ceilings of every matching subcommand.
func (e *Engine) SandboxFor(toolName string, callInfo map[string]any) (SandboxProfile, bool) {
	var best SandboxRule
	bestScore := -1
	var domains []string
	for _, sr := range e.sandboxRules {
		matches := e.ruleApplies(sr.Rule, toolName, callInfo)
		if sr.Profile.OutboundDomains != nil {
			// A ceiling must survive unrelated commands in a chain. Reuse
			// deny's any-subcommand semantics only for this restriction;
			// other profile fields retain their original matching semantics.
			domainRule := sr.Rule
			domainRule.Effect = EffectDeny
			// Keep whole-call matches too, including exact compound rules.
			if matches || e.ruleApplies(domainRule, toolName, callInfo) {
				domains = netpolicy.Intersect(domains, sr.Profile.OutboundDomains)
			}
		}
		if !matches {
			continue
		}
		score := sandboxRuleSpecificity(sr.Rule)
		if score > bestScore {
			best = sr
			bestScore = score
		}
	}
	if bestScore >= 0 {
		best.Profile.OutboundDomains = domains
		return best.Profile, true
	}
	if domains != nil {
		return SandboxProfile{OutboundDomains: domains}, true
	}
	return SandboxProfile{}, false
}

// sandboxRuleSpecificity ranks competing sandbox rules so the most specific
// one wins: specifier length dominates (longer ≈ more specific), tool-name
// length breaks ties. The ×2 weight is an unmeasured heuristic.
func sandboxRuleSpecificity(r Rule) int {
	return len(strings.TrimSpace(r.Specifier))*2 + len(strings.TrimSpace(r.Tool))
}

// Decision is the outcome of evaluating a call.
type Decision struct {
	Effect     Effect
	Rule       Rule // the rule that decided the outcome (zero value if no match)
	Assessment CallAssessment
}

// DenyRule checks hard rules without running the judge or assessing approval.
// Read-only previews use this same deny boundary as execution.
func (e *Engine) DenyRule(toolName string, callInfo map[string]any) (Rule, bool) {
	for _, rule := range e.rules {
		if rule.Effect == EffectDeny && e.ruleApplies(rule, toolName, callInfo) {
			return rule, true
		}
	}
	return Rule{}, false
}

// Evaluate decides what to do with a tool call without cancellation support.
// It is retained for API compatibility; runtime callers should use
// EvaluateContext so remote judges inherit the tool call's context.
func (e *Engine) Evaluate(toolName string, callInfo map[string]any, defaultEffect Effect) Decision {
	return e.EvaluateContext(context.Background(), toolName, callInfo, defaultEffect)
}

// EvaluateContext decides what to do with a tool call. Precedence (highest first):
//
//  1. deny from ANY source (a deny can never be overridden, not even by the
//     auto-mode judge)
//  2. ask declared by the user (declared/remembered) — explicit prompts win
//  3. allow declared by the user (declared/remembered) — a remembered allow
//     overrides a builtin ask and avoids an unnecessary remote judgment
//  4. auto-mode judge (shell only): remaining commands are judged before
//     builtin rules — confidently safe runs immediately, so builtin ask rules
//     do not preempt it; a dangerous verdict falls through to the chain below
//  5. builtin ask
//  6. builtin allow
//  7. defaultEffect (no rule matched)
//
// Tool names are compared case-insensitively so users can write "Bash(...)"
// (claude-code style) while the engine keys on the lowercase canonical name.
func (e *Engine) EvaluateContext(ctx context.Context, toolName string, callInfo map[string]any, defaultEffect Effect) (decision Decision) {
	judgeReason := ""
	defer func() {
		if judgeReason != "" && decision.Assessment.Reason == "" {
			decision.Assessment.Reason = judgeReason
			decision.Assessment.Verdict = JudgeFailed
		}
		decision = e.applyAssessment(toolName, callInfo, decision)
	}()
	// 1. deny from any source (hard rules — the judge never overrides them)
	if r, ok := e.DenyRule(toolName, callInfo); ok {
		return Decision{Effect: EffectDeny, Rule: r}
	}
	// 2. user-declared ask
	for _, r := range e.rules {
		if r.Effect == EffectAsk && !r.isBuiltin() && e.ruleApplies(r, toolName, callInfo) {
			return Decision{Effect: EffectAsk, Rule: r}
		}
	}
	// 3. user-declared allow (covers remembered allows and avoids an
	// unnecessary remote judgment for a decision the user already made).
	if d, ok := e.evaluateAllowCoverage(toolName, callInfo, false); ok {
		return d
	}
	for _, r := range e.rules {
		if r.Effect == EffectAllow && !r.isBuiltin() && e.ruleApplies(r, toolName, callInfo) {
			return Decision{Effect: EffectAllow, Rule: r}
		}
	}
	// 4. auto mode: the judge is the first-pass authority for remaining shell calls —
	// commands not covered by a user rule are judged before builtin rules
	// (rm, git push, ...) no longer preempt it. Confidently safe runs
	// immediately; a dangerous verdict falls through to the chain, where
	// a dangerous verdict requires approval even when builtin rules allow it.
	judgedDangerous := false
	if defaultEffect == EffectAsk && e.autoJudge != nil && toolName == "shell" && (e.autoJudgeEnabled == nil || e.autoJudgeEnabled()) {
		if command, _ := callInfo["command"].(string); command != "" {
			allow, decided := e.autoJudge(ctx, command)
			// The request may outlive a switch to manual mode. Discard stale
			// verdicts and use ordinary rules/structure checks in that case.
			active := e.autoJudgeEnabled == nil || e.autoJudgeEnabled()
			if active && decided {
				if allow {
					return Decision{Effect: EffectAllow, Assessment: CallAssessment{Verdict: JudgeSafe, Reason: "jev: judged safe"}}
				}
				judgedDangerous = true
			} else if active {
				judgeReason = "jev: judgment failed"
			}
		}
	}
	// 5. builtin ask
	for _, r := range e.rules {
		if r.Effect == EffectAsk && r.isBuiltin() && e.ruleApplies(r, toolName, callInfo) {
			d := Decision{Effect: EffectAsk, Rule: r}
			if judgedDangerous {
				d.Assessment = CallAssessment{Verdict: JudgeDangerous, Reason: "jev: judged dangerous"}
			}
			return d
		}
	}
	// Builtin convenience rules cannot overrule a content-specific danger
	// verdict. User-declared allows were already considered before the judge.
	if judgedDangerous {
		return Decision{Effect: EffectAsk, Assessment: CallAssessment{Verdict: JudgeDangerous, Reason: "jev: judged dangerous"}}
	}
	// 6. builtin allow
	if d, ok := e.evaluateAllowCoverage(toolName, callInfo, true); ok {
		return d
	}
	for _, r := range e.rules {
		if r.Effect == EffectAllow && r.isBuiltin() && e.ruleApplies(r, toolName, callInfo) {
			return Decision{Effect: EffectAllow, Rule: r}
		}
	}
	// 7. defaultEffect (no rule matched): judge unavailable/uncertain keeps
	// the ordinary default policy.
	return Decision{Effect: defaultEffect}
}

func (e *Engine) applyAssessment(toolName string, callInfo map[string]any, decision Decision) Decision {
	assessor, ok := e.matcherFor(toolName).(CallAssessor)
	if !ok {
		return decision
	}
	var assessment CallAssessment
	switch assessor.(type) {
	case BashMatcher, *BashMatcher:
		assessment = e.assessShellWithRules(toolName, callInfo)
	default:
		assessment = assessor.Assess(callInfo)
	}
	if decision.Assessment.Verdict != JudgeNotRun || assessment.Reason == "" && decision.Assessment.Reason != "" {
		// Keep the judge's verdict independently of dynamic structure signals.
		assessment.Reason = decision.Assessment.Reason
		assessment.Verdict = decision.Assessment.Verdict
	}
	decision.Assessment = assessment
	if decision.Effect == EffectAllow && assessment.Verdict == JudgeSafe {
		return decision // Auto judges the complete command, including dynamic syntax.
	}
	if !assessment.RequiresApproval() || decision.Effect == EffectDeny {
		return decision
	}
	if decision.Effect == EffectAllow && e.hasRememberedLiteralAllow(toolName, callInfo) {
		return decision
	}
	decision.Effect = EffectAsk
	return decision
}

// evaluateAllowCoverage combines shell rules using the same parsed approval
// units as structure assessment. Other tools use ordinary per-rule matching.
func (e *Engine) evaluateAllowCoverage(toolName string, callInfo map[string]any, builtinOnly bool) (Decision, bool) {
	switch e.matcherFor(toolName).(type) {
	case BashMatcher, *BashMatcher:
		return e.evaluateShellAllowCoverage(toolName, callInfo, builtinOnly)
	default:
		return Decision{}, false
	}
}

// isBuiltin reports whether a rule came from the baked-in default policy.
func (r Rule) isBuiltin() bool { return r.Source == "builtin" }

// ruleApplies reports whether a rule matches a given tool call: the tool name
// must match (case-insensitive, or "*" wildcard), and if the rule has a
// specifier, the tool's matcher must accept it. Matchers implementing
// EffectAwareMatcher get the rule's effect so they can pick effect-dependent
// semantics themselves (e.g. bash all- vs any-subcommand).
func (e *Engine) ruleApplies(r Rule, toolName string, callInfo map[string]any) bool {
	if !toolNameMatches(r.Tool, toolName) {
		return false
	}
	if r.Literal != "" {
		matcher, ok := e.matcherFor(toolName).(LiteralMatcher)
		return ok && matcher.MatchLiteral(r.Literal, callInfo)
	}
	if r.Specifier == "" {
		return true
	}
	m := e.matcherFor(toolName)
	if m == nil {
		return false
	}
	if em, ok := m.(EffectAwareMatcher); ok {
		match, err := em.MatchForEffect(r.Specifier, callInfo, r.Effect)
		return err == nil && match
	}
	match, err := m.Match(r.Specifier, callInfo)
	return err == nil && match
}

// hasRememberedLiteralAllow reports whether the user previously approved this
// exact call text. It deliberately ignores declared and wildcard allows: only
// a human-created literal grant (remembered for the project, or granted for
// this session) may suppress a future dynamic-shell prompt.
func (e *Engine) hasRememberedLiteralAllow(toolName string, callInfo map[string]any) bool {
	for _, rule := range e.rules {
		if (rule.Source == "remembered" || rule.Source == "session") && rule.Effect == EffectAllow && rule.Literal != "" &&
			e.ruleApplies(rule, toolName, callInfo) {
			return true
		}
	}
	return false
}

// matcherFor resolves the matcher registered for a tool: the lowercased name
// first, then the name as given, then the shared "mcp" matcher for mcp__*
// tools. Returns nil when no matcher is registered.
func (e *Engine) matcherFor(toolName string) SpecifierMatcher {
	if m, ok := e.matchers[strings.ToLower(toolName)]; ok {
		return m
	}
	if m, ok := e.matchers[toolName]; ok {
		return m
	}
	if strings.HasPrefix(strings.ToLower(toolName), "mcp__") {
		if m, ok := e.matchers["mcp"]; ok {
			return m
		}
	}
	return nil
}

func toolNameMatches(ruleTool, callTool string) bool {
	if ruleTool == "*" {
		return true
	}
	rule := strings.ToLower(ruleTool)
	call := strings.ToLower(callTool)
	if rule == call {
		return true
	}
	if strings.HasSuffix(rule, "*") {
		return strings.HasPrefix(call, strings.TrimSuffix(rule, "*"))
	}
	return false
}

// ParseRule parses a "Tool(specifier)" string into a Rule with the given
// effect. "Tool" (no parens) parses to an empty specifier (match-all).
// Examples:
//
//	"Bash(npm run *)"   → {Tool:"bash", Specifier:"npm run *"}
//	"Read(./.env)"      → {Tool:"read", Specifier:"./.env"}
//	"Bash"              → {Tool:"bash", Specifier:""}  (match-all)
//	"mcp__github__*"    → {Tool:"mcp__github__*"}      (bare, match-all)
func ParseRule(s string, effect Effect, source string) (Rule, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Rule{}, fmt.Errorf("empty rule")
	}
	// Bare tool name (no parentheses) — match-all for that tool.
	// Reject anything that contains a stray ')' to keep the grammar tight.
	if !strings.Contains(s, "(") {
		if strings.ContainsAny(s, ")") || strings.ContainsAny(s, " \t") {
			return Rule{}, fmt.Errorf("malformed rule %q: unexpected character", s)
		}
		return Rule{Tool: s, Effect: effect, Source: source}, nil
	}
	open := strings.IndexByte(s, '(')
	close := strings.LastIndexByte(s, ')')
	if open <= 0 || close != len(s)-1 {
		return Rule{}, fmt.Errorf("malformed rule %q: expected Tool(specifier)", s)
	}
	tool := s[:open]
	spec := s[open+1 : close]
	if tool == "" {
		return Rule{}, fmt.Errorf("malformed rule %q: empty tool name", s)
	}
	spec = strings.TrimSpace(spec)
	return Rule{Tool: tool, Specifier: spec, Effect: effect, Source: source}, nil
}
