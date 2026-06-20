package contextinit

import (
	graphpkg "nekocode/bot/index/graph"
	"nekocode/bot/index/projectctx"
	"nekocode/bot/index/service"
)

type ContextTarget interface {
	Add(role, content string, source ...string)
	SetContextWindow(budget int)
}

type IndexManager interface {
	Init() error
	Graph() *graphpkg.Graph
}

type Result struct {
	ProjectContext string
	IndexManager   *service.Manager
}

type Options struct {
	CWD             string
	ContextWindow   int
	LoadProjectText func(string) string
	NewIndexManager func(string) (*service.Manager, error)
}

func ApplyProjectContextAndIndex(target ContextTarget, opts Options) Result {
	if target == nil {
		return Result{}
	}
	defer target.SetContextWindow(opts.ContextWindow)

	if opts.CWD == "" {
		return Result{}
	}

	loadProjectText := opts.LoadProjectText
	if loadProjectText == nil {
		loadProjectText = projectctx.LoadProjectContext
	}
	projectText := loadProjectText(opts.CWD)
	if projectText != "" {
		target.Add("system", projectText)
	}

	newIndexManager := opts.NewIndexManager
	if newIndexManager == nil {
		newIndexManager = service.NewManager
	}
	mgr, err := newIndexManager(opts.CWD)
	if err != nil {
		return Result{ProjectContext: projectText}
	}
	if err := mgr.Init(); err != nil {
		return Result{ProjectContext: projectText}
	}
	injectSkeleton(target, mgr, opts.CWD)

	return Result{ProjectContext: projectText, IndexManager: mgr}
}

func injectSkeleton(target ContextTarget, mgr IndexManager, cwd string) {
	if mgr == nil {
		return
	}
	if graph := mgr.Graph(); graph != nil {
		if skeleton := graph.FormatSkeleton(cwd); skeleton != "" {
			target.Add("system", skeleton)
		}
	}
}
