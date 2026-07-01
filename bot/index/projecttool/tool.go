package projecttool

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	graphpkg "nekocode/bot/index/graph"
	"nekocode/bot/index/service"
	"nekocode/bot/tools/core"
	"nekocode/common"
)

// ProjectInfoTool exposes the code graph to the agent via the tool system.
// It holds a reference to the Manager so it always accesses the current graph
// (even after Manager.Rebuild replaces it).
type ProjectInfoTool struct {
	mgr *service.Manager
}

// NewProjectInfoTool creates a new project_info tool.
func NewProjectInfoTool(mgr *service.Manager) *ProjectInfoTool {
	return &ProjectInfoTool{mgr: mgr}
}

func (t *ProjectInfoTool) Name() string { return "project_info" }
func (t *ProjectInfoTool) DangerLevel(args map[string]any) common.DangerLevel {
	return common.LevelSafe
}
func (t *ProjectInfoTool) ExecutionMode(args map[string]any) core.ExecutionMode {
	return core.ModeParallel
}

func (t *ProjectInfoTool) Description() string {
	return "Pre-built project index. ALWAYS use this FIRST for: finding symbols (symbol:), finding files (file:), checking dependencies (deps:), full-text search (search:), or getting project overview (skeleton). Faster and more accurate than grep/glob for code structure queries."
}

func (t *ProjectInfoTool) Parameters() []core.Parameter {
	return []core.Parameter{
		{
			Name:        "query",
			Type:        "string",
			Description: "Format: symbol:<name>, deps:<pkg>, file:<name>, search:<term>, or skeleton",
			Required:    true,
		},
	}
}

func (t *ProjectInfoTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return "Missing required parameter 'query'. Usage: query=\"file:manager.go\" or query=\"symbol:Agent\". Note: 'file' is not a parameter name — use query=\"file:<name>\".", nil
	}

	if strings.HasPrefix(query, "search:") {
		value := strings.TrimSpace(strings.TrimPrefix(query, "search:"))
		return t.querySearch(value), nil
	}

	return t.mgr.Query(func(graph *graphpkg.Graph) string {
		if query == "skeleton" {
			return graph.FormatSkeleton(t.mgr.CWD())
		}

		prefix, value, ok := strings.Cut(query, ":")
		if !ok {
			return "Invalid query format. Use '<prefix>:<value>' (e.g. \"file:manager.go\", \"symbol:Agent\") or \"skeleton\"."
		}
		value = strings.TrimSpace(value)

		switch prefix {
		case "symbol":
			return querySymbol(graph, value)
		case "deps":
			return queryDeps(graph, value)
		case "file":
			return queryFile(graph, value)
		default:
			return fmt.Sprintf("Unknown query prefix '%s'. Available: symbol, deps, file, search, skeleton", prefix)
		}
	}), nil
}

func querySymbol(graph *graphpkg.Graph, name string) string {
	symbols := graph.QuerySymbol(name)
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

func queryDeps(graph *graphpkg.Graph, pkgPath string) string {
	deps := graph.QueryDeps(pkgPath)
	if deps == nil {
		return fmt.Sprintf("Package '%s' not found in project index or has no internal dependencies.", pkgPath)
	}
	sort.Strings(deps)
	var b strings.Builder
	fmt.Fprintf(&b, "Dependencies of %s (%d):\n", pkgPath, len(deps))
	for _, d := range deps {
		fmt.Fprintf(&b, "  %s\n", d)
	}
	return b.String()
}

func queryFile(graph *graphpkg.Graph, name string) string {
	files := graph.QueryFile(name)
	if len(files) == 0 {
		return fmt.Sprintf("No files matching '%s' found in project index. The file may have been deleted or renamed — try glob to search the filesystem directly.", name)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d file(s) matching '%s':\n", len(files), name)
	for _, f := range files {
		fmt.Fprintf(&b, "  %s\n", shortenPath(f.Path))
	}
	return b.String()
}

func (t *ProjectInfoTool) querySearch(term string) string {
	if t.mgr.Indexer() == nil {
		return "Full-text search is not available (database not initialized)."
	}
	nodes, err := t.mgr.Indexer().SearchFTS(term, 50)
	if err != nil {
		return fmt.Sprintf("Search error: %v", err)
	}
	if len(nodes) == 0 {
		return fmt.Sprintf("No results for '%s' in project index.", term)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d result(s) for '%s':\n", len(nodes), term)
	for _, n := range nodes {
		fmt.Fprintf(&b, "  %s %s — %s:%d\n", n.Kind, n.Name, shortenPath(n.File), n.Line)
	}
	return b.String()
}

var (
	homeDir     string
	homeDirOnce sync.Once
)

func shortenPath(path string) string {
	homeDirOnce.Do(func() {
		homeDir, _ = os.UserHomeDir()
	})
	if homeDir != "" && strings.HasPrefix(path, homeDir) {
		return "~" + path[len(homeDir):]
	}
	parts := strings.Split(path, "/")
	if len(parts) > 3 {
		parts = parts[len(parts)-3:]
	}
	return strings.Join(parts, "/")
}
