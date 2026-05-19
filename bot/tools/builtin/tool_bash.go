// BashTool — execute shell commands. common.DangerLevel auto-classified by command keywords:
// forbidden (sudo/eval/ssh) -> reject, destructive (rm/kill/shutdown) -> confirm,
// write (mkdir/cp/git commit) -> confirm, rest -> auto-approve.
package builtin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"nekocode/bot/tools"

	"nekocode/common"
)

type BashTool struct{}

func (t *BashTool) Name() string                                     { return "bash" }
func (t *BashTool) ExecutionMode(map[string]any) tools.ExecutionMode { return tools.ModeSequential }

func (t *BashTool) Description() string {
	return "Execute shell commands. 120s timeout. Shell state NOT preserved between calls (use && to chain, absolute paths instead of cd). Prefer dedicated tools: Read, Edit, Write, Grep, Glob. Never git push --force or skip hooks."
}

func (t *BashTool) Parameters() []tools.Parameter {
	return []tools.Parameter{
		{Name: "command", Type: "string", Required: true, Description: "The command to execute"},
	}
}

func (t *BashTool) DangerLevel(args map[string]any) common.DangerLevel {
	cmd, _ := args["command"].(string)
	cmd = strings.TrimSpace(cmd)

	if matchAny(cmd, []string{
		"sudo", "eval", "nc ", "ncat",
		"telnet", "ssh ", "scp ", "nohup", "disown",
		"> /dev/", "mkfs", "dd ", "chown", "chmod 777",
		"| bash", "| sh", "|bash", "|sh",
	}) {
		return common.LevelForbidden
	}

	if matchAny(cmd, []string{
		"curl", "wget", "rm ", "rmdir", "chmod ", "chown ", "kill", "pkill",
		"shutdown", "reboot", "mv ", "git push", "git reset --hard",
		"docker rm", "docker rmi",
	}) {
		return common.LevelDestructive
	}

	if matchAny(cmd, []string{
		"mkdir", "touch ", "cp ", "tar ", "zip ",
		"gzip ", "git commit", "git add", "pip install", "npm install",
		"go install", "cargo install", "make ", "docker build",
	}) {
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
	"git status", "git log", "git diff", "git branch", "git show",
	"git blame", "git tag", "git remote", "git config",
	"ls", "pwd", "whoami", "date", "env", "printenv",
	"which", "type ", "uname", "hostname", "id ", "wc ",
	"cat ", "head ", "tail ", "less ", "more ",
	"du ", "df ", "free ", "uptime", "ps ", "pgrep",
	"man ", "info ", "file ", "stat ",
}

func isReadOnly(cmd string) bool {
	lower := strings.ToLower(cmd)
	for _, p := range readOnlyPrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}

func matchAny(cmd string, patterns []string) bool {
	for _, p := range patterns {
		if strings.Contains(cmd, p) || strings.HasPrefix(cmd, p) {
			return true
		}
	}
	return false
}

func (t *BashTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	cmdStr, ok := args["command"].(string)
	if !ok || cmdStr == "" {
		return "", fmt.Errorf("missing command parameter")
	}

	cmdStr = strings.TrimSpace(cmdStr)

	cmd := exec.Command("bash", "-c", cmdStr)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Dir, _ = os.Getwd()

	// Kill the entire process group on context cancellation.
	// exec.CommandContext only kills the direct child (bash), not grandchildren.
	stop := context.AfterFunc(ctx, func() {
		if cmd.Process != nil {
			syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	})
	defer stop()

	output, err := cmd.CombinedOutput()
	cleaned := tools.StripAnsi(string(output))

	if err != nil {
		return "", fmt.Errorf("command failed: %v\nOutput: %s", err, cleaned)
	}

	return cleaned, nil
}
