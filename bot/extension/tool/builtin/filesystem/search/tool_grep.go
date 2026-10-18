// GrepTool — content search via ripgrep. Returns matching lines with line numbers. Supports regex and context lines.
package search

import (
	"context"
	"fmt"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/toolutil"
	"nekocode/bot/extension/tool/runtime/workspace"
	"os/exec"
	"strconv"
	"strings"
)

type GrepTool struct {
	toolutil.SafeReadOnlyTool
}

func (t *GrepTool) Name() string { return "grep" }
func (t *GrepTool) Description() string {
	return "Search file contents (rg or grep). Returns matching lines with line numbers. Supports regex, glob filtering, and context lines (-A/-B/-C)."
}

func (t *GrepTool) Parameters() []core.Parameter {
	return []core.Parameter{
		{Name: "pattern", Type: "string", Required: true, Description: "Search pattern (regex)"},
		{Name: "path", Type: "string", Required: false, Description: "Directory to search, default: current directory"},
		{Name: "glob", Type: "string", Required: false, Description: "File filter pattern, e.g. *.go, *.py"},
		{Name: "context_lines", Type: "string", Required: false, Description: "Context lines, e.g. -A3, -B2, -C5 or a number (default -C)"},
	}
}

func (t *GrepTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	pattern, err := toolutil.RequireStringArg(args, "pattern")
	if err != nil {
		return "", err
	}

	basePath := toolutil.OptStringArg(args, "path", ".")
	safeBasePath, err := toolutil.ValidatePathReadableContext(ctx, basePath)
	if err != nil {
		return "", err
	}

	bin := "rg"
	grepArgs := []string{"-n"}
	if _, err := exec.LookPath("rg"); err != nil {
		bin = "grep"
		grepArgs = append(grepArgs, "-E", "-r", "-I")
	} else {
		grepArgs = append(grepArgs, "--no-heading")
	}

	if glob, ok := args["glob"].(string); ok && glob != "" {
		if bin == "rg" {
			grepArgs = append(grepArgs, "--glob", glob)
		} else {
			grepArgs = append(grepArgs, "--include="+glob)
		}
	}

	if ctxLines, ok := args["context_lines"].(string); ok && ctxLines != "" {
		ctxLines = strings.TrimSpace(ctxLines)
		if _, err := strconv.Atoi(ctxLines); err == nil {
			grepArgs = append(grepArgs, "-C", ctxLines)
		} else {
			grepArgs = append(grepArgs, strings.Fields(ctxLines)...)
		}
	}

	grepArgs = append(grepArgs, "--", pattern, safeBasePath)

	cmd := exec.CommandContext(ctx, bin, grepArgs...)
	cmd.Dir, err = workspace.WorkingDirectory(ctx)
	if err != nil {
		return "", err
	}
	output, err := cmd.CombinedOutput()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "No matches found", nil
		}
		return "", fmt.Errorf("grep failed: %w\nOutput: %s", err, string(output))
	}

	return toolutil.StripAnsi(string(output)), nil
}
