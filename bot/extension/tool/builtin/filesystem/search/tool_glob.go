package search

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/toolutil"
)

type GlobTool struct {
	toolutil.SafeReadOnlyTool
}

func (t *GlobTool) Name() string { return "glob" }

func (t *GlobTool) Description() string {
	return "Find files matching a glob pattern. Supports ** for recursive directory search (e.g. \"src/**/*.go\"). Returns newline-separated file paths."
}

func (t *GlobTool) Parameters() []core.Parameter {
	return []core.Parameter{
		{Name: "pattern", Type: "string", Required: true, Description: "Glob pattern, e.g. \"*.go\" or \"src/**/*.md\". ** matches zero or more directories."},
		{Name: "path", Type: "string", Required: false, Description: "Base directory for the search (default: current working directory)"},
	}
}

func (t *GlobTool) Execute(ctx context.Context, args map[string]any) (core.ToolOutput, error) {
	pattern, err := toolutil.RequireStringArg(args, "pattern")
	if err != nil {
		return core.ToolOutput{}, err
	}

	basePath := toolutil.OptStringArg(args, "path", ".")
	safeBasePath, err := toolutil.ValidatePathReadableContext(ctx, basePath)
	if err != nil {
		return core.ToolOutput{}, err
	}

	if filepath.IsAbs(pattern) || filepath.VolumeName(pattern) != "" {
		return core.ToolOutput{}, fmt.Errorf("glob pattern must be relative to path")
	}
	pattern = filepath.ToSlash(pattern)
	for _, segment := range strings.Split(pattern, "/") {
		if segment == ".." {
			return core.ToolOutput{}, fmt.Errorf("glob pattern must not contain parent traversal")
		}
		if _, err := path.Match(segment, ""); err != nil {
			return core.ToolOutput{}, fmt.Errorf("glob failed: %w", err)
		}
	}
	pattern = path.Clean(pattern)
	if err := ctx.Err(); err != nil {
		return core.ToolOutput{}, err
	}
	// Root's filesystem rejects escaping symlinks while opening directories,
	// including replacements during traversal, before their names can be read.
	root, err := os.OpenRoot(safeBasePath)
	if err != nil {
		return core.ToolOutput{}, fmt.Errorf("glob failed: %w", err)
	}
	defer root.Close()
	var matches []string
	if strings.Contains(pattern, "**") {
		matches, err = globRecursive(ctx, root.FS(), strings.Split(pattern, "/"))
	} else {
		matches, err = fs.Glob(root.FS(), pattern)
	}
	if err != nil {
		return core.ToolOutput{}, fmt.Errorf("glob failed: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return core.ToolOutput{}, err
	}

	if len(matches) == 0 {
		return core.ToolOutput{Text: "No matching files found"}, nil
	}

	var sb strings.Builder
	for _, m := range matches {
		sb.WriteString(filepath.Join(safeBasePath, filepath.FromSlash(m)))
		sb.WriteByte('\n')
	}
	return core.ToolOutput{Text: sb.String()}, nil
}

func globRecursive(ctx context.Context, filesystem fs.FS, pattern []string) ([]string, error) {
	var matches []string
	err := fs.WalkDir(filesystem, ".", func(name string, entry fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			if name == "." {
				return err
			}
			return nil
		}
		if name != "." && matchGlobParts(name, pattern) {
			matches = append(matches, name)
		}
		return nil
	})
	return matches, err
}

// matchGlobParts matches whole path components; ** consumes zero or more levels.
func matchGlobParts(name string, pattern []string) bool {
	names := strings.Split(name, "/")
	// Dynamic programming avoids exponential backtracking with repeated **.
	matched := make([]bool, len(names)+1)
	matched[0] = true
	for _, segment := range pattern {
		next := make([]bool, len(names)+1)
		if segment == "**" {
			next[0] = matched[0]
			for j := 1; j <= len(names); j++ {
				next[j] = matched[j] || next[j-1]
			}
		} else {
			for j := 1; j <= len(names); j++ {
				ok, _ := path.Match(segment, names[j-1])
				next[j] = matched[j-1] && ok
			}
		}
		matched = next
	}
	return matched[len(names)]
}
