package shell

import (
	"context"
	"strconv"
	"strings"
	"time"

	"nekocode/bot/tools/toolhelpers"
	"nekocode/bot/tools/core"
	"nekocode/common"
)

const defaultBashTimeout = 120 * time.Second

type BashTool struct{}

func (t *BashTool) Name() string                                     { return "bash" }
func (t *BashTool) ExecutionMode(map[string]any) core.ExecutionMode { return core.ModeSequential }

func (t *BashTool) Description() string {
	return "Execute shell commands. " + strconv.Itoa(int(defaultBashTimeout.Seconds())) + "s timeout by default, configurable via timeout_ms parameter (max 600s). " +
		"Shell state NOT preserved between calls (use && to chain, absolute paths instead of cd). " +
		"Prefer dedicated tools (Read, Edit, Write, Grep, Glob) — bash is a last resort when no dedicated tool fits. " +
		"Confirm OS compatibility before running distro-specific commands. " +
		"Exploratory bash (ls, cat, grep, find, git diff/log/status) consumes read quota and may be blocked when budget exhausted. " +
		"Never git push --force or skip hooks."
}

func (t *BashTool) Parameters() []core.Parameter {
	return []core.Parameter{
		{Name: "command", Type: "string", Required: true, Description: "The command to execute"},
		{Name: "timeout_ms", Type: "number", Required: false, Description: "Timeout in milliseconds (default 120000, max 600000)"},
	}
}

func (t *BashTool) DangerLevel(args map[string]any) common.DangerLevel {
	cmd, _ := args["command"].(string)
	return classifyShellDanger(strings.TrimSpace(cmd))
}

func (t *BashTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	cmdStr, err := toolhelpers.RequireStringArg(args, "command")
	if err != nil {
		return "", err
	}
	return runCommand(ctx, strings.TrimSpace(cmdStr), bashTimeout(args))
}

func bashTimeout(args map[string]any) time.Duration {
	timeout := defaultBashTimeout
	if timeoutMs, ok := args["timeout_ms"].(float64); ok && timeoutMs > 0 {
		timeout = time.Duration(timeoutMs) * time.Millisecond
		if timeout > 600*time.Second {
			timeout = 600 * time.Second
		}
	}
	return timeout
}
