package catalog

import (
	"nekocode/bot/config"
	"nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/builtin/index"
	"nekocode/bot/extension/tool/builtin/lsp"
	"nekocode/bot/extension/tool/builtin/question"
	"nekocode/bot/extension/tool/builtin/shell"
	"nekocode/bot/extension/tool/builtin/task"
	"nekocode/bot/extension/tool/builtin/todo"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/bot/extension/tool/runtime/workspace"
	"nekocode/protocol"
)

// Toolbox owns the builtin registry and the stateful runtimes shared by tools
// across registry rebuilds: the shell process registry and the LSP server pool.
type Toolbox struct {
	Registry  *tools.Registry
	shell     *shell.ShellTool
	lsp       *lsp.LSPTool
	index     *index.IndexTool
	task      *task.TaskTool
	taskList  *task.TaskListTool
	question  *question.Tool
	todo      *todo.TodoWriteTool
	workspace *workspace.Manager
	// extraTools survive registry rebuilds (triggered by config changes
	// like /model) without being re-declared by the caller.
	extraTools []core.Tool
}

// ToolboxConfig groups configuration-dependent builtins without making the
// constructor signature grow for every optional tool.
type ToolboxConfig struct {
	Workspace             *workspace.Manager
	ImageGenModels        []config.ImageGenConfig
	ImageUnderstandModels []config.ImageUnderstandConfig
	// ExtraTools are appended on every registry rebuild; the assembly layer
	// uses this to inject cross-cutting tools (e.g. deliver_file bridging to
	// IM connectors) without the catalog depending on them.
	ExtraTools []core.Tool
}

// NewToolbox preserves the original image-generation-only constructor.
func NewToolbox(imageGenModels []config.ImageGenConfig) *Toolbox {
	return NewToolboxWithConfig(ToolboxConfig{ImageGenModels: imageGenModels})
}

// NewToolboxWithConfig assembles a fresh registry with the full catalog.
func NewToolboxWithConfig(cfg ToolboxConfig) *Toolbox {
	manager := cfg.Workspace
	if manager == nil {
		manager = workspace.New("", nil)
	}
	e := &Toolbox{workspace: manager}
	e.RebuildRegistryWithConfig(cfg)
	return e
}

// RebuildRegistry preserves the original image-generation-only rebuild API.
func (e *Toolbox) RebuildRegistry(imageGenModels []config.ImageGenConfig) {
	e.RebuildRegistryWithConfig(ToolboxConfig{ImageGenModels: imageGenModels})
}

// RebuildRegistryWithConfig keeps the shell and LSP runtimes alive while
// replacing tool schemas and configuration-dependent tools.
func (e *Toolbox) RebuildRegistryWithConfig(cfg ToolboxConfig) {
	if e.shell == nil {
		e.shell = &shell.ShellTool{}
	}
	if e.lsp == nil {
		e.lsp = lsp.NewLSPTool()
	}
	if e.index == nil {
		e.index = index.NewIndexTool()
	}
	if e.task == nil {
		e.task = task.NewTaskTool()
	}
	if e.taskList == nil {
		e.taskList = task.NewTaskListTool()
	}
	if e.question == nil {
		e.question = question.NewTool()
	}
	if e.todo == nil {
		e.todo = &todo.TodoWriteTool{}
	}
	if e.workspace == nil {
		e.workspace = workspace.New("", nil)
	}
	e.Registry = tools.New()
	e.Registry.SetWorkspace(e.workspace)
	registerAll(e.Registry, cfg.ImageGenModels, cfg.ImageUnderstandModels, e.shell, e.lsp, e.index, e.task, e.taskList, e.question, e.todo)
	for _, t := range cfg.ExtraTools {
		e.Registry.Register(t)
	}
	for _, t := range e.extraTools {
		e.Registry.Register(t)
	}
}

// SetExtraTools registers tools that persist across registry rebuilds
// (config changes rebuild the registry from the catalog alone — without
// this, injected tools would vanish on the next /model switch).
func (e *Toolbox) SetExtraTools(tools ...core.Tool) {
	e.extraTools = append(e.extraTools, tools...)
	for _, t := range tools {
		e.Registry.Register(t)
	}
}

func (e *Toolbox) Workspace() *workspace.Manager { return e.workspace }

// ManagedProcessSummary returns volatile process state for the next model
// request. It is not persisted in conversation history.
func (e *Toolbox) ManagedProcessSummary() string {
	if e.shell == nil {
		return ""
	}
	return e.shell.ProcessSummary()
}

func (e *Toolbox) SetSessionID(id string) {
	if e.workspace != nil {
		e.workspace.SetSession(id)
	}
	if e.shell != nil {
		e.shell.SetSessionID(id)
	}
}

// CloseSession stops owned processes before revoking temporary workspace
// grants. On failure, callers must keep the current session active.
func (e *Toolbox) CloseSession(id string) error {
	if e.shell != nil {
		if err := e.shell.StopSession(id); err != nil {
			return err
		}
	}
	if e.workspace != nil {
		e.workspace.DropSession(id)
	}
	return nil
}

// Close shuts down the stateful shell tool and LSP servers. It is nil-safe and
// idempotent: a second call is a no-op.
func (e *Toolbox) Close() error {
	if e.shell != nil {
		if err := e.shell.Shutdown(); err != nil {
			return err
		}
		e.shell = nil
	}
	if e.lsp != nil {
		e.lsp.Close()
		e.lsp = nil
	}
	if e.index != nil {
		if err := e.index.Close(); err != nil {
			return err
		}
		e.index = nil
	}
	return nil
}

// SetSandboxEngine injects the permission engine into the shell tool.
// Nil-safe: a no-op when the shell tool is gone (e.g. after Close).
func (e *Toolbox) SetSandboxEngine(engine *permission.Engine) {
	if e.shell != nil {
		e.shell.SetSandboxEngine(engine)
	}
}

// WireTaskRunner binds the concrete delegated-task runtime owned by the bot.
func (e *Toolbox) WireTaskRunner(run taskbridge.TaskRunner) {
	if e.task != nil {
		e.task.Wire(run)
	}
}

func (e *Toolbox) WireTaskLister(list taskbridge.TaskLister) {
	if e.taskList != nil {
		e.taskList.Wire(list)
	}
}

// WireInteraction binds application callbacks to the concrete builtin tools
// owned by this toolbox.
func (e *Toolbox) WireInteraction(ask protocol.QuestionFunc, todos protocol.TodoFunc) {
	if e.question != nil {
		e.question.SetQuestionFunc(ask)
	}
	if e.todo != nil {
		e.todo.SetUpdateFn(todos)
	}
}
