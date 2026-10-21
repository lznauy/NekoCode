package index

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	indexcore "nekocode/bot/extension/tool/builtin/index/core"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/workspace"
)

// IndexTool exposes the code graph to the agent via the tool system.
// It lazily initializes its own Manager on first use, so registration
// requires no external setup.
type IndexTool struct {
	mu     sync.Mutex
	root   string
	closed bool
	mgr    *indexcore.Manager
	mgrErr error
}

// NewIndexTool creates a new index tool. The Manager is built lazily on
// first Execute using the injected workspace, or cwd for standalone use.
func NewIndexTool() *IndexTool {
	return &IndexTool{}
}

func (t *IndexTool) ensureInit(ctx context.Context) (*indexcore.Manager, error) {
	cwd, err := workspace.WorkingDirectory(ctx)
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, fmt.Errorf("index tool is closed")
	}
	if t.root != "" && t.root != cwd {
		return nil, fmt.Errorf("index tool belongs to workspace %q, not %q", t.root, cwd)
	}
	if t.root == "" {
		t.root = cwd
		t.mgr, t.mgrErr = indexcore.NewManager(cwd)
		if t.mgrErr == nil {
			t.mgrErr = t.mgr.Init()
		}
	}
	return t.mgr, t.mgrErr
}

// Close releases the index watcher and database owned by this tool instance.
func (t *IndexTool) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	if t.mgr != nil {
		return t.mgr.Close()
	}
	return nil
}

func (t *IndexTool) Name() string { return "index" }
func (t *IndexTool) ExecutionMode(args map[string]any) core.ExecutionMode {
	return core.ModeParallel
}

func (t *IndexTool) Description() string {
	return "Query the project index for symbols (symbol:), callers/callees, files (file:), package dependencies (deps:), text (search:), or an architecture overview (skeleton). Use it when code structure is the question; confirm relevant source before editing because index results are navigation evidence, not current file contents."
}

func (t *IndexTool) Parameters() []core.Parameter {
	return []core.Parameter{
		{
			Name:        "query",
			Type:        "string",
			Description: "Format: symbol:<name>, callers:<name>, callees:<name>, deps:<pkg>, file:<name>, search:<term>, or skeleton",
			Required:    true,
		},
	}
}

func (t *IndexTool) Execute(ctx context.Context, args map[string]any) (core.ToolOutput, error) {
	mgr, err := t.ensureInit(ctx)
	if err != nil {
		return core.ToolOutput{Text: fmt.Sprintf("Index initialization failed: %v", err)}, nil
	}

	query, _ := args["query"].(string)
	if query == "" {
		return core.ToolOutput{Text: "Missing required parameter 'query'. Usage: query=\"file:manager.go\" or query=\"symbol:Agent\". Note: 'file' is not a parameter name — use query=\"file:<name>\"."}, nil
	}

	if strings.HasPrefix(query, "search:") {
		value := strings.TrimSpace(strings.TrimPrefix(query, "search:"))
		return core.ToolOutput{Text: querySearch(mgr, value)}, nil
	}

	if query == "skeleton" {
		return core.ToolOutput{Text: mgr.Skeleton()}, nil
	}

	prefix, value, ok := strings.Cut(query, ":")
	if !ok {
		return core.ToolOutput{Text: "Invalid query format. Use '<prefix>:<value>' (e.g. \"file:manager.go\", \"symbol:Agent\") or \"skeleton\"."}, nil
	}
	value = strings.TrimSpace(value)

	switch prefix {
	case "symbol":
		return core.ToolOutput{Text: querySymbol(mgr, value)}, nil
	case "callers":
		return core.ToolOutput{Text: queryCalls(mgr, value, true)}, nil
	case "callees":
		return core.ToolOutput{Text: queryCalls(mgr, value, false)}, nil
	case "deps":
		return core.ToolOutput{Text: queryDeps(mgr, value)}, nil
	case "file":
		return core.ToolOutput{Text: queryFile(mgr, value)}, nil
	default:
		return core.ToolOutput{Text: fmt.Sprintf("Unknown query prefix '%s'. Available: symbol, callers, callees, deps, file, search, skeleton", prefix)}, nil
	}
}

func querySymbol(mgr *indexcore.Manager, name string) string {
	symbols := mgr.QuerySymbol(name)
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

func queryCalls(mgr *indexcore.Manager, name string, callers bool) string {
	direction := "callee"
	relations := mgr.QueryCallees(name)
	if callers {
		direction = "caller"
		relations = mgr.QueryCallers(name)
	}
	if len(relations) == 0 {
		return fmt.Sprintf("No resolved %ss found for symbols matching '%s'.", direction, name)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d resolved %s relation(s) for symbols matching '%s':\n", len(relations), direction, name)
	for _, relation := range relations {
		if callers {
			writeCallRelation(&b, relation.Related, relation.Symbol)
		} else {
			writeCallRelation(&b, relation.Symbol, relation.Related)
		}
	}
	return b.String()
}

func writeCallRelation(b *strings.Builder, caller, callee indexcore.Symbol) {
	fmt.Fprintf(b, "  %s %s — %s:%d -> %s %s — %s:%d\n",
		caller.Kind, caller.Name, shortenPath(caller.File), caller.Line,
		callee.Kind, callee.Name, shortenPath(callee.File), callee.Line)
}

func queryDeps(mgr *indexcore.Manager, pkgPath string) string {
	deps := mgr.QueryDeps(pkgPath)
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

func queryFile(mgr *indexcore.Manager, name string) string {
	files := mgr.QueryFile(name)
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

func querySearch(mgr *indexcore.Manager, term string) string {
	nodes, err := mgr.Search(term, 50)
	if err != nil {
		return fmt.Sprintf("Search error: %v", err)
	}
	if nodes == nil {
		return "Full-text search is not available (database not initialized)."
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

func shortenPath(path string) string {
	path = filepath.ToSlash(path)
	parts := strings.Split(path, "/")
	if len(parts) > 3 {
		parts = parts[len(parts)-3:]
	}
	return strings.Join(parts, "/")
}
