package catalog

import (
	"nekocode/bot/config"
	"nekocode/bot/tools"
	edittool "nekocode/bot/tools/filesystem/edit"
	listtool "nekocode/bot/tools/filesystem/list"
	readtool "nekocode/bot/tools/filesystem/read"
	searchtool "nekocode/bot/tools/filesystem/search"
	treetool "nekocode/bot/tools/filesystem/tree"
	writetool "nekocode/bot/tools/filesystem/write"
	"nekocode/bot/tools/media"
	"nekocode/bot/tools/question"
	"nekocode/bot/tools/shell"
	"nekocode/bot/tools/tasktool"
	"nekocode/bot/tools/todo"
	"nekocode/bot/tools/web"
)

func RegisterAll(r *tools.Registry, imageGenModels []config.ImageGenConfig) {
	r.Register(&shell.BashTool{})
	r.Register(&readtool.ReadTool{})
	r.Register(&writetool.WriteTool{})
	r.Register(&listtool.ListTool{})
	r.Register(&treetool.TreeTool{})
	r.Register(&searchtool.GlobTool{})
	r.Register(&edittool.EditTool{})
	r.Register(&searchtool.GrepTool{})
	r.Register(web.NewWebSearchTool())
	r.Register(web.NewWebFetchTool())
	r.Register(question.NewTool())
	r.Register(&todo.TodoWriteTool{})
	r.Register(tasktool.NewTaskTool())

	if len(imageGenModels) > 0 {
		r.Register(media.NewImageGenTool(imageGenModels))
	}
}
