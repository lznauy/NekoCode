package subagent

import (
	"context"

	"nekocode/common"
)

type AgentType struct {
	Name               string
	SystemPrompt       string
	Tools              []string
	OmitProjectContext bool
}

// ToolCallEvent is fired for each tool executed inside a sub-agent.
type ToolCallEvent struct {
	Action   string // "tool_start" | "execute_tool"
	ToolName string
	ToolArgs string
	Output   string
}

// subCallbackCtxKey is the context key for injecting a sub-agent TUI callback
// from run_exec.go through to the Wire func in bot.go.
type subCallbackCtxKey struct{}

// SubCallbackFn is the function signature for sub-agent → TUI callbacks.
type SubCallbackFn func(action, toolName, toolArgs, output string)

// WithSubCallback returns a child context with the sub-callback injected.
func WithSubCallback(ctx context.Context, cb SubCallbackFn) context.Context {
	return context.WithValue(ctx, subCallbackCtxKey{}, cb)
}

// SubCallbackFromCtx retrieves the sub-callback from context, if any.
func SubCallbackFromCtx(ctx context.Context) (SubCallbackFn, bool) {
	cb, ok := ctx.Value(subCallbackCtxKey{}).(SubCallbackFn)
	return cb, ok
}

type RunConfig struct {
	Prompt          string
	AgentType       AgentType
	Cwd             string
	ProjectContext  string
	Thoroughness    string
	ContextWindow     int
	OnPhase         func(phase string)
	AddTokens       func(prompt, compl int)
	DisableThinking bool
	ConfirmFn       common.ConfirmFunc
	Handoff         string // injected into system prompt for cross-agent context
	OnToolCall      func(ev ToolCallEvent) // sub-agent tool execution callback
}

var (
	builtins = common.NewRegistry[AgentType](func(a AgentType) string { return a.Name })
	plugins  = common.NewRegistry[AgentType](func(a AgentType) string { return a.Name })
)

func register(a AgentType) { builtins.Register(a) }

// RegisterPlugin registers a plugin-provided agent type.
func RegisterPlugin(a AgentType) { plugins.Register(a) }

// UnregisterPlugin removes a plugin-provided agent type by name.
func UnregisterPlugin(name string) { plugins.Unregister(name) }

// Get looks up an agent type by name, checking builtins first, then plugins.
func Get(name string) (AgentType, bool) {
	if a, ok := builtins.Get(name); ok {
		return a, ok
	}
	return plugins.Get(name)
}

