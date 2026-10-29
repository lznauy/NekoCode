package toolpolicy

import (
	"context"
	"testing"

	tools "nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/bot/extension/tool/runtime/runner"
	"nekocode/bot/policy"
	"nekocode/protocol"
)

type observedTool struct{}

func (observedTool) Name() string                                    { return "shell" }
func (observedTool) ExecutionMode(map[string]any) core.ExecutionMode { return core.ModeSequential }
func (observedTool) Description() string                             { return "fixture" }
func (observedTool) Parameters() []core.Parameter                    { return nil }
func (observedTool) Execute(context.Context, map[string]any) (core.ToolOutput, error) {
	return core.ToolOutput{Text: "ran"}, nil
}

func TestPermissionHooksObserveActorsAndCannotAuthorize(t *testing.T) {
	gov := policy.New()
	var actors []string
	for _, point := range []policy.HookPoint{policy.PermissionRequest, policy.PermissionDenied} {
		point := point
		if err := gov.Register(policy.Hook{Name: string(point), Point: point, On: func(state policy.State) *policy.Result {
			facts := state.Facts()
			actors = append(actors, facts.ActorID)
			if facts.Tool.Args["call_id"] != "call" {
				t.Errorf("missing call id: %+v", facts)
			}
			// Even an invalid observation result cannot turn rejection into approval.
			return &policy.Result{Decision: &policy.Decision{Action: policy.Terminate, Reason: "cannot control permissions"}}
		}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, actor := range []string{"main", "child-task"} {
		executor := runner.NewExecutor(tools.New(observedTool{}))
		executor.SetPermissionPolicy(permission.PermissionsDecl{Ask: []string{"shell"}}, t.TempDir(), t.TempDir())
		ObservePermissions(executor, gov, actor)
		executor.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply { return protocol.Deny() })
		result := executor.ExecuteBatch(t.Context(), []core.ToolCallItem{{ID: "call", Name: "shell", Args: map[string]any{"command": "fixture"}}})[0]
		if result.Error == "" {
			t.Fatal("observer bypassed rejection")
		}
	}
	if len(actors) != 4 || actors[0] != "main" || actors[1] != "main" || actors[2] != "child-task" || actors[3] != "child-task" {
		t.Fatalf("actors=%v", actors)
	}
}
