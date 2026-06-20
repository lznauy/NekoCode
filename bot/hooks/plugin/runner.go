package plugin

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

func runPluginCommand(pluginRoot string, action hookAction) (output string, truncated bool, err error) {
	cmd := action.Command
	cmd = strings.ReplaceAll(cmd, "${CLAUDE_PLUGIN_ROOT}", pluginRoot)
	cmd = strings.ReplaceAll(cmd, "${PLUGIN_ROOT}", pluginRoot)

	timeout := action.Timeout
	if timeout <= 0 {
		timeout = 5000
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Millisecond)
	defer cancel()

	c := exec.CommandContext(ctx, "sh", "-c", cmd)
	c.Dir = pluginRoot
	out, runErr := c.CombinedOutput()
	raw := string(out)

	if len(raw) > maxPluginOutputBytes {
		raw = raw[:maxPluginOutputBytes] + "\n[... truncated at " + fmt.Sprint(maxPluginOutputBytes) + " bytes]"
		truncated = true
	}
	return raw, truncated, runErr
}
