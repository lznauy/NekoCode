package permission

import (
	"path"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

func shellCallTokens(call *syntax.CallExpr) []shellInvocationToken {
	tokens := make([]shellInvocationToken, len(call.Args))
	for i, word := range call.Args {
		value, static := staticShellWord(word)
		// find/xargs placeholders such as {} and -I{} are literal Bash
		// words, not brace expansions. Preserve them while still rejecting
		// substitutions, globs, brace lists and ranges.
		if literal := word.Lit(); !static && strings.ContainsAny(literal, "{}") &&
			!strings.ContainsAny(literal, "\\*?[~,") && !strings.Contains(literal, "..") {
			value, static = literal, true
		}
		tokens[i] = shellInvocationToken{value: value, static: static, singleWord: static || quotedIndirectWord(word)}
	}
	return tokens
}

func quotedIndirectWord(word *syntax.Word) bool {
	if len(word.Parts) != 1 {
		return false
	}
	quoted, ok := word.Parts[0].(*syntax.DblQuoted)
	if !ok {
		return false
	}
	for _, part := range quoted.Parts {
		if parameter, ok := part.(*syntax.ParamExp); ok &&
			(parameter.Param.Value == "@" || parameter.Index != nil || parameter.Exp != nil) {
			return false
		}
	}
	return true
}

// These programs execute argv embedded in their arguments. Their input can
// also supply executable names or options, so prefix grants are insufficient
// even when no static target can be recovered.
func indirectShellTargets(tokens []shellInvocationToken) ([][]shellInvocationToken, bool, bool) {
	tokens, dynamic := unwrapShellInvocation(tokens, nil)
	if dynamic || len(tokens) == 0 {
		return nil, false, true
	}
	switch strings.ToLower(path.Base(tokens[0].value)) {
	case "xargs":
		if target := xargsTarget(tokens[1:]); len(target) > 0 {
			return [][]shellInvocationToken{target}, true, staticIndirectExecutable(target)
		}
		return nil, true, false
	case "find":
		var targets [][]shellInvocationToken
		indirect := false
		understood := true
		// An unquoted operand may expand into extra predicates, including
		// -exec. A quoted scalar remains one data operand.
		operandIsDynamic := func(index int) bool {
			return index < len(tokens) && !tokens[index].singleWord
		}
		for i := 1; i < len(tokens); i++ {
			if !tokens[i].static {
				indirect = true
				understood = false
				continue
			}
			switch tokens[i].value {
			case "-exec", "-execdir", "-ok", "-okdir":
				indirect = true
				start := i + 1
				for i++; i < len(tokens); i++ {
					if tokens[i].static && (tokens[i].value == ";" || tokens[i].value == "+") {
						break
					}
				}
				if i > start {
					targets = append(targets, tokens[start:i])
					understood = understood && i < len(tokens) && staticIndirectExecutable(tokens[start:i])
				}
			case "-fprintf":
				understood = understood && i+2 < len(tokens) && !operandIsDynamic(i+1) && !operandIsDynamic(i+2)
				i += 2
			case "-name", "-iname", "-path", "-ipath", "-wholename", "-iwholename", "-regex", "-iregex",
				"-type", "-xtype", "-user", "-group", "-uid", "-gid", "-perm", "-size", "-links", "-inum",
				"-samefile", "-newer", "-anewer", "-cnewer", "-atime", "-ctime", "-mtime", "-amin", "-cmin", "-mmin",
				"-used", "-maxdepth", "-mindepth", "-regextype", "-context", "-printf", "-fprint", "-fprint0", "-fls", "-files0-from", "-D":
				understood = understood && i+1 < len(tokens) && !operandIsDynamic(i+1)
				i++ // Predicate operands are data, including a literal "-exec".
			case "-H", "-L", "-P", "-O0", "-O1", "-O2", "-O3", "-print", "-print0", "-ls", "-delete", "-prune", "-quit", "-true", "-false", "-empty", "-readable", "-writable", "-executable", "-nouser", "-nogroup", "-depth", "-mount", "-xdev", "-follow", "-daystart", "-noleaf", "-ignore_readdir_race", "-noignore_readdir_race", "-a", "-and", "-o", "-or", "!", "-not", "(", ")", ",":
			default:
				// This is a deliberately closed grammar. New predicates and GNU
				// abbreviations must not silently change operand interpretation.
				if strings.HasPrefix(tokens[i].value, "-") {
					understood = false
				}
			}
		}
		return targets, indirect || !understood, understood
	}
	return nil, false, true
}

func xargsTarget(tokens []shellInvocationToken) []shellInvocationToken {
	for i := 0; i < len(tokens); i++ {
		if !tokens[i].static {
			return nil
		}
		arg := tokens[i].value
		if arg == "--" {
			return tokens[i+1:]
		}
		if arg == "" || arg == "-" || !strings.HasPrefix(arg, "-") {
			return tokens[i:]
		}
		if strings.HasPrefix(arg, "--") {
			name, _, attached := strings.Cut(arg, "=")
			switch name {
			case "--arg-file", "--delimiter", "--max-lines", "--max-args", "--max-procs", "--max-chars", "--process-slot-var":
				if !attached {
					i++
					if i >= len(tokens) || !tokens[i].static {
						return nil
					}
				}
			case "--eof":
				// Optional values are attached with '='; the next word is argv.
			case "--null", "--no-run-if-empty", "--verbose", "--interactive", "--exit", "--show-limits", "--open-tty":
				if attached {
					return nil
				}
			default:
				return nil // Unknown options cannot be safely projected as an argv.
			}
			continue
		}
		for j := 1; j < len(arg); j++ {
			switch arg[j] {
			case 'a', 'd', 'E', 'L', 'n', 'P', 's':
				if j+1 == len(arg) {
					i++
					if i >= len(tokens) || !tokens[i].static {
						return nil
					}
				}
				j = len(arg) // Remaining characters, if any, are this option's value.
			case 'e', 'l':
				j = len(arg) // GNU's deprecated forms take only an attached optional value.
			case '0', 'r', 't', 'p', 'x', 'o':
			default:
				return nil
			}
		}
	}
	return nil
}

// The executable must be fixed independently of input/placeholders. Dynamic
// arguments still require exact approval; they never become prefix grants.
func staticIndirectExecutable(tokens []shellInvocationToken) bool {
	return len(tokens) > 0 && tokens[0].static && tokens[0].value != "" && !strings.ContainsAny(tokens[0].value, "{}")
}

// Unknown nested forms cannot be certified by a human or model approval: they
// could hide a command covered by a hard deny. Require a direct command instead.
func classifyIndirectExecution(tokens []shellInvocationToken) (indirect, unsupported bool) {
	targets, indirect, understood := indirectShellTargets(tokens)
	if indirect && !understood {
		return indirect, true
	}
	for _, target := range targets {
		if _, unsupported := classifyIndirectExecution(target); unsupported {
			return indirect, true
		}
	}
	return indirect, false
}

// Only guard matching sees these projections. Allows still cover the outer
// command and require an exact session approval for indirect execution.
func indirectGuardCommands(call *syntax.CallExpr) []string {
	queue, _, _ := indirectShellTargets(shellCallTokens(call))
	var commands []string
	for len(queue) > 0 {
		tokens := queue[0]
		queue = queue[1:]
		var words []string
		for _, token := range tokens {
			word := "${__nekocode_dynamic_argument}"
			if token.static {
				word, _ = syntax.Quote(token.value, syntax.LangBash)
			}
			words = append(words, word)
		}
		commands = append(commands, strings.Join(words, " "))
		nested, _, _ := indirectShellTargets(tokens)
		queue = append(queue, nested...)
	}
	return commands
}
