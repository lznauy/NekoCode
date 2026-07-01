package diff

import (
	"context"
	"fmt"
	"os"
	"strings"

	"nekocode/bot/tools"
	"nekocode/bot/tools/core"
	"nekocode/common"
)

// Tool exposes diff computation as a callable tool for the agent.
type Tool struct{}

func NewTool() *Tool { return &Tool{} }

func (t *Tool) Name() string                                    { return "diff" }
func (t *Tool) ExecutionMode(map[string]any) core.ExecutionMode { return core.ModeParallel }
func (t *Tool) DangerLevel(map[string]any) common.DangerLevel   { return common.LevelSafe }

func (t *Tool) Description() string {
	return "Show unified diff between two texts or files. Useful for previewing changes before applying them, comparing file versions, or reviewing what an edit would change."
}

func (t *Tool) Parameters() []core.Parameter {
	return []core.Parameter{
		{
			Name:        "old",
			Type:        "string",
			Required:    true,
			Description: "Original text, or a file path prefixed with 'path:' (e.g. 'path:foo.go').",
		},
		{
			Name:        "new",
			Type:        "string",
			Required:    true,
			Description: "Modified text, or a file path prefixed with 'path:' (e.g. 'path:foo.go').",
		},
		{
			Name:        "context",
			Type:        "number",
			Required:    false,
			Description: "Number of context lines around each hunk (default 3).",
		},
		{
			Name:        "path",
			Type:        "string",
			Required:    false,
			Description: "Optional file path for display in the diff header.",
		},
	}
}

// Preview returns the actual diff content, so the tool call list shows the
// full comparison before execution (same pattern as edit's Preview).
func (t *Tool) Preview(args map[string]any) string {
	result, _ := t.computeDiff(args)
	return result
}

func (t *Tool) Execute(ctx context.Context, args map[string]any) (string, error) {
	return t.computeDiff(args)
}

func (t *Tool) computeDiff(args map[string]any) (string, error) {
	old, _ := args["old"].(string)
	new, _ := args["new"].(string)
	path, _ := args["path"].(string)

	contextLines := 3
	if c, ok := args["context"].(float64); ok {
		contextLines = int(c)
	} else if c, ok := args["context"].(int); ok {
		contextLines = c
	}
	if contextLines < 0 {
		contextLines = 0
	}

	// Resolve file paths
	old, err := resolveSource(old)
	if err != nil {
		return "", fmt.Errorf("old: %w", err)
	}
	new, err = resolveSource(new)
	if err != nil {
		return "", fmt.Errorf("new: %w", err)
	}

	// Use [path#TAG] header like edit tool for consistent rendering
	return RenderTextChange(old, new, TextChangeOptions{
		Context:      contextLines,
		Header:       TagHeader(path, "diff"),
		NoChangeText: NoChanges,
	}), nil
}

func resolveSource(s string) (string, error) {
	const prefix = "path:"
	if strings.HasPrefix(s, prefix) {
		path := strings.TrimSpace(strings.TrimPrefix(s, prefix))
		safePath, err := tools.ValidatePath(path)
		if err != nil {
			return "", fmt.Errorf("validate %s: %w", path, err)
		}
		data, err := os.ReadFile(safePath)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", path, err)
		}
		return string(data), nil
	}
	return s, nil
}
