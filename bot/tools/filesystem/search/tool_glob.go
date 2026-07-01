// GlobTool — file pattern matching, always common.LevelSafe auto-approve.
package search

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nekocode/bot/tools/core"
	"nekocode/bot/tools/toolhelpers"
)

type GlobTool struct {
	toolhelpers.SafeReadOnlyTool
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

func (t *GlobTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	pattern, err := toolhelpers.RequireStringArg(args, "pattern")
	if err != nil {
		return "", err
	}

	basePath := toolhelpers.OptStringArg(args, "path", ".")

	var matches []string
	if strings.Contains(pattern, "**") {
		var err error
		matches, err = globRecursive(basePath, pattern)
		if err != nil {
			return "", fmt.Errorf("glob failed: %w", err)
		}
	} else {
		var err error
		matches, err = filepath.Glob(filepath.Join(basePath, pattern))
		if err != nil {
			return "", fmt.Errorf("glob failed: %w", err)
		}
	}

	if len(matches) == 0 {
		return "No matching files found", nil
	}

	var sb strings.Builder
	for _, m := range matches {
		sb.WriteString(m)
		sb.WriteByte('\n')
	}
	return sb.String(), nil
}

func globRecursive(basePath, pattern string) ([]string, error) {
	var matches []string
	parts := strings.Split(pattern, "**")

	err := filepath.Walk(basePath, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(basePath, p)
		if err != nil {
			return nil
		}
		if rel == "." {
			return nil
		}
		// Match each segment between ** anchors. A segment may be empty.
		if matchGlobParts(rel, parts) {
			matches = append(matches, p)
		}
		return nil
	})
	return matches, err
}

// matchGlobParts matches a path against pattern segments separated by **.
// e.g. pattern "docs/**/*.md" → parts ["docs/", "/*.md"]
// The ** matches zero or more directory levels.
func matchGlobParts(path string, parts []string) bool {
	if len(parts) == 1 {
		// No ** in pattern — use standard Match.
		ok, _ := filepath.Match(parts[0], path)
		return ok
	}
	// Must start with first part.
	if !strings.HasPrefix(path, parts[0]) {
		return false
	}
	// Must end with last part.
	if !hasSuffixMatch(path, parts[len(parts)-1]) {
		return false
	}
	// Middle segments must appear in order (if non-empty).
	remaining := path[len(parts[0]):]
	for _, seg := range parts[1 : len(parts)-1] {
		if seg == "" {
			continue
		}
		idx := strings.Index(remaining, seg)
		if idx < 0 {
			return false
		}
		remaining = remaining[idx+len(seg):]
	}
	return true
}

func hasSuffixMatch(path, suffix string) bool {
	if suffix == "" {
		return true
	}
	// filepath.Match semantics: suffix like "/*.md" should match "/ARCHITECTURE.md".
	ok, _ := filepath.Match("*"+suffix, path)
	if ok {
		return true
	}
	// Also try matching from a path separator boundary.
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			ok, _ = filepath.Match("*"+suffix, path[i:])
			return ok
		}
	}
	return false
}
