package permission

import (
	"strings"
	"sync"
	"sync/atomic"

	"mvdan.cc/sh/v3/syntax"
)

var shellApprovalCache sync.Map // command -> immutable []shellApprovalUnit
var shellApprovalCount atomic.Int64

// shellApprovalUnit is either a reusable command or an exact statement whose
// shell effects cannot be represented by a command-prefix grant.
type shellApprovalUnit struct {
	command    string
	specifier  string
	structures []ShellStructure
}

func shellApprovalUnits(command string) []shellApprovalUnit {
	command = strings.TrimSpace(command)
	if cached, ok := shellApprovalCache.Load(command); ok {
		return cached.([]shellApprovalUnit)
	}
	units := parseShellApprovalUnits(command)
	if shellApprovalCount.Add(1) > shellParseCacheMax {
		shellApprovalCache.Clear()
		shellApprovalCount.Store(1)
	}
	shellApprovalCache.Store(command, units)
	return units
}

func parseShellApprovalUnits(command string) []shellApprovalUnit {
	if command == "" {
		return nil
	}
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(command), "")
	if err != nil {
		return []shellApprovalUnit{{command: command, structures: []ShellStructure{ShellUnparseable}}}
	}
	report := ClassifyShellStructure(command)
	whole := func() []shellApprovalUnit {
		structures := report.Structures
		if len(structures) == 0 {
			structures = []ShellStructure{"shell_context"}
		}
		return []shellApprovalUnit{{command: command, structures: structures}}
	}
	// These effects can change how later commands resolve or execute. An
	// approval for the effect alone must not authorize arbitrary new tails.
	for _, s := range report.Structures {
		switch s {
		case ShellEnvironment, ShellEval, ShellSource, ShellDynamicCommand, ShellUnparseable:
			return whole()
		}
	}
	var units []shellApprovalUnit
	var visit func(*syntax.Stmt) bool
	visit = func(stmt *syntax.Stmt) bool {
		if binary, ok := stmt.Cmd.(*syntax.BinaryCmd); ok {
			if len(stmt.Redirs) != 0 {
				return false
			}
			// A dynamic consumer can interpret its pipeline input as code;
			// approving the consumer text alone does not approve a new producer.
			if (binary.Op == syntax.Pipe || binary.Op == syntax.PipeAll) && ClassifyShellStructure(renderShellNode(stmt)).Dynamic() {
				return false
			}
			return visit(binary.X) && visit(binary.Y)
		}
		call, ok := stmt.Cmd.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return false
		}
		bare := renderShellNode(call)
		bareReport := ClassifyShellStructure(bare)
		copyStmt := *stmt
		copyStmt.Semicolon = syntax.Pos{}
		start, end := int(copyStmt.Pos().Offset()), int(copyStmt.End().Offset())
		if start < 0 || end > len(command) || start >= end {
			return false
		}
		text := strings.TrimSpace(command[start:end])
		local := ClassifyShellStructure(text)
		// A redirection needs its own exact approval, but does not prevent
		// remembering the ordinary command independently (e.g. curl *).
		ordinaryCommand := true
		for _, structure := range bareReport.Structures {
			if structure != ShellCommandSubstitution && structure != ShellProcessSubstitution {
				ordinaryCommand = false
			}
		}
		// A statement with dynamic structures is only ever approved as one
		// exact unit: a reusable prefix for its own command (printf *, curl *)
		// would silently reuse new substitutions in later scripts. A plain
		// redirection does not prevent remembering the command independently.
		if ordinaryCommand && !hasDynamicStructure(local.Structures) {
			// exec replaces the shell and disappears once wrappers are
			// unwrapped, so it must be detected on the raw argv.
			if first, known := staticShellWord(call.Args[0]); known && strings.ToLower(first) == "exec" {
				return false
			}
			tokens := make([]shellInvocationToken, len(call.Args))
			for i, w := range call.Args {
				value, static := staticShellWord(w)
				tokens[i] = shellInvocationToken{value: value, static: static}
			}
			unwrapped, unknown := unwrapShellInvocation(tokens, nil)
			if unknown || len(unwrapped) == 0 {
				return false
			}
			name := unwrapped[0].value
			// Shell builtins that mutate resolution or the execution
			// environment cannot be represented by a command prefix; keep
			// the complete statement as one exact approval.
			switch name {
			case "cd", "pushd", "popd", "hash", "alias", "unalias", "enable", "trap", "set", "shopt", "unset", "read", "readarray", "mapfile", "umask", "ulimit", "let", "getopts", "exec":
				return false
			}
			// An interpreter without a script can execute stdin supplied by
			// a pipeline; keep that connected script under one approval.
			if isShellInterpreter(name) {
				return false
			}
			spec := name
			if len(unwrapped) > 1 {
				spec += " *"
			}
			// A script path is not a command prefix: `scripts/a.sh *` would
			// authorize every future argument of that script, so keep script
			// invocations exact and session-scoped instead.
			if strings.ContainsAny(name, `/\`) || !isCommandWildcardSpecifier(name+" *") {
				return false
			}
			units = append(units, shellApprovalUnit{command: bare, specifier: spec})
		}
		if local.Dynamic() {
			units = append(units, shellApprovalUnit{command: text, structures: local.Structures})
		}
		return true
	}
	for _, stmt := range file.Stmts {
		if !visit(stmt) {
			return whole()
		}
	}
	return units
}

func shellUnitCovered(unit shellApprovalUnit, command string, rules []Rule) (int, bool) {
	for i, r := range rules {
		if r.Effect != EffectAllow {
			continue
		}
		if r.Literal != "" {
			// Only a human-approved exact statement can cover a dynamic unit.
			// Session grants are stamped Source="session" after approval, so
			// both spellings of the same decision must be accepted here.
			if len(unit.structures) > 0 && r.Source != "remembered" && r.Source != "session" {
				continue
			}
			literal := strings.TrimSpace(r.Literal)
			if literal == strings.TrimSpace(command) || literal == unit.command {
				return i, true
			}
			continue
		}
		if len(unit.structures) > 0 {
			continue
		}
		if r.Specifier == "" || BashRuleMatches(r.Specifier, CallInfoForBash(unit.command), MatchAllSubcommands) {
			return i, true
		}
	}
	return -1, false
}

// ShellRulesToRemember uses the same approval units as evaluation, so saving
// one long script never discards reusable grants for its ordinary commands.
func ShellRulesToRemember(tool, command string) []Rule {
	return pendingShellRules(tool, command, nil)
}

// pendingShellRules derives the grants a "remember" decision would create.
// Ordinary commands become reusable command-prefix rules and may be persisted;
// units whose effects cannot be expressed by a prefix (dynamic structures)
// only ever authorize their exact statement for the current session, so they
// never accumulate in the trusted permission store.
func pendingShellRules(tool, command string, existing []Rule) []Rule {
	var rules []Rule
	seen := map[string]bool{}
	for _, unit := range shellApprovalUnits(command) {
		if _, covered := shellUnitCovered(unit, command, existing); covered {
			continue
		}
		r := Rule{Tool: tool, Effect: EffectAllow}
		key := "spec:" + unit.specifier
		if len(unit.structures) > 0 {
			r.Literal = unit.command
			r.Lifetime = LifetimeSession
			key = "literal:" + unit.command
		} else {
			r.Specifier = unit.specifier
		}
		if !seen[key] {
			rules = append(rules, r)
			seen[key] = true
		}
	}
	return rules
}

// PendingShellRules returns only the grants missing from the current policy.
// It does not run the auto judge or weaken explicit deny/ask precedence.
func (e *Engine) PendingShellRules(tool, command string) []Rule {
	return pendingShellRules(tool, command, e.shellAllowRules(tool, false))
}

// ApprovedShellRules explains which existing grants a partial approval reuses.
func (e *Engine) ApprovedShellRules(tool, command string) []Rule {
	rules := e.shellAllowRules(tool, false)
	var approved []Rule
	seen := map[int]bool{}
	for _, unit := range shellApprovalUnits(command) {
		if idx, covered := shellUnitCovered(unit, command, rules); covered && !seen[idx] {
			approved = append(approved, rules[idx])
			seen[idx] = true
		}
	}
	return approved
}

func (e *Engine) shellAllowRules(tool string, builtinOnly bool) []Rule {
	var rules []Rule
	for _, r := range e.rules {
		if r.Effect == EffectAllow && toolNameMatches(r.Tool, tool) && r.isBuiltin() == builtinOnly {
			rules = append(rules, r)
		}
	}
	if !builtinOnly {
		for _, r := range e.rules {
			if r.Effect == EffectAllow && toolNameMatches(r.Tool, tool) && r.isBuiltin() {
				rules = append(rules, r)
			}
		}
	}
	return rules
}

func (e *Engine) evaluateShellAllowCoverage(tool string, info map[string]any, builtinOnly bool) (Decision, bool) {
	command, _ := info["command"].(string)
	units := shellApprovalUnits(command)
	if len(units) == 0 {
		return Decision{}, false
	}
	// Preserve the ordinary single-command policy order: user allows, auto
	// judge, builtin asks, then builtin allows. Joint coverage is for scripts.
	if !builtinOnly && len(units) == 1 && len(units[0].structures) == 0 {
		return Decision{}, false
	}
	rules := e.shellAllowRules(tool, builtinOnly)
	first, firstUser := -1, -1
	for _, unit := range units {
		idx, covered := shellUnitCovered(unit, command, rules)
		if !covered {
			return Decision{}, false
		}
		if first < 0 {
			first = idx
		}
		if !rules[idx].isBuiltin() && firstUser < 0 {
			firstUser = idx
		}
	}
	for idx, rule := range rules {
		if rule.Specifier == "" && rule.Literal == "" {
			first = idx
			break
		}
	}
	if first < 0 {
		return Decision{}, false
	}
	// User-priority coverage must actually use a user grant. A collection
	// of builtin rules belongs after the judge and builtin asks, even for
	// compound commands.
	if !builtinOnly {
		if firstUser < 0 {
			return Decision{}, false
		}
		if rules[first].isBuiltin() {
			first = firstUser
		}
	}
	return Decision{Effect: EffectAllow, Rule: rules[first]}, true
}

func (e *Engine) assessShellWithRules(tool string, info map[string]any) CallAssessment {
	command, _ := info["command"].(string)
	return assessShellUnits(command, e.shellAllowRules(tool, false))
}

func assessShellUnits(command string, rules []Rule) CallAssessment {
	var assessment CallAssessment
	seen := map[ShellStructure]bool{}
	for _, unit := range shellApprovalUnits(command) {
		if _, covered := shellUnitCovered(unit, command, rules); covered {
			continue
		}
		for _, s := range unit.structures {
			if !seen[s] {
				assessment.Signals = append(assessment.Signals, string(s))
				seen[s] = true
			}
		}
	}
	if len(assessment.Signals) > 0 {
		assessment.Reason = "dynamic shell execution"
	}
	return assessment
}
