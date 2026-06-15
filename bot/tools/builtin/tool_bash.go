// BashTool — execute shell commands. common.DangerLevel auto-classified by command keywords:
// forbidden (sudo/eval/ssh) -> reject, destructive (rm/kill/shutdown) -> confirm,
// write (mkdir/cp/git commit) -> confirm, rest -> auto-approve.
package builtin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"nekocode/bot/tools"

	"nekocode/common"
)

const defaultBashTimeout = 120 * time.Second

type BashTool struct{}

func (t *BashTool) Name() string                                     { return "bash" }
func (t *BashTool) ExecutionMode(map[string]any) tools.ExecutionMode { return tools.ModeSequential }

func (t *BashTool) Description() string {
	return "Execute shell commands. " + strconv.Itoa(int(defaultBashTimeout.Seconds())) + "s timeout by default, configurable via timeout_ms parameter (max 600s). Shell state NOT preserved between calls (use && to chain, absolute paths instead of cd). Prefer dedicated tools: Read, Edit, Write, Grep, Glob. Never git push --force or skip hooks."
}

func (t *BashTool) Parameters() []tools.Parameter {
	return []tools.Parameter{
		{Name: "command", Type: "string", Required: true, Description: "The command to execute"},
		{Name: "timeout_ms", Type: "number", Required: false, Description: "Timeout in milliseconds (default 120000, max 600000)"},
	}
}

func (t *BashTool) DangerLevel(args map[string]any) common.DangerLevel {
	cmd, _ := args["command"].(string)
	cmd = strings.TrimSpace(cmd)
	// Strip heredoc bodies before keyword matching so that source code
	// embedded in heredocs (e.g. "func " matching "nc ") does not
	// cause false-positive forbidden classifications.
	cmdForMatch := stripHeredocBodies(cmd)

	if matchAny(cmdForMatch, []string{
		"sudo", "eval", "nc ", "ncat",
		"telnet", "ssh ", "scp ", "nohup", "disown",
		"> /dev/", "mkfs", "dd ", "chmod 777",
		"| bash", "| sh", "|bash", "|sh",
	}) {
		return common.LevelForbidden
	}

	if matchAny(cmdForMatch, []string{
		"curl", "wget", "rm ", "rmdir", "chmod ", "chown ", "kill", "pkill",
		"shutdown", "reboot", "mv ", "git push", "git reset --hard",
		"git branch -d", "git branch -D",
		"git config --global", "git config --system", "git config --local",
		"git config --replace-all", "git config --unset", "git config --edit",
		"docker rm", "docker rmi",
	}) {
		return common.LevelDestructive
	}

	if matchAny(cmdForMatch, []string{
		"mkdir", "touch ", "cp ", "tar ", "zip ",
		"gzip ", "git commit", "git add", "pip install", "npm install",
		"go install", "cargo install", "make ", "docker build",
	}) {
		return common.LevelWrite
	}

	// Detect shell redirection that writes to files ( > path) — uses
	// quoted-stripped variant so " > " inside a string is not flagged.
	if hasWriteRedirection(cmdForMatch) {
		return common.LevelWrite
	}

	// Commands that only produce output — no file system changes.
	if isReadOnly(cmd) {
		return common.LevelSafe
	}

	return common.LevelWrite
}

var readOnlyPrefixes = []string{
	"go version", "go env", "go doc", "go vet", "go fmt",
	"git status", "git log", "git diff", "git show",
	"git blame", "git tag", "git remote",
	"pwd", "whoami", "date", "printenv",
	"which", "uname", "hostname", "wc ",
	"cat ", "head ", "tail ", "less ", "more ",
	"du ", "df ", "free ", "uptime", "pgrep",
	"man ", "info ", "file ", "stat ",
}

// readOnlyCommands are single-word commands that are safe even without arguments.
// Matched as exact word (command name followed by space or end-of-string) to
// avoid false positives like "ls" matching "lsblk" or "env" matching "envsubst".
var readOnlyCommands = []string{
	"ls", "env", "id", "ps", "type",
}

func isReadOnly(cmd string) bool {
	lower := strings.ToLower(cmd)
	for _, p := range readOnlyPrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	for _, c := range readOnlyCommands {
		if lower == c || strings.HasPrefix(lower, c+" ") {
			return true
		}
	}
	return false
}

func matchAny(cmd string, patterns []string) bool {
	for _, p := range patterns {
		if strings.Contains(cmd, p) {
			return true
		}
	}
	return false
}

// stripQuotedSegments replaces single-quoted and double-quoted segments with
// spaces of the same length, so that shell keywords inside quotes are not
// mistaken for operators (e.g. " > " inside a string literal).
func stripQuotedSegments(cmd string) string {
	out := make([]byte, 0, len(cmd))
	i := 0
	for i < len(cmd) {
		ch := cmd[i]
		if ch == '\'' {
			out = append(out, ' ') // replace opening quote
			i++
			for i < len(cmd) && cmd[i] != '\'' {
				out = append(out, ' ')
				i++
			}
			if i < len(cmd) {
				out = append(out, ' ') // replace closing quote
				i++
			}
		} else if ch == '"' {
			out = append(out, ' ') // replace opening quote
			i++
			for i < len(cmd) && cmd[i] != '"' {
				if cmd[i] == '\\' && i+1 < len(cmd) {
					out = append(out, ' ', ' ') // escape + escaped char
					i += 2
				} else {
					out = append(out, ' ')
					i++
				}
			}
			if i < len(cmd) {
				out = append(out, ' ') // replace closing quote
				i++
			}
		} else {
			out = append(out, ch)
			i++
		}
	}
	return string(out)
}

// stripHeredocBodies truncates cmd at the first here-doc marker so that
// heredoc body content does not pollute keyword matching.
func stripHeredocBodies(cmd string) string {
	clean := stripQuotedSegments(cmd)
	if idx := strings.Index(clean, "<<"); idx >= 0 {
		return cmd[:idx]
	}
	// Fallback: << inside a double-quoted shell wrapper (bash "cmd << 'EOF'")
	// is masked by stripQuotedSegments. Only truncate when the rest of the
	// command is multi-line — single-line quoted << (echo "a<<b") is not a
	// here-doc and must be left intact.
	if idx := strings.Index(cmd, "<<"); idx >= 0 && strings.IndexByte(cmd[idx:], '\n') >= 0 {
		return cmd[:idx]
	}
	return cmd
}

// isWriteRedirect returns true when the token at position idx in cmd is a
// shell output redirect (>, >>, or 2>) that writes to a real file — not to
// /dev/null or another /dev/ pseudo-file.
func isWriteRedirect(cmd string, idx int, tokLen int) bool {
	rest := strings.TrimSpace(cmd[idx+tokLen:])
	if rest == "" {
		return false
	}
	return !strings.HasPrefix(rest, "/dev/null") && !strings.HasPrefix(rest, "/dev/")
}

// hasWriteRedirection returns true when cmd contains a shell redirect
// that writes to a regular file (not /dev/null or /dev/).
func hasWriteRedirection(cmd string) bool {
	clean := stripQuotedSegments(cmd)
	// Spaced tokens match standard redirects like "cmd > /path",
	// "cmd >> /path", "cmd 2> /path", "cmd &> /path", "cmd 1> /path".
	spacedToks := []string{" > ", ">> ", "2> ", " &> ", "1> "}
	for _, tok := range spacedToks {
		pos := 0
		for {
			idx := strings.Index(clean[pos:], tok)
			if idx < 0 {
				break
			}
			idx += pos
			if isWriteRedirect(clean, idx, len(tok)) {
				return true
			}
			pos = idx + 1
		}
	}
	// Compact tokens match "cmd>/path", "cmd>>file", "cmd2>/path",
	// "cmd&>/path", "cmd1>/path" (no space between operator and target).
	compactToks := []string{">", ">>", "2>", "&>", "1>"}
	for _, tok := range compactToks {
		pos := 0
		for {
			idx := strings.Index(clean[pos:], tok)
			if idx < 0 {
				break
			}
			idx += pos
			next := idx + len(tok)
			// If the char after the token is a space, the spaced variant already
			// handled it (or it's a legit non-redirect like "cmd2 > file").
			if next >= len(clean) || clean[next] == ' ' {
				pos = idx + 1
				continue
			}
			if isWriteRedirect(clean, idx, len(tok)) {
				return true
			}
			pos = idx + 1
		}
	}
	// Catch leading redirect: "> file", ">> file" (spaced form not caught elsewhere).
	for _, prefix := range []string{"> ", ">> "} {
		if !strings.HasPrefix(clean, prefix) {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(clean, prefix))
		if rest != "" && !strings.HasPrefix(rest, "/dev/null") && !strings.HasPrefix(rest, "/dev/") {
			return true
		}
	}
	return false
}

func (t *BashTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	cmdStr, err := requireStringArg(args, "command")
	if err != nil {
		return "", err
	}

	cmdStr = strings.TrimSpace(cmdStr)

	// Parse timeout from args, default 120s, max 600s.
	timeout := defaultBashTimeout
	if timeoutMs, ok := args["timeout_ms"].(float64); ok && timeoutMs > 0 {
		timeout = time.Duration(timeoutMs) * time.Millisecond
		if timeout > 600*time.Second {
			timeout = 600 * time.Second
		}
	}

	// Create a context with the configured timeout, inheriting from parent ctx.
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "bash", "-c", cmdStr)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if dir, err := os.Getwd(); err == nil {
		cmd.Dir = dir
	}

	// Kill the entire process group on context cancellation.
	// exec.CommandContext only kills the direct child (bash), not grandchildren.
	stop := context.AfterFunc(cmdCtx, func() {
		if cmd.Process != nil {
			syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	})
	defer stop()

	output, err := cmd.CombinedOutput()
	cleaned := tools.StripAnsi(string(output))

	if err != nil {
		if cmdCtx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("command timed out after %v: %v\nOutput: %s", timeout, err, cleaned)
		}
		return "", fmt.Errorf("command failed: %v\nOutput: %s", err, cleaned)
	}

	return cleaned, nil
}
