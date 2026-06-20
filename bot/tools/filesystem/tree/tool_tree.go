package tree

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"nekocode/bot/tools"
	"nekocode/bot/tools/toolhelpers"
)

type TreeTool struct {
	toolhelpers.SafeReadOnlyTool
}

func (t *TreeTool) Name() string { return "tree" }
func (t *TreeTool) Description() string {
	return "Show directory tree. depth (default 3, max 6), limit (default 400). Ignores hidden files."
}

func (t *TreeTool) Parameters() []tools.Parameter {
	return []tools.Parameter{
		{Name: "path", Type: "string", Required: true, Description: "Directory to show (absolute path)"},
		{Name: "depth", Type: "integer", Required: false, Description: "Levels deep (default 3, max 6)"},
		{Name: "limit", Type: "integer", Required: false, Description: "Max entries to show (default 400, max 800)"},
	}
}

func (t *TreeTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	path, err := toolhelpers.RequireStringArg(args, "path")
	if err != nil {
		return "", err
	}

	depth := toolhelpers.ClampIntArg(args, "depth", 3, 1, 6)
	limit := toolhelpers.ClampIntArg(args, "limit", 400, 1, 800)

	var b strings.Builder
	fmt.Fprintf(&b, "%s/\n", filepath.Base(path))

	count, err := walkTree(path, "", depth, limit, &b)
	if err != nil {
		return "", err
	}
	if count == 0 {
		b.WriteString("(empty)")
	}
	if count >= limit {
		fmt.Fprintf(&b, "\n... [reached limit of %d entries]", limit)
	}
	return b.String(), nil
}

func walkTree(dir, prefix string, depth, limit int, b *strings.Builder) (int, error) {
	if depth <= 0 {
		return 0, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("read dir %s: %w", dir, err)
	}

	type item struct {
		name  string
		isDir bool
	}
	var items []item
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".") {
			items = append(items, item{e.Name(), e.IsDir()})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].name < items[j].name })

	count := 0
	for i, it := range items {
		if count >= limit {
			break
		}
		conn, next := "├── ", prefix+"│   "
		if i == len(items)-1 {
			conn, next = "└── ", prefix+"    "
		}

		if it.isDir {
			fmt.Fprintf(b, "%s%s%s/\n", prefix, conn, it.name)
			n, _ := walkTree(filepath.Join(dir, it.name), next, depth-1, limit-count-1, b)
			count += n
		} else {
			fmt.Fprintf(b, "%s%s%s\n", prefix, conn, it.name)
		}
		count++
	}
	return count, nil
}
