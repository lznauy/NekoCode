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
	return "Search file contents (rg or grep). Returns matching lines with line numbers. During directory searches rg normally skips hidden and ignored paths; the grep fallback uses recursive text search with its own filtering rules. Use context for surrounding lines, or before/after for separate counts."
}

func (t *GrepTool) Parameters() []core.Parameter {
	return []core.Parameter{
		{Name: "pattern", Type: "string", Required: true, Description: "Search pattern (regex)"},
		{Name: "path", Type: "string", Required: false, Description: "File or directory to search. Defaults to the current directory."},
		{Name: "glob", Type: "string", Required: false, Description: "File filter pattern, e.g. *.go, *.py"},
		{Name: "context", Type: "integer", Description: "Non-negative number of lines before and after each match. Default 0. Cannot combine with before/after."},
		{Name: "before", Type: "integer", Description: "Non-negative number of lines before each match. Default 0."},
		{Name: "after", Type: "integer", Description: "Non-negative number of lines after each match. Default 0."},
	}
}

func (t *GrepTool) Execute(ctx context.Context, args map[string]any) (core.ToolOutput, error) {
	pattern, err := toolutil.RequireStringArg(args, "pattern")
	if err != nil {
		return core.ToolOutput{}, err
	}

	for _, key := range []string{"path", "glob", "context_lines"} {
		if _, supplied := args[key]; supplied {
			if _, err := toolutil.StringArg(args, key); err != nil {
				return core.ToolOutput{}, err
			}
		}
	}
	contextArgs, err := grepContextArgs(args)
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

	grepArgs = append(grepArgs, contextArgs...)

	grepArgs = append(grepArgs, "--", pattern, safeBasePath)

	dir, err := workspace.WorkingDirectory(ctx)
	if err != nil {
		return core.ToolOutput{}, err
	}
	output, err := toolutil.CaptureCommand(ctx, bin, grepArgs, dir)

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			if strings.TrimSpace(output.Text) != "" {
				output.Text = "No matches reported. Search diagnostic:\n" + toolutil.StripAnsi(output.Text)
				return output, nil
			}
			return core.ToolOutput{Text: "No matches found"}, nil
		}
		return output, fmt.Errorf("grep failed: %w", err)
	}

	output.Text = toolutil.StripAnsi(output.Text)
	return output, nil
}

func grepContextArgs(args map[string]any) ([]string, error) {
	_, symmetric := args["context"]
	_, before := args["before"]
	_, after := args["after"]
	_, legacy := args["context_lines"]
	if symmetric && (before || after) || legacy && (symmetric || before || after) {
		return nil, fmt.Errorf("use context, before/after, or legacy context_lines separately")
	}
	var flags []string
	for _, field := range []struct{ key, flag string }{{"context", "-C"}, {"before", "-B"}, {"after", "-A"}} {
		if _, supplied := args[field.key]; !supplied {
			continue
		}
		n, err := toolutil.RequireIntArg(args, field.key)
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return nil, fmt.Errorf("%s must be non-negative", field.key)
		}
		flags = append(flags, field.flag, strconv.Itoa(n))
	}
	// Accept old calls from persisted conversations without advertising shell syntax.
	if value, ok := args["context_lines"].(string); ok && strings.TrimSpace(value) != "" {
		return parseContextLines(value)
	}
	return flags, nil
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
