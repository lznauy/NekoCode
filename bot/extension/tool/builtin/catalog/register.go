package catalog

import (
	"nekocode/bot/config"
	"nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/builtin/contextrefs"
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

func registerAll(r *tools.Registry, imageGenModels []config.ImageGenConfig, imageUnderstandModels []config.ImageUnderstandConfig, shellTool *shell.ShellTool, lspTool *lsp.LSPTool, indexTool *index.IndexTool, taskTool *task.TaskTool, taskListTool *task.TaskListTool, questionTool *question.Tool, todoTool *todo.TodoWriteTool, referencesTool, evidenceTool *contextrefs.Tool) {
	plan := tools.RegistrationOptions{PlanAllowed: true, DelegationAllowed: true}
	r.RegisterWithOptions(shellTool, tools.RegistrationOptions{
		DelegationAllowed: true,
		Privileged:        shellTool.ExecuteWithPermission,
		PermissionPlan:    shellTool.PermissionPlan,
	})
	r.RegisterWithOptions(shell.NewProcessTool(shellTool), tools.RegistrationOptions{DelegationAllowed: true})
	r.RegisterWithOptions(&read.ReadTool{}, plan)
	writeTool := &write.WriteTool{}
	r.RegisterWithOptions(writeTool, tools.RegistrationOptions{Preview: writeTool.PreviewContext, DelegationAllowed: true})
	r.RegisterWithOptions(&list.ListTool{}, plan)
	r.RegisterWithOptions(&tree.TreeTool{}, plan)
	r.RegisterWithOptions(&search.GlobTool{}, plan)
	editTool := &edit.EditTool{}
	r.RegisterWithOptions(editTool, tools.RegistrationOptions{Preview: editTool.PreviewContext, DelegationAllowed: true})
	r.RegisterWithOptions(&search.GrepTool{}, plan)
	r.RegisterWithOptions(web.NewWebSearchTool(), plan)
	r.RegisterWithOptions(web.NewWebFetchTool(), plan)
	r.RegisterWithOptions(web.NewWebExtractTool(), plan)
	r.RegisterWithOptions(questionTool, tools.RegistrationOptions{CoordinatorAllowed: true})
	r.RegisterWithOptions(todoTool, tools.RegistrationOptions{CoordinatorAllowed: true, PlanAllowed: true})
	r.Register(taskTool)
	r.RegisterWithOptions(taskListTool, tools.RegistrationOptions{PlanAllowed: true, CoordinatorAllowed: true})
	r.RegisterWithOptions(referencesTool, plan)
	r.RegisterWithOptions(evidenceTool, plan)
	diffTool := diff.NewTool()
	r.RegisterWithOptions(diffTool, tools.RegistrationOptions{PlanAllowed: true, Preview: diffTool.PreviewContext, DelegationAllowed: true})
	r.RegisterWithOptions(indexTool, plan)
	for _, t := range lspTool.Tools() {
		options := tools.RegistrationOptions{DelegationAllowed: true}
		switch t.Name() {
		case "lsp_definition", "lsp_references", "lsp_hover", "lsp_diagnostics":
			options.PlanAllowed = true
		}
		r.RegisterWithOptions(t, options)
	}

	if len(imageGenModels) > 0 {
		r.RegisterWithOptions(media.NewImageGenTool(imageGenModels), tools.RegistrationOptions{DelegationAllowed: true})
	}
	if len(imageUnderstandModels) > 0 {
		imageUnderstandTool := media.NewImageUnderstandTool(imageUnderstandModels)
		r.RegisterWithOptions(imageUnderstandTool, tools.RegistrationOptions{
			PermissionPlan: imageUnderstandTool.PermissionPlan, DelegationAllowed: true,
		})
	}
}
