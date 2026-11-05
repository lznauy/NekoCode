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

func (t *GrepTool) Execute(ctx context.Context, args map[string]any) (core.ToolOutput, error) {
	pattern, err := toolutil.RequireStringArg(args, "pattern")
	if err != nil {
		return core.ToolOutput{}, err
	}

	basePath := toolutil.OptStringArg(args, "path", ".")
	safeBasePath, err := toolutil.ValidatePathReadableContext(ctx, basePath)
	if err != nil {
		return core.ToolOutput{}, err
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

	if ctxLines, ok := args["context_lines"].(string); ok && strings.TrimSpace(ctxLines) != "" {
		contextArgs, err := parseContextLines(ctxLines)
		if err != nil {
			return core.ToolOutput{}, err
		}
		grepArgs = append(grepArgs, contextArgs...)
	}

	grepArgs = append(grepArgs, "--", pattern, safeBasePath)

	dir, err := workspace.WorkingDirectory(ctx)
	if err != nil {
		return core.ToolOutput{}, err
	}
	output, err := toolutil.CaptureCommand(ctx, bin, grepArgs, dir)

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return core.ToolOutput{Text: "No matches found"}, nil
		}
		return output, fmt.Errorf("grep failed: %w", err)
	}

	output.Text = toolutil.StripAnsi(output.Text)
	return output, nil
}

// Only context flags are allowed: other search options can execute programs.
func parseContextLines(value string) ([]string, error) {
	fields := strings.Fields(value)
	invalid := func() ([]string, error) {
		return nil, fmt.Errorf("context_lines must contain non-negative counts or -A/-B/-C counts")
	}
	count := func(s string) bool {
		if s == "" {
			return false
		}
		for _, c := range s {
			if c < '0' || c > '9' {
				return false
			}
		}
		_, err := strconv.Atoi(s)
		return err == nil
	}
	if len(fields) == 1 && count(fields[0]) {
		return []string{"-C", fields[0]}, nil
	}
	var args []string
	for i := 0; i < len(fields); i++ {
		field := fields[i]
		if len(field) < 2 || field[0] != '-' || !strings.ContainsRune("ABC", rune(field[1])) {
			return invalid()
		}
		n := field[2:]
		if n == "" {
			i++
			if i == len(fields) {
				return invalid()
			}
			n = fields[i]
		}
		if !count(n) {
			return invalid()
		}
		args = append(args, field[:2], n)
	}
	return args, nil
}
