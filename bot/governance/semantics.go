package governance

import "strings"

type Semantics struct {
	Exploratory     bool
	Mutating        bool
	Verifying       bool
	SourceProducing bool
	Delegating      bool
	Network         bool
	Risky           bool
}

func ClassifyToolCall(name string, args map[string]any) Semantics {
	switch name {
	case "read", "grep", "glob", "list", "tree", "project_info":
		return Semantics{Exploratory: true, SourceProducing: true}
	case "web_search", "web_fetch":
		return Semantics{Exploratory: true, SourceProducing: true, Network: true}
	case "write", "edit":
		return Semantics{Mutating: true}
	case "todo_write":
		return Semantics{}
	case "task":
		return Semantics{Delegating: true, Exploratory: taskLooksExploratory(args)}
	case "bash":
		return classifyBash(args)
	default:
		return Semantics{}
	}
}

func taskLooksExploratory(args map[string]any) bool {
	t, _ := args["type"].(string)
	return t == "" || t == "researcher"
}

func classifyBash(args map[string]any) Semantics {
	cmd, _ := args["command"].(string)
	cmd = strings.TrimSpace(strings.ToLower(cmd))
	if cmd == "" {
		return Semantics{Risky: true}
	}
	sem := Semantics{}
	if BashLooksExploratory(cmd) {
		sem.Exploratory = true
		sem.SourceProducing = true
	}
	if bashLooksVerifying(cmd) {
		sem.Verifying = true
		sem.SourceProducing = true
	}
	if !sem.Verifying && bashLooksMutating(cmd) {
		sem.Mutating = true
	}
	return sem
}

func BashLooksExploratory(cmd string) bool {
	cmd = strings.TrimSpace(strings.ToLower(cmd))
	for _, p := range []string{
		"ls", "cat ", "head ", "tail ", "less ", "more ", "wc ",
		"find ", "fd ", "rg ", "grep ", "du ", "df ", "file ", "stat ",
		"pwd", "git status", "git log", "git diff", "git show", "git blame",
	} {
		if cmd == p || strings.HasPrefix(cmd, p) {
			return true
		}
	}
	return false
}

func bashLooksVerifying(cmd string) bool {
	for _, p := range []string{
		"go test", "go vet", "npm test", "npm run test", "npm run lint",
		"pnpm test", "pnpm lint", "yarn test", "yarn lint",
		"cargo test", "cargo clippy", "pytest", "python -m pytest",
		"make test", "make lint",
	} {
		if cmd == p || strings.HasPrefix(cmd, p+" ") || strings.HasPrefix(cmd, p+" ./") {
			return true
		}
	}
	return false
}

func bashLooksMutating(cmd string) bool {
	for _, p := range []string{
		"mkdir", "touch ", "cp ", "mv ", "rm ", "rmdir", "chmod ", "chown ",
		"git add", "git commit", "git reset", "npm install", "pnpm install",
		"yarn add", "go install", "cargo install", "make ",
	} {
		if cmd == p || strings.HasPrefix(cmd, p) {
			return true
		}
	}
	return strings.Contains(cmd, " > ") || strings.Contains(cmd, " >> ")
}
