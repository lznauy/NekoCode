package builtin

import (
	"nekocode/bot/projctx"
	"nekocode/bot/tools"
)

func RegisterAll(r *tools.Registry) {
	r.Register(&BashTool{})
	r.Register(&ReadTool{})
	r.Register(&WriteTool{})
	r.Register(&ListTool{})
	r.Register(&TreeTool{})
	r.Register(&GlobTool{})
	r.Register(&EditTool{})
	r.Register(&GrepTool{})
	r.Register(NewWebSearchTool())
	r.Register(NewWebFetchTool())
	r.Register(NewTodoWriteTool())
	r.Register(NewTaskTool())
}

// RegisterProjectInfo registers the project_info tool with a built index.
func RegisterProjectInfo(r *tools.Registry, idx *projctx.ProjectIndex) {
	r.Register(NewProjectInfoTool(idx))
}
