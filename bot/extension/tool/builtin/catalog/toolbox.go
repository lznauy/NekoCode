package catalog

import (
	"nekocode/bot/config"
	"nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/builtin/lsp"
	"nekocode/bot/extension/tool/builtin/question"
	"nekocode/bot/extension/tool/builtin/shell"
	"nekocode/bot/extension/tool/builtin/task"
	"nekocode/bot/extension/tool/builtin/todo"
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
	task      *task.TaskTool
	question  *question.Tool
	todo      *todo.TodoWriteTool
	workspace *workspace.Manager
}

// ToolboxConfig groups configuration-dependent builtins without making the
// constructor signature grow for every optional tool.
type ToolboxConfig struct {
	ImageGenModels        []config.ImageGenConfig
	ImageUnderstandModels []config.ImageUnderstandConfig
}

// NewToolbox preserves the original image-generation-only constructor.
func NewToolbox(imageGenModels []config.ImageGenConfig) *Toolbox {
	return NewToolboxWithConfig(ToolboxConfig{ImageGenModels: imageGenModels})
}

// NewToolboxWithConfig assembles a fresh registry with the full catalog.
func NewToolboxWithConfig(cfg ToolboxConfig) *Toolbox {
	e := &Toolbox{workspace: workspace.New("", nil)}
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
	if e.task == nil {
		e.task = task.NewTaskTool()
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
	registerAll(e.Registry, cfg.ImageGenModels, cfg.ImageUnderstandModels, e.shell, e.lsp, e.task, e.question, e.todo)
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
