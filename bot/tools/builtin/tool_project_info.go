package builtin

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"nekocode/bot/projctx"
	"nekocode/bot/tools"

	"nekocode/common"
)

// ProjectInfoTool lets the agent query the pre-computed project index.
// This replaces expensive grep/glob exploration with direct symbol lookups.
type ProjectInfoTool struct {
	idx *projctx.ProjectIndex
}

func NewProjectInfoTool(idx *projctx.ProjectIndex) *ProjectInfoTool {
	return &ProjectInfoTool{idx: idx}
}

func (t *ProjectInfoTool) Name() string        { return "project_info" }
func (t *ProjectInfoTool) DangerLevel(args map[string]interface{}) common.DangerLevel { return common.LevelSafe }
func (t *ProjectInfoTool) ExecutionMode(args map[string]interface{}) tools.ExecutionMode {
	return tools.ModeParallel
}

func (t *ProjectInfoTool) Description() string {
	return "Fast project index lookup. Single query parameter: symbol:<name>, deps:<pkg>, file:<name>, skeleton. Prefer over grep/glob for structural questions."
}

func (t *ProjectInfoTool) Parameters() []tools.Parameter {
	return []tools.Parameter{
		{
			Name:        "query",
			Type:        "string",
			Description: "Format: symbol:<name>, deps:<pkg>, file:<name>, or skeleton",
			Required:    true,
		},
	}
}

func (t *ProjectInfoTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	if t.idx == nil {
		return "Project index not available for this workspace.", nil
	}

	query, _ := args["query"].(string)
	if query == "" {
		return "Missing required parameter 'query'. Usage: query=\"file:manager.go\" or query=\"symbol:Agent\". Note: 'file' is not a parameter name — use query=\"file:<name>\".", nil
	}

	// "skeleton" is a standalone query — no colon needed.
	if query == "skeleton" {
		return t.idx.FormatSkeleton(), nil
	}

	colon := strings.IndexByte(query, ':')
	if colon < 0 {
		return "Invalid query format. Use '<prefix>:<value>' (e.g. \"file:manager.go\", \"symbol:Agent\") or \"skeleton\".", nil
	}

	prefix := query[:colon]
	value := strings.TrimSpace(query[colon+1:])

	switch prefix {
	case "symbol":
		return t.querySymbol(value), nil
	case "deps":
		return t.queryDeps(value), nil
	case "file":
		return t.queryFile(value), nil
	case "skeleton":
		return t.idx.FormatSkeleton(), nil
	default:
		return fmt.Sprintf("Unknown query prefix '%s'. Available: symbol, deps, file, skeleton", prefix), nil
	}
}

func (t *ProjectInfoTool) querySymbol(name string) string {
	symbols := t.idx.QuerySymbol(name)
	if len(symbols) == 0 {
		return fmt.Sprintf("No symbols matching '%s' found in project index. Try grep for a broader search.", name)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d symbol(s) matching '%s':\n", len(symbols), name)
	for _, s := range symbols {
		fmt.Fprintf(&b, "  %s %s — %s:%d (%s)\n", s.Kind, s.Name, shortenPath(s.File), s.Line, s.PkgPath)
	}
	return b.String()
}

func (t *ProjectInfoTool) queryDeps(pkgPath string) string {
	deps := t.idx.QueryDeps(pkgPath)
	if deps == nil {
		return fmt.Sprintf("Package '%s' not found in project index.", pkgPath)
	}
	if len(deps) == 0 {
		return fmt.Sprintf("Package '%s' has no internal dependencies.", pkgPath)
	}
	sort.Strings(deps)
	var b strings.Builder
	fmt.Fprintf(&b, "Dependencies of %s (%d):\n", pkgPath, len(deps))
	for _, d := range deps {
		b.WriteString("  " + d + "\n")
	}
	return b.String()
}

func (t *ProjectInfoTool) queryFile(name string) string {
	files := t.idx.QueryFile(name)
	if len(files) == 0 {
		return fmt.Sprintf("No files matching '%s' found in project index. The file may have been deleted or renamed — try glob to search the filesystem directly.", name)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d file(s) matching '%s':\n", len(files), name)
	for _, f := range files {
		fmt.Fprintf(&b, "  %s (%s, ~%d lines)\n", shortenPath(f.Path), f.Package, f.Lines)
	}
	return b.String()
}

func shortenPath(path string) string {
	// Strip the home-relative prefix for readability.
	const homePrefix = "/home/"
	if i := strings.Index(path, homePrefix); i >= 0 {
		rest := path[i+len(homePrefix):]
		if j := strings.IndexByte(rest, '/'); j >= 0 {
			return "~/" + rest[j+1:]
		}
	}
	// Show last 3 path segments.
	parts := strings.Split(path, "/")
	if len(parts) > 3 {
		parts = parts[len(parts)-3:]
	}
	return strings.Join(parts, "/")
}
