package ledger

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"nekocode/bot/governance"
)

type ToolEvent struct {
	Name      string
	Args      map[string]any
	Output    string
	Error     string
	Blocked   bool
	BlockText string
	Semantics governance.Semantics
}

type Verification struct {
	Command string
	Passed  bool
	Output  string
}

type Ledger struct {
	mu sync.RWMutex

	readFiles      map[string]bool
	modifiedFiles  map[string]bool
	blockedTools   []ToolEvent
	toolErrors     []ToolEvent
	verifications  []Verification
	toolEventCount int
}

func New() *Ledger {
	return &Ledger{
		readFiles:     make(map[string]bool),
		modifiedFiles: make(map[string]bool),
	}
}

func (l *Ledger) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.readFiles = make(map[string]bool)
	l.modifiedFiles = make(map[string]bool)
	l.blockedTools = nil
	l.toolErrors = nil
	l.verifications = nil
	l.toolEventCount = 0
}

func (l *Ledger) RecordTool(ev ToolEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.toolEventCount++
	if ev.Blocked {
		l.blockedTools = append(l.blockedTools, ev)
		return
	}
	if ev.Error != "" {
		l.toolErrors = append(l.toolErrors, ev)
	}
	if ev.Semantics.SourceProducing {
		for _, p := range extractPaths(ev.Name, ev.Args) {
			l.readFiles[p] = true
		}
	}
	if ev.Semantics.Mutating {
		for _, p := range extractPaths(ev.Name, ev.Args) {
			l.modifiedFiles[p] = true
		}
	}
	if ev.Semantics.Verifying {
		l.verifications = append(l.verifications, Verification{
			Command: commandArg(ev.Args),
			Passed:  ev.Error == "",
			Output:  ev.Output,
		})
	}
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	s := Snapshot{
		ToolEventCount: len(l.blockedTools) + len(l.toolErrors),
	}
	for p := range l.readFiles {
		s.ReadFiles = append(s.ReadFiles, p)
	}
	for p := range l.modifiedFiles {
		s.ModifiedFiles = append(s.ModifiedFiles, p)
	}
	s.BlockedTools = append(s.BlockedTools, l.blockedTools...)
	s.ToolErrors = append(s.ToolErrors, l.toolErrors...)
	s.Verifications = append(s.Verifications, l.verifications...)
	s.ToolEventCount = l.toolEventCount
	return s
}

type Snapshot struct {
	ReadFiles      []string
	ModifiedFiles  []string
	BlockedTools   []ToolEvent
	ToolErrors     []ToolEvent
	Verifications  []Verification
	ToolEventCount int
}

// WasRead checks whether a specific file path has been read (tracked in ledger).
// The path is cleaned before comparison to match ledger storage format.
func (l *Ledger) WasRead(path string) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	cleaned := filepath.Clean(path)
	return l.readFiles[cleaned]
}

func (s Snapshot) HasModifications() bool {
	return len(s.ModifiedFiles) > 0
}

func (s Snapshot) HasNonDocumentationModifications() bool {
	for _, path := range s.ModifiedFiles {
		if !isDocumentationPath(path) {
			return true
		}
	}
	return false
}

func (s Snapshot) HasPassingVerification() bool {
	for _, v := range s.Verifications {
		if v.Passed {
			return true
		}
	}
	return false
}

func (s Snapshot) Summary() string {
	return fmt.Sprintf("%d modified, %d verifications, %d tool errors, %d blocked tools",
		len(s.ModifiedFiles), len(s.Verifications), len(s.ToolErrors), len(s.BlockedTools))
}

func extractPaths(name string, args map[string]any) []string {
	switch name {
	case "read", "write", "edit":
		if p, _ := args["path"].(string); p != "" {
			return []string{filepath.Clean(p)}
		}
	case "bash":
		cmd, _ := args["command"].(string)
		return cleanPaths(extractBashReadPaths(cmd))
	}
	return nil
}

func cleanPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p != "" {
			out = append(out, filepath.Clean(p))
		}
	}
	return out
}

func commandArg(args map[string]any) string {
	cmd, _ := args["command"].(string)
	return strings.TrimSpace(cmd)
}

func extractBashReadPaths(cmd string) []string {
	fields := shellFields(strings.TrimSpace(cmd))
	if len(fields) == 0 {
		return nil
	}
	name := filepath.Base(fields[0])
	switch name {
	case "cat", "less", "more", "file", "stat":
		return nonOptionArgs(fields[1:])
	case "head", "tail", "wc":
		return nonOptionArgs(skipOptionValues(fields[1:]))
	case "find", "fd":
		return leadingPathArgs(fields[1:])
	case "rg", "grep":
		return grepPathArgs(fields[1:])
	case "git":
		return gitPathArgs(fields[1:])
	}
	return nil
}

func shellFields(s string) []string {
	var fields []string
	var b strings.Builder
	var quote rune
	escaped := false
	for _, r := range s {
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if r == ' ' || r == '\t' || r == '\n' {
			if b.Len() > 0 {
				fields = append(fields, b.String())
				b.Reset()
			}
			continue
		}
		b.WriteRune(r)
	}
	if escaped {
		b.WriteRune('\\')
	}
	if b.Len() > 0 {
		fields = append(fields, b.String())
	}
	return fields
}

func nonOptionArgs(args []string) []string {
	var out []string
	for _, a := range args {
		if a == "--" {
			continue
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		if looksLikePathArg(a) {
			out = append(out, a)
		}
	}
	return out
}

func skipOptionValues(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-n" || a == "-c" || a == "--lines" || a == "--bytes" {
			i++
			continue
		}
		if strings.HasPrefix(a, "-n") || strings.HasPrefix(a, "-c") ||
			strings.HasPrefix(a, "--lines=") || strings.HasPrefix(a, "--bytes=") {
			continue
		}
		out = append(out, a)
	}
	return out
}

func leadingPathArgs(args []string) []string {
	var out []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		if !looksLikePathArg(a) {
			break
		}
		out = append(out, a)
	}
	return out
}

func grepPathArgs(args []string) []string {
	var positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if consumesNextArg(a) {
			i++
			continue
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		positional = append(positional, a)
	}
	if len(positional) <= 1 {
		return nil
	}
	return filterPathArgs(positional[1:])
}

func gitPathArgs(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	switch args[0] {
	case "diff", "show", "blame", "log":
		for i, a := range args[1:] {
			if a == "--" {
				return filterPathArgs(args[i+2:])
			}
		}
	}
	return nil
}

func consumesNextArg(a string) bool {
	switch a {
	case "-e", "-f", "-g", "-m", "-A", "-B", "-C", "--regexp", "--file",
		"--glob", "--max-count", "--after-context", "--before-context", "--context":
		return true
	}
	return false
}

func filterPathArgs(args []string) []string {
	var out []string
	for _, a := range args {
		if looksLikePathArg(a) {
			out = append(out, a)
		}
	}
	return out
}

func looksLikePathArg(a string) bool {
	if a == "" || strings.HasPrefix(a, "-") {
		return false
	}
	return strings.Contains(a, "/") || strings.Contains(a, ".") || a == "." || a == ".."
}

func isDocumentationPath(path string) bool {
	cleaned := filepath.Clean(path)
	base := strings.ToLower(filepath.Base(cleaned))
	switch base {
	case "readme", "readme.md", "readme.mdx", "changelog", "changelog.md", "license", "license.md", "notice", "notice.md":
		return true
	}
	switch strings.ToLower(filepath.Ext(cleaned)) {
	case ".md", ".mdx", ".rst", ".adoc", ".txt":
		return true
	}
	return false
}
