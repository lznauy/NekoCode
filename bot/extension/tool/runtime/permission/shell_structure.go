package permission

import (
	"path/filepath"
	"strings"

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
)

// ShellStructureReport is a stable, ordered classification of one shell
// command. Dynamic reports whether command-level allow patterns are
// insufficient; authorization then needs an exact approval or an active
// auto judge's safe verdict for the complete command.
type ShellStructureReport struct {
	Structures []ShellStructure
}

func (r ShellStructureReport) Dynamic() bool { return len(r.Structures) > 0 }

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
		case *syntax.CallExpr:
			classifyShellCall(n, add)
		case *syntax.Stmt:
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
	if len(call.Args) == 0 {
		return
	}
	if _, ok := staticShellWord(call.Args[0]); !ok {
		add(ShellDynamicCommand)
		return
	}
	name, args, ok, dynamic := effectiveShellCall(call)
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

func effectiveShellCall(call *syntax.CallExpr) (string, []string, bool, bool) {
	tokens := make([]shellInvocationToken, len(call.Args))
	for i, word := range call.Args {
		value, ok := staticShellWord(word)
		tokens[i] = shellInvocationToken{value: value, static: ok}
	}
	tokens, dynamic := unwrapShellInvocation(tokens)
	if dynamic {
		return "", nil, false, true
	}
	if len(tokens) == 0 {
		return "", nil, false, false
	}
	if !tokens[0].static {
		return "", nil, false, true
	}
	args := make([]string, 0, len(tokens)-1)
	for _, token := range tokens[1:] {
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
func unwrapShellInvocation(tokens []shellInvocationToken) ([]shellInvocationToken, bool) {
	for len(tokens) > 0 {
		if !tokens[0].static {
			return nil, true
		}
		var next int
		var wrapped, dynamic bool
		switch strings.ToLower(filepath.Base(tokens[0].value)) {
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
		tokens = tokens[next:]
	}
	return nil, false
}

func unwrapEnv(tokens []shellInvocationToken) (int, bool) {
	for i := 1; i < len(tokens); i++ {
		if !tokens[i].static {
			return 0, true
		}
		arg := tokens[i].value
		switch {
		case arg == "--":
			return i + 1, false
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
			arg == "-v", arg == "--debug", strings.Contains(arg, "="):
			continue
		case strings.HasPrefix(arg, "-"):
			return 0, true
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
	var appendParts func([]syntax.WordPart) bool
	appendParts = func(parts []syntax.WordPart) bool {
		for _, part := range parts {
			switch p := part.(type) {
			case *syntax.Lit:
				out.WriteString(p.Value)
			case *syntax.SglQuoted:
				out.WriteString(p.Value)
			case *syntax.DblQuoted:
				if !appendParts(p.Parts) {
					return false
				}
			default:
				return false
			}
		}
		return true
	}
	if !appendParts(word.Parts) {
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
	name, _, ok, _ := effectiveShellCall(call)
	return ok && isShellInterpreter(name)
}
