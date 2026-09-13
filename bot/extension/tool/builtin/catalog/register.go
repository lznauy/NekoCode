package catalog

import (
	"nekocode/bot/config"
	"nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/builtin/diff"
	"nekocode/bot/extension/tool/builtin/filesystem/edit"
	"nekocode/bot/extension/tool/builtin/filesystem/list"
	"nekocode/bot/extension/tool/builtin/filesystem/read"
	"nekocode/bot/extension/tool/builtin/filesystem/search"
	"nekocode/bot/extension/tool/builtin/filesystem/tree"
	"nekocode/bot/extension/tool/builtin/filesystem/write"
	"nekocode/bot/extension/tool/builtin/index"
	"nekocode/bot/extension/tool/builtin/lsp"
	"nekocode/bot/extension/tool/builtin/media"
	"nekocode/bot/extension/tool/builtin/question"
	"nekocode/bot/extension/tool/builtin/shell"
	"nekocode/bot/extension/tool/builtin/task"
	"nekocode/bot/extension/tool/builtin/todo"
	"nekocode/bot/extension/tool/builtin/web"
)

func registerAll(r *tools.Registry, imageGenModels []config.ImageGenConfig, shellTool *shell.ShellTool, lspTool *lsp.LSPTool, taskTool *task.TaskTool, questionTool *question.Tool, todoTool *todo.TodoWriteTool) {
	plan := tools.RegistrationOptions{PlanAllowed: true}
	r.RegisterWithOptions(shellTool, tools.RegistrationOptions{
		Privileged:     shellTool.ExecuteWithPermission,
		PermissionPlan: shellTool.PermissionPlan,
	})
	r.Register(shell.NewProcessTool(shellTool))
	r.RegisterWithOptions(&read.ReadTool{}, plan)
	writeTool := &write.WriteTool{}
	r.RegisterWithOptions(writeTool, tools.RegistrationOptions{Preview: writeTool.PreviewContext})
	r.RegisterWithOptions(&list.ListTool{}, plan)
	r.RegisterWithOptions(&tree.TreeTool{}, plan)
	r.RegisterWithOptions(&search.GlobTool{}, plan)
	editTool := &edit.EditTool{}
	r.RegisterWithOptions(editTool, tools.RegistrationOptions{Preview: editTool.PreviewContext})
	r.RegisterWithOptions(&search.GrepTool{}, plan)
	r.RegisterWithOptions(web.NewWebSearchTool(), plan)
	r.RegisterWithOptions(web.NewWebFetchTool(), plan)
	r.RegisterWithOptions(web.NewWebExtractTool(), plan)
	r.Register(questionTool)
	r.Register(todoTool)
	r.Register(taskTool)
	diffTool := diff.NewTool()
	r.RegisterWithOptions(diffTool, tools.RegistrationOptions{Preview: diffTool.PreviewContext})
	r.Register(index.NewIndexTool())
	for _, t := range lspTool.Tools() {
		options := tools.RegistrationOptions{}
		switch t.Name() {
		case "lsp_definition", "lsp_references", "lsp_hover", "lsp_diagnostics":
			options.PlanAllowed = true
		}
		r.RegisterWithOptions(t, options)
	}

	if len(imageGenModels) > 0 {
		r.Register(media.NewImageGenTool(imageGenModels))
	}
}
