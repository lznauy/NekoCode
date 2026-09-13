package permission

import (
	"bytes"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"

	"mvdan.cc/sh/v3/syntax"
)

// BashMatcher matches bash command rules. Specifier semantics (claude-code):
//
//	"npm run *"   matches commands starting with "npm run " (* = any suffix)
//	"npm run"     exact match (command is exactly "npm run")
//	"npm run:*"   same as "npm run *" (trailing :* form)
//	"*"           matches every command
//
// Compound commands (cmd1 && cmd2) are split on &&, ||, ;, |, & and newlines;
// the rule must match EVERY subcommand for the call to match (so an allow
// rule can't be bypassed by chaining with a disallowed command). A bare
// process wrapper (timeout/time/nice/nohup) is stripped before matching.
type BashMatcher struct{}

func (BashMatcher) Assess(info map[string]any) CallAssessment {
	command, _ := info["command"].(string)
	report := ClassifyShellStructure(command)
	if !report.Dynamic() {
		return CallAssessment{}
	}
	structures := make([]string, len(report.Structures))
	for i, structure := range report.Structures {
		structures[i] = string(structure)
	}
	return CallAssessment{
		Reason:  "dynamic shell execution",
		Signals: structures,
	}
}

func (BashMatcher) MatchLiteral(literal string, info map[string]any) bool {
	command, _ := info["command"].(string)
	return strings.TrimSpace(literal) != "" && strings.TrimSpace(literal) == strings.TrimSpace(command)
}

func (BashMatcher) Match(spec string, info map[string]any) (bool, error) {
	return BashRuleMatches(spec, info, MatchAllSubcommands), nil
}

// MatchForEffect implements EffectAwareMatcher: allow rules must cover EVERY
// subcommand of a compound command (a narrow allow must not be bypassed by
// chaining a disallowed command), while deny/ask rules fire when ANY
// subcommand matches (a denied or guarded subcommand must not slip through
// inside a chain).
func (BashMatcher) MatchForEffect(spec string, info map[string]any, effect Effect) (bool, error) {
	mode := MatchAllSubcommands
	if effect == EffectDeny || effect == EffectAsk {
		mode = MatchAnySubcommand
	}
	return BashRuleMatches(spec, info, mode), nil
}

// CompoundAllowCoverage implements AllowCoverer: the command is split into
// subcommands and coverage holds when EVERY subcommand is matched by at
// least one allow specifier. deciding is the index (into specs) of the first
// specifier that covers a subcommand. hasBareAllow short-circuits coverage:
// a match-all allow covers every subcommand (deciding = -1, the engine maps
// it back to the bare rule). Non-compound commands (a single subcommand)
// report covered = false so the engine falls back to per-rule Match.
func (BashMatcher) CompoundAllowCoverage(hasBareAllow bool, specs []string, callInfo map[string]any) (deciding int, covered bool) {
	cmd, _ := callInfo["command"].(string)
	subcmds := shellCommands(cmd)
	if len(subcmds) <= 1 {
		return -1, false
	}
	if hasBareAllow {
		return -1, true
	}
	if len(specs) == 0 {
		return -1, false
	}
	deciding = -1
	for _, sub := range subcmds {
		subCovered := false
		for i, spec := range specs {
			if matchBashPattern(normalizeBashSpec(spec), sub) {
				subCovered = true
				if deciding < 0 {
					deciding = i
				}
				break
			}
		}
		if !subCovered {
			return -1, false
		}
	}
	return deciding, true
}

type BashMatchMode int

const (
	MatchAllSubcommands BashMatchMode = iota
	MatchAnySubcommand
)

func BashRuleMatches(spec string, info map[string]any, mode BashMatchMode) bool {
	cmd, _ := info["command"].(string)
	if cmd == "" || spec == "" {
		return false
	}
	if spec == "*" {
		return true
	}
	pattern := normalizeBashSpec(spec)
	trimmedCmd := strings.TrimSpace(cmd)
	if mode == MatchAllSubcommands && !hasBashWildcard(pattern) && strings.TrimSpace(pattern) == trimmedCmd {
		return true
	}
	subcmds := shellCommands(cmd)
	if len(subcmds) == 0 {
		return false
	}
	matched := 0
	for _, sub := range subcmds {
		if matchBashPattern(pattern, sub) {
			if mode == MatchAnySubcommand {
				return true
			}
			matched++
			continue
		}
		if mode == MatchAllSubcommands {
			return false
		}
	}
	return mode == MatchAllSubcommands && matched == len(subcmds)
}

func hasBashWildcard(pattern string) bool {
	return pattern == "*" || strings.HasSuffix(pattern, "*")
}

// normalizeBashSpec turns "npm run:*" into "npm run *".
func normalizeBashSpec(spec string) string {
	if strings.HasSuffix(spec, ":*") {
		return strings.TrimSuffix(spec, ":*") + " *"
	}
	return spec
}

// matchBashPattern checks whether a single (sub)command matches a pattern.
// A trailing " *" enforces a word boundary (space or end); a trailing "*"
// without space matches any suffix.
func matchBashPattern(pattern, cmd string) bool {
	cmd = strings.TrimSpace(stripWrappers(cmd))
	pattern = strings.TrimSpace(pattern)
	if pattern == cmd {
		return true
	}
	if strings.HasSuffix(pattern, " *") {
		prefix := strings.TrimSuffix(pattern, " *")
		return cmd == prefix || strings.HasPrefix(cmd, prefix+" ")
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(cmd, strings.TrimSuffix(pattern, "*"))
	}
	return false
}

// stripWrappers uses the same option-aware invocation model as dynamic-shell
// classification, so matching and approval cannot disagree about the command
// hidden behind a wrapper.
func stripWrappers(cmd string) string {
	parser := syntax.NewParser(syntax.Variant(syntax.LangBash))
	file, err := parser.Parse(strings.NewReader(cmd), "")
	if err != nil || len(file.Stmts) != 1 {
		return cmd
	}
	call, ok := file.Stmts[0].Cmd.(*syntax.CallExpr)
	if !ok || len(call.Args) == 0 {
		return cmd
	}
	tokens := make([]shellInvocationToken, len(call.Args))
	for i, word := range call.Args {
		value, static := staticShellWord(word)
		if !static {
			return cmd
		}
		tokens[i] = shellInvocationToken{value: value, static: true}
	}
	unwrapped, dynamic := unwrapShellInvocation(tokens)
	if dynamic || len(unwrapped) == 0 || len(unwrapped) == len(tokens) {
		return cmd
	}
	words := make([]string, len(unwrapped))
	for i, token := range unwrapped {
		words[i] = token.value
	}
	return strings.Join(words, " ")
}

// shellParseCacheMax bounds the memoization of shell parses; once full, the
// cache is cleared wholesale and refilled. A full clear avoids per-entry
// eviction bookkeeping on the hot path, and permission evaluation tolerates
// the occasional re-parse.
const shellParseCacheMax = 256

var (
	shellParseCache sync.Map // command string → []string subcommands
	shellParseCount atomic.Int64
)

// shellCommands splits a command line into its rendered subcommands via a
// full shell parse (mvdan.cc/sh). The result is memoized: the same command is
// re-matched against many rules (deny/ask/allow passes plus allow coverage)
// on every tool call, and the parse dominates that cost. Callers must not
// mutate the returned slice — it is shared from the cache.
func shellCommands(cmd string) []string {
	if v, ok := shellParseCache.Load(cmd); ok {
		return v.([]string)
	}
	out := parseShellCommands(cmd)
	if shellParseCount.Add(1) > shellParseCacheMax {
		shellParseCache.Clear()
		shellParseCount.Store(1)
	}
	shellParseCache.Store(cmd, out)
	return out
}

func parseShellCommands(cmd string) []string {
	parser := syntax.NewParser()
	file, err := parser.Parse(strings.NewReader(cmd), "")
	if err != nil {
		if trimmed := strings.TrimSpace(cmd); trimmed != "" {
			return []string{trimmed}
		}
		return nil
	}
	var out []string
	syntax.Walk(file, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.CmdSubst, *syntax.ProcSubst:
			return true
		case *syntax.CallExpr:
			if s := renderShellNode(n); s != "" {
				out = append(out, s)
			}
			return true
		}
		return true
	})
	return out
}

func renderShellNode(node syntax.Node) string {
	var b bytes.Buffer
	if err := syntax.NewPrinter().Print(&b, node); err != nil {
		return ""
	}
	return strings.TrimSpace(b.String())
}

// FilePathMatcher matches file-path rules for read/edit/write/glob/grep.
// Specifiers use gitignore-style patterns with path anchors:
//
//	"/src/**"        project-relative (resolved against workspace)
//	"~/.*rc"         home-relative
//	"//abs/path/**"  filesystem-absolute
//	"*.env" / "**/.env"  relative to cwd, matches at any depth
//
// info must contain "path" (the call's target path, absolute) and optionally
// "workspace" and "home" for anchor resolution.
type FilePathMatcher struct{}

func (FilePathMatcher) Match(spec string, info map[string]any) (bool, error) {
	target, _ := info["path"].(string)
	if target == "" || spec == "" {
		return false, nil
	}
	workspace, _ := info["workspace"].(string)
	home, _ := info["home"].(string)
	return matchPathPattern(spec, target, workspace, home), nil
}

// matchPathPattern evaluates a gitignore-style path rule against an absolute
// target path. This is a pragmatic implementation (not a full gitignore
// engine): it handles the common anchors and * / ** wildcards.
func matchPathPattern(spec, target, workspace, home string) bool {
	var root, pat string
	switch {
	case strings.HasPrefix(spec, "//"):
		root = "/"
		pat = spec[1:] // "//abs" → "/abs"
	case strings.HasPrefix(spec, "~/"):
		root = home
		pat = strings.TrimPrefix(spec, "~")
	case strings.HasPrefix(spec, "/"):
		root = workspace
		pat = spec
	default:
		// bare pattern → matches at any depth (gitignore semantics)
		return matchGitignoreAnyDepth(spec, target, workspace)
	}
	abs := filepath.Join(root, pat)
	return matchGlob(abs, target)
}

// matchGitignoreAnyDepth matches a bare pattern (no anchor) against any path
// segment, like gitignore matching "**/pattern".
func matchGitignoreAnyDepth(spec, target, workspace string) bool {
	if strings.ContainsAny(spec, `/\`) {
		if workspace != "" {
			if rel, err := filepath.Rel(workspace, target); err == nil && !strings.HasPrefix(rel, "..") {
				if matchGlob(filepath.ToSlash(spec), filepath.ToSlash(rel)) {
					return true
				}
			}
		}
		return matchGlob(filepath.ToSlash(spec), filepath.ToSlash(target))
	}
	base := filepath.Base(target)
	if matchGlob(spec, base) {
		return true
	}
	// also try matching each parent directory's segment
	dir := filepath.Dir(target)
	for dir != "/" && dir != "." {
		if matchGlob(spec, filepath.Base(dir)) {
			return true
		}
		dir = filepath.Dir(dir)
	}
	return false
}

// matchGlob does a filepath.Match with ** support (** crosses directories).
func matchGlob(pattern, name string) bool {
	pattern = filepath.ToSlash(pattern)
	name = filepath.ToSlash(name)
	if !strings.Contains(pattern, "**") {
		ok, _ := filepath.Match(pattern, name)
		return ok
	}
	re, err := regexp.Compile("^" + globToRegex(pattern) + "$")
	if err != nil {
		return false
	}
	return re.MatchString(name)
}

func globToRegex(pattern string) string {
	var b strings.Builder
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				b.WriteString(".*")
				i++
			} else {
				b.WriteString(`[^/]*`)
			}
		case '?':
			b.WriteString(`[^/]`)
		default:
			b.WriteString(regexp.QuoteMeta(pattern[i : i+1]))
		}
	}
	return b.String()
}

// DomainMatcher matches web_fetch rules of the form "domain:host" or
// "domain:*.example.com". Matching is case-insensitive.
type DomainMatcher struct{}

func (DomainMatcher) Match(spec string, info map[string]any) (bool, error) {
	host, _ := info["domain"].(string)
	if host == "" {
		return false, nil
	}
	if !strings.HasPrefix(spec, "domain:") {
		return false, nil
	}
	rule := strings.ToLower(strings.TrimPrefix(spec, "domain:"))
	host = strings.ToLower(host)
	rule = strings.TrimSuffix(rule, ".")
	host = strings.TrimSuffix(host, ".")
	if rule == "*" {
		return true, nil
	}
	if strings.HasPrefix(rule, "*.") {
		suffix := strings.TrimPrefix(rule, "*.")
		// "*.github.com" matches subdomains but NOT the bare apex.
		return strings.HasSuffix(host, "."+suffix), nil
	}
	return host == rule, nil
}

// MCPMatcher matches MCP tool rules: "mcp__server" (all tools from server) or
// "mcp__server__tool". info must contain "tool" (the full canonical name).
type MCPMatcher struct{}

func (MCPMatcher) Match(spec string, info map[string]any) (bool, error) {
	tool, _ := info["tool"].(string)
	if tool == "" || spec == "" {
		return false, nil
	}
	if spec == tool {
		return true, nil
	}
	// "mcp__server__*" matches all tools of server; "mcp__server" matches
	// any tool whose prefix is "mcp__server__".
	if strings.HasSuffix(spec, "__*") {
		prefix := strings.TrimSuffix(spec, "__*") + "__"
		return strings.HasPrefix(tool, prefix), nil
	}
	if strings.HasSuffix(spec, "*") {
		return strings.HasPrefix(tool, strings.TrimSuffix(spec, "*")), nil
	}
	return false, nil
}

// DefaultMatchers returns the standard matcher set keyed by tool name.
func DefaultMatchers() map[string]SpecifierMatcher {
	return map[string]SpecifierMatcher{
		"bash":        BashMatcher{},
		"shell":       BashMatcher{},
		"write":       FilePathMatcher{},
		"edit":        FilePathMatcher{},
		"read":        FilePathMatcher{},
		"list":        FilePathMatcher{},
		"tree":        FilePathMatcher{},
		"glob":        FilePathMatcher{},
		"grep":        FilePathMatcher{},
		"web_fetch":   DomainMatcher{},
		"webfetch":    DomainMatcher{},
		"web_extract": DomainMatcher{},
		"mcp":         MCPMatcher{},
	}
}
