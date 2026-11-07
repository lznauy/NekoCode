package runner

import (
	"context"
	"testing"

	"nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/mode"
)

func TestInteractionModeCeilingSurvivesFullAccess(t *testing.T) {
	registry := tools.New()
	registry.RegisterWithOptions(fakeTool{name: "read-test"}, tools.RegistrationOptions{PlanAllowed: true})
	for _, name := range []string{"shell", "write", "task", "mcp_mutation"} {
		registry.Register(fakeTool{name: name})
	}
	e := NewExecutor(registry)
	e.SetFullAccess(true)
	plan, _ := mode.Resolve(mode.Plan)
	if err := e.SetInteractionMode(plan.Snapshot); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"shell", "write", "task", "mcp_mutation"} {
		result := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{ID: name, Name: name}})[0]
		if result.Error == "" {
			t.Fatalf("plan/full executed %s", name)
		}
	}
	if result := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{ID: "read", Name: "read-test"}})[0]; result.Error != "" {
		t.Fatal(result.Error)
	}
	invalid := plan.Snapshot
	invalid.ToolPolicy = mode.AllTools
	if err := e.SetInteractionMode(invalid); err == nil || !e.AccessObservation().PlanMode {
		t.Fatal("invalid policy relaxed plan")
	}
	e.SetPlanMode(false)
	if result := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{ID: "write", Name: "write"}})[0]; result.Error != "" {
		t.Fatalf("standard retained plan ceiling: %s", result.Error)
	}
}
