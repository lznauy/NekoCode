package search

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

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

	if _, supplied := args["path"]; supplied {
		if _, err := toolutil.StringArg(args, "path"); err != nil {
			return core.ToolOutput{}, err
		}
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
		matches, err = globNonRecursive(ctx, root.FS(), pattern)
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

// fs.Glob deliberately ignores I/O errors. Capture them at its FS boundary so
// a permission failure cannot be presented to the model as an empty search.
func globNonRecursive(ctx context.Context, filesystem fs.FS, pattern string) ([]string, error) {
	checked := &checkedGlobFS{FS: filesystem, ctx: ctx}
	matches, err := fs.Glob(checked, pattern)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if checked.err != nil {
		return nil, checked.err
	}
	return matches, err
}

type checkedGlobFS struct {
	fs.FS
	ctx context.Context
	err error
}

func (f *checkedGlobFS) record(name string, err error) {
	// Absent paths and attempts to descend into a file are ordinary non-matches.
	if err != nil && !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR) && f.err == nil {
		f.err = fmt.Errorf("glob search incomplete at %s: %w", name, err)
	}
}

func (f *checkedGlobFS) Stat(name string) (fs.FileInfo, error) {
	if err := f.ctx.Err(); err != nil {
		return nil, err
	}
	info, err := fs.Stat(f.FS, name)
	f.record(name, err)
	return info, err
}

func (f *checkedGlobFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if err := f.ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := fs.ReadDir(f.FS, name)
	f.record(name, err)
	return entries, err
}

func globRecursive(ctx context.Context, filesystem fs.FS, pattern []string) ([]string, error) {
	// Directories that cannot satisfy the fixed segments before the first **
	// are pruned without being read, so read failures in unrelated subtrees
	// cannot abort a scoped search.
	prefix := pattern
	if i := slices.Index(pattern, "**"); i >= 0 {
		prefix = pattern[:i]
	}
	var matches []string
	err := fs.WalkDir(filesystem, ".", func(name string, entry fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return fmt.Errorf("glob search incomplete at %s: %w", name, err)
		}
		if name != "." && entry.IsDir() && !dirWithinPatternPrefix(name, prefix) {
			return fs.SkipDir
		}
		if name != "." && matchGlobParts(name, pattern) {
			matches = append(matches, name)
		}
		return nil
	})
	return matches, err
}

// dirWithinPatternPrefix reports whether the directory name is compatible with
// the fixed pattern segments that precede the first **; deeper segments may
// still complete the prefix, so only a mismatch among the segments present
// disqualifies the subtree.
func dirWithinPatternPrefix(name string, prefix []string) bool {
	if len(prefix) == 0 {
		return true
	}
	parts := strings.Split(name, "/")
	for j := 0; j < len(parts) && j < len(prefix); j++ {
		ok, _ := path.Match(prefix[j], parts[j])
		if !ok {
			return false
		}
	}
	return true
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
