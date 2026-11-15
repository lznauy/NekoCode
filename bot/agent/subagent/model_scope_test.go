package subagent

import (
	"context"
	"testing"

	"nekocode/bot/calllog"
	"nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/provider/types"
)

type scopeObserverTool struct {
	testProxyTool
	session, call string
}

func (t *scopeObserverTool) Execute(ctx context.Context, _ map[string]any) (core.ToolOutput, error) {
	t.session = calllog.SessionFromContext(ctx)
	t.call = core.ToolCallIDFromContext(ctx)
	return core.ToolOutput{Text: "scope observed"}, nil
}

func TestTaskToolModelsKeepParentLogScopeWithDifferentWorkspaceIdentity(t *testing.T) {
	inspect := &scopeObserverTool{testProxyTool: testProxyTool{name: "inspect"}}
	registry := tools.New(inspect)
	registry.Workspace().SetSession("bare-task-workspace-id")
	model := &scriptedLLM{scripts: [][]types.StreamToken{
		{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "inspect-call", Name: "inspect", Arguments: `{}`}}}, completionScript(),
	}}
	_, err := New(Config{LLM: model, Tools: registry}).Run(t.Context(), RunConfig{
		SessionID: "parent-session", SubagentID: "bare-task-workspace-id", Profile: Profile{Name: "general", Tools: []string{"inspect"}},
		FullAccess: func() bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	if inspect.session != "parent-session" || inspect.call != "inspect-call" {
		t.Fatalf("scope=%q call=%q", inspect.session, inspect.call)
	}
}
