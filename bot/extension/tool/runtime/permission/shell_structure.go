package permission

import (
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

// ShellStructure identifies shell constructs whose executed program cannot be
// determined safely from a flat command-prefix rule.
type ShellStructure string

const (
	ShellCommandSubstitution ShellStructure = "command_substitution"
	ShellProcessSubstitution ShellStructure = "process_substitution"
	ShellDynamicCommand      ShellStructure = "dynamic_command"
	ShellEval                ShellStructure = "eval"
	ShellSource              ShellStructure = "source"
	ShellCommandString       ShellStructure = "shell_command_string"
	ShellHeredocCode         ShellStructure = "shell_heredoc_code"
	ShellUnparseable         ShellStructure = "unparseable"
	ShellFileRedirection     ShellStructure = "file_redirection"
	ShellEnvironment         ShellStructure = "shell_environment"
)

// ShellStructureReport is a stable, ordered classification of one shell
// command. Dynamic reports whether command-level allow patterns are
// insufficient; authorization then needs an exact approval or an active
// auto judge's safe verdict for the complete command.
type ShellStructureReport struct {
	Structures []ShellStructure
}

func (r ShellStructureReport) Dynamic() bool { return len(r.Structures) > 0 }

// hasDynamicStructure reports whether any structure changes which program
// runs. A plain redirection also prevents a prefix rule from covering the
// whole call, but the command itself stays a reusable program name.
func hasDynamicStructure(structures []ShellStructure) bool {
	for _, structure := range structures {
		if structure != ShellFileRedirection {
			return true
		}
	}
	return false
}

// ClassifyShellStructure parses command as Bash and finds indirect execution
// constructs. A parse failure is classified as dynamic so a broad allow rule
// cannot silently approve syntax the permission engine could not inspect.
func ClassifyShellStructure(command string) ShellStructureReport {
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(command), "")
	if err != nil {
		return ShellStructureReport{Structures: []ShellStructure{ShellUnparseable}}
	}
	seen := map[ShellStructure]bool{}
	add := func(structure ShellStructure) {
		seen[structure] = true
	}
	syntax.Walk(file, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.CmdSubst:
			add(ShellCommandSubstitution)
		case *syntax.ProcSubst:
			add(ShellProcessSubstitution)
		case *syntax.ArithmExp, *syntax.ArithmCmd:
			// Arithmetic can assign shell variables, including through values
			// interpreted recursively as arithmetic expressions.
			add(ShellEnvironment)
		case *syntax.ParamExp:
			if n.Exp != nil && (n.Exp.Op == syntax.AssignUnset || n.Exp.Op == syntax.AssignUnsetOrNull) {
				add(ShellEnvironment)
			}
		case *syntax.CallExpr:
			classifyShellCall(n, add)
		case *syntax.DeclClause:
			// export/readonly/declare can alter the execution environment
			// of later commands, but are not represented as CallExpr nodes.
			add(ShellEnvironment)
		case *syntax.Stmt:
			for _, redir := range n.Redirs {
				switch redir.Op {
				case syntax.RdrOut, syntax.AppOut, syntax.ClbOut, syntax.RdrInOut, syntax.RdrAll, syntax.AppAll:
					add(ShellFileRedirection)
				case syntax.DplOut:
					// Bash also accepts >&file. Only a known fd duplication,
					// move, or close is guaranteed not to open a file.
					target, known := staticShellWord(redir.Word)
					if known && target == "-" {
						continue
					}
					target = strings.TrimSuffix(target, "-")
					isFD := known && target != ""
					for _, r := range target {
						if r < '0' || r > '9' {
							isFD = false
						}
					}
					if !isFD {
						add(ShellFileRedirection)
					}
				}
			}
			if stmtRunsShellHeredoc(n) {
				add(ShellHeredocCode)
			}
		}
		return true
	})
	order := []ShellStructure{
		ShellCommandSubstitution,
		ShellProcessSubstitution,
		ShellDynamicCommand,
		ShellEval,
		ShellSource,
		ShellCommandString,
		ShellHeredocCode,
		ShellFileRedirection,
		ShellEnvironment,
	}
	report := ShellStructureReport{}
	for _, structure := range order {
		if seen[structure] {
			report.Structures = append(report.Structures, structure)
		}
	}
	return report
}

func classifyShellCall(call *syntax.CallExpr, add func(ShellStructure)) {
	if len(call.Assigns) != 0 {
		add(ShellEnvironment)
	}
	if len(call.Args) == 0 {
		return
	}
	if _, ok := staticShellWord(call.Args[0]); !ok {
		add(ShellDynamicCommand)
		return
	}
	name, args, ok, dynamic := effectiveShellCall(call, add)
	if dynamic {
		add(ShellDynamicCommand)
		return
	}
	if !ok {
		return
	}
	switch name {
	case "eval":
		add(ShellEval)
	case "source", ".":
		add(ShellSource)
	}
	if isShellInterpreter(name) && shellArgsExecuteString(args) {
		add(ShellCommandString)
	}
}

func effectiveShellCall(call *syntax.CallExpr, add func(ShellStructure)) (string, []string, bool, bool) {
	tokens := make([]shellInvocationToken, len(call.Args))
	for i, word := range call.Args {
		value, ok := staticShellWord(word)
		tokens[i] = shellInvocationToken{value: value, static: ok}
	}
	tokens, dynamic := unwrapShellInvocation(tokens, add)
	if dynamic {
		return "", nil, false, true
	}
	if len(tokens) == 0 {
		return "", nil, false, false
	}
	if !tokens[0].static {
		return "", nil, false, true
	}
	if add != nil && strings.ToLower(tokens[0].value) == "printf" {
		// printf is a shell builtin; -v mutates shell variables. Match its
		// name case-insensitively like wrapper resolution.
		for _, token := range tokens[1:] {
			if !token.static {
				add(ShellEnvironment)
				break
			}
			if token.value == "--" {
				break
			}
			if strings.HasPrefix(token.value, "-v") {
				add(ShellEnvironment)
				break
			}
			if !strings.HasPrefix(token.value, "-") {
				break
			}
		}
	}
	args := make([]string, 0, len(tokens)-1)
	for _, token := range tokens[1:] {
		// An unknown interpreter option may expand to -c (or select code
		// from stdin). Do not erase that uncertainty before classification.
		if !token.static && isShellInterpreter(tokens[0].value) {
			return "", nil, false, true
		}
		args = append(args, token.value)
	}
	return strings.ToLower(tokens[0].value), args, true, false
}

type shellInvocationToken struct {
	value  string
	static bool
}

// unwrapShellInvocation resolves command wrappers through one shared,
// option-aware model. Both structural classification and Bash rule matching
// use this function, so adding a wrapper cannot create two conflicting views
// of the command that will actually execute.
func unwrapShellInvocation(tokens []shellInvocationToken, add func(ShellStructure)) ([]shellInvocationToken, bool) {
	for len(tokens) > 0 {
		if !tokens[0].static {
			return nil, true
		}
		var next int
		var wrapped, dynamic bool
		// Bash resolves commands case-sensitively, but wrappers are matched
		// against their canonical spelling: EXEC/TIMEOUT must not become a
		// distinct command that bypasses the wrapper rules below.
		wrapper := strings.ToLower(filepath.Base(tokens[0].value))
		switch wrapper {
		case "env":
			next, dynamic = unwrapEnv(tokens)
			wrapped = true
		case "command":
			next, dynamic = unwrapOptions(tokens, map[string]bool{"-p": false, "-v": false, "-V": false})
			wrapped = true
		case "exec":
			next, dynamic = unwrapOptions(tokens, map[string]bool{"-a": true, "-c": false, "-l": false})
			wrapped = true
		case "builtin":
			next, dynamic = unwrapOptions(tokens, map[string]bool{"-p": false})
			wrapped = true
		case "nohup":
			next, dynamic = unwrapOptions(tokens, map[string]bool{"--help": false, "--version": false})
			wrapped = true
		case "busybox", "toybox":
			next, dynamic = unwrapOptions(tokens, nil)
			wrapped = true
		case "timeout":
			next, dynamic = unwrapTimeout(tokens)
			wrapped = true
		case "nice":
			next, dynamic = unwrapNice(tokens)
			wrapped = true
		case "time":
			next, dynamic = unwrapOptions(tokens, map[string]bool{
				"-p": false, "--portability": false, "-a": false, "--append": false,
				"-v": false, "--verbose": false, "-f": true, "--format": true,
				"-o": true, "--output": true,
			})
			wrapped = true
		case "stdbuf":
			next, dynamic = unwrapStdbuf(tokens)
			wrapped = true
		}
		if !wrapped {
			return tokens, false
		}
		if dynamic {
			return nil, true
		}
		if next <= 0 || next >= len(tokens) {
			return nil, false
		}
		if add != nil {
			for _, token := range tokens[1:next] {
				if wrapper == "env" && strings.Contains(token.value, "=") {
					// Environment settings can load code or change which program
					// runs. A command-prefix rule does not approve those effects.
					add(ShellEnvironment)
				}
				if wrapper == "time" && (token.value == "-o" || token.value == "--output") {
					add(ShellFileRedirection)
				}
			}
		}
		tokens = tokens[next:]
	}
	return nil, false
}

func unwrapEnv(tokens []shellInvocationToken) (int, bool) {
	operands := false
	for i := 1; i < len(tokens); i++ {
		if !tokens[i].static {
			return 0, true
		}
		arg := tokens[i].value
		if operands {
			if strings.Contains(arg, "=") {
				continue
			}
			return i, false
		}
		switch {
		case arg == "--":
			operands = true
		case arg == "-S" || arg == "--split-string" || strings.HasPrefix(arg, "--split-string="):
			// env parses this argument into a new argv at runtime. Treat it as
			// indirect execution instead of trying to emulate coreutils parsing.
			return 0, true
		case arg == "-u" || arg == "--unset" || arg == "-C" || arg == "--chdir":
			if i+1 >= len(tokens) {
				return len(tokens), false
			}
			i++
		case strings.HasPrefix(arg, "-u") && len(arg) > 2,
			strings.HasPrefix(arg, "--unset="), strings.HasPrefix(arg, "--chdir="),
			arg == "-i", arg == "--ignore-environment", arg == "-0", arg == "--null",
			arg == "-v", arg == "--debug":
			continue
		case strings.HasPrefix(arg, "-"):
			return 0, true
		case strings.Contains(arg, "="):
			operands = true
		default:
			return i, false
		}
	}
	return len(tokens), false
}

func unwrapOptions(tokens []shellInvocationToken, options map[string]bool) (int, bool) {
	for i := 1; i < len(tokens); i++ {
		if !tokens[i].static {
			return 0, true
		}
		arg := tokens[i].value
		if arg == "--" {
			return i + 1, false
		}
		needsValue, known := options[arg]
		if !known {
			if strings.HasPrefix(arg, "-") {
				return 0, true
			}
			return i, false
		}
		if needsValue {
			if i+1 >= len(tokens) {
				return len(tokens), false
			}
			i++
		}
	}
	return len(tokens), false
}

func unwrapTimeout(tokens []shellInvocationToken) (int, bool) {
	i, dynamic := unwrapOptions(tokens, map[string]bool{
		"--foreground": false, "--preserve-status": false, "--verbose": false,
		"-k": true, "--kill-after": true, "-s": true, "--signal": true,
	})
	if dynamic || i >= len(tokens) {
		return i, dynamic
	}
	// timeout requires a duration before the command.
	return i + 1, false
}

func unwrapNice(tokens []shellInvocationToken) (int, bool) {
	for i := 1; i < len(tokens); i++ {
		if !tokens[i].static {
			return 0, true
		}
		arg := tokens[i].value
		switch {
		case arg == "--":
			return i + 1, false
		case arg == "-n" || arg == "--adjustment":
			if i+1 >= len(tokens) {
				return len(tokens), false
			}
			i++
		case strings.HasPrefix(arg, "-n") && len(arg) > 2,
			strings.HasPrefix(arg, "--adjustment="), isSignedNumber(arg):
			continue
		case strings.HasPrefix(arg, "-"):
			return 0, true
		default:
			return i, false
		}
	}
	return len(tokens), false
}

func unwrapStdbuf(tokens []shellInvocationToken) (int, bool) {
	for i := 1; i < len(tokens); i++ {
		if !tokens[i].static {
			return 0, true
		}
		arg := tokens[i].value
		if arg == "--" {
			return i + 1, false
		}
		if arg == "-i" || arg == "-o" || arg == "-e" {
			if i+1 >= len(tokens) {
				return len(tokens), false
			}
			i++
			continue
		}
		if strings.HasPrefix(arg, "-i") || strings.HasPrefix(arg, "-o") || strings.HasPrefix(arg, "-e") ||
			strings.HasPrefix(arg, "--input=") || strings.HasPrefix(arg, "--output=") || strings.HasPrefix(arg, "--error=") {
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return 0, true
		}
		return i, false
	}
	return len(tokens), false
}

func isSignedNumber(value string) bool {
	if len(value) < 2 || (value[0] != '-' && value[0] != '+') {
		return false
	}
	for _, r := range value[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func staticShellWord(word *syntax.Word) (string, bool) {
	if word == nil {
		return "", false
	}
	var out strings.Builder
	var appendParts func([]syntax.WordPart, bool) bool
	appendParts = func(parts []syntax.WordPart, doubleQuoted bool) bool {
		for _, part := range parts {
			switch p := part.(type) {
			case *syntax.Lit:
				for i := 0; i < len(p.Value); i++ {
					b := p.Value[i]
					if b == '\\' && i+1 < len(p.Value) {
						next := p.Value[i+1]
						if !doubleQuoted || strings.ContainsRune("$`\"\\\n", rune(next)) {
							i++
							if next != '\n' {
								out.WriteByte(next)
							}
							continue
						}
					}
					// These unquoted characters can expand to different words.
					// Leave their spelling intact for matching and treat an
					// executable containing them as dynamically selected.
					if !doubleQuoted && strings.ContainsRune("*?[{}~", rune(b)) {
						return false
					}
					out.WriteByte(b)
				}
			case *syntax.SglQuoted:
				value := p.Value
				if p.Dollar {
					// Expand only this quoted literal; never pass substitutions
					// to the expansion package during permission analysis.
					var err error
					value, err = expand.Literal(nil, &syntax.Word{Parts: []syntax.WordPart{p}})
					if err != nil {
						return false
					}
					if i := strings.IndexByte(value, 0); i >= 0 {
						value = value[:i]
					}
				}
				out.WriteString(value)
			case *syntax.DblQuoted:
				// Locale-dependent translations cannot be predicted statically.
				if p.Dollar || !appendParts(p.Parts, true) {
					return false
				}
			default:
				return false
			}
		}
		return true
	}
	if !appendParts(word.Parts, false) {
		return "", false
	}
	return out.String(), true
}

func isShellInterpreter(name string) bool {
	name = strings.ToLower(filepath.Base(name))
	switch name {
	case "bash", "sh", "dash", "zsh", "ksh", "mksh":
		return true
	default:
		return false
	}
}

func shellArgsExecuteString(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.Contains(arg[1:], "c") {
			return true
		}
	}
	return false
}

func stmtRunsShellHeredoc(stmt *syntax.Stmt) bool {
	if stmt == nil || len(stmt.Redirs) == 0 {
		return false
	}
	hasHeredoc := false
	for _, redirect := range stmt.Redirs {
		if redirect.Op == syntax.Hdoc || redirect.Op == syntax.DashHdoc || redirect.Op == syntax.WordHdoc {
			hasHeredoc = true
			break
		}
	}
	if !hasHeredoc {
		return false
	}
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok {
		return false
	}
	name, _, ok, _ := effectiveShellCall(call, nil)
	return ok && isShellInterpreter(name)
}
