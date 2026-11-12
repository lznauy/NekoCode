package runner

import (
	"context"
	"testing"

	"nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/mode"
	"nekocode/protocol"
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

func TestCoordinatorPolicyAndWorkerPermissions(t *testing.T) {
	registry := tools.New(fakeTool{name: "read"}, fakeTool{name: "write"}, fakeTool{name: "shell"}, fakeTool{name: "capability"})
	registry.RegisterWithOptions(fakeTool{name: "coordinate"}, tools.RegistrationOptions{CoordinatorAllowed: true})
	parent := NewExecutor(registry)
	parent.SetFullAccess(true)
	definition, _ := mode.Resolve(mode.SuperAgent)
	if err := parent.SetInteractionMode(definition.Snapshot); err != nil {
		t.Fatal(err)
	}
	descriptors := parent.ToolDescriptors()
	if len(descriptors) != 1 || descriptors[0].Name != "coordinate" {
		t.Fatalf("tool view: %+v", descriptors)
	}
	for _, name := range []string{"read", "write", "shell", "capability"} {
		result := parent.ExecuteBatch(t.Context(), []core.ToolCallItem{{ID: name, Name: name}})[0]
		if result.Error == "" {
			t.Fatalf("coordinator executed %s under FULL", name)
		}
	}
	child := NewExecutor(registry)
	parent.ConfigureChildPermissions(child)
	if result := child.ExecuteBatch(t.Context(), []core.ToolCallItem{{ID: "write", Name: "write"}})[0]; result.Error != "" {
		t.Fatalf("worker inherited coordinator ceiling: %s", result.Error)
	}
	parent.SetFullAccess(false)
	if child.AccessObservation().Mode == "full" {
		t.Fatal("worker lost live permission inheritance")
	}
	// Replacement must not inherit trusted coordination metadata.
	registry.Register(fakeTool{name: "coordinate"})
	if len(parent.ToolDescriptors()) != 0 {
		t.Fatal("replacement retained coordinator capability")
	}
	if result := parent.ExecuteBatch(t.Context(), []core.ToolCallItem{{ID: "replacement", Name: "coordinate"}})[0]; result.Error == "" {
		t.Fatal("replacement bypassed policy")
	}
}

func TestCoordinatorBlocksPreviewEntryPoints(t *testing.T) {
	for _, entryPoint := range []string{"prepare", "sequential"} {
		t.Run(entryPoint, func(t *testing.T) {
			previews, emitted := 0, 0
			e := previewRuleExecutor(t, t.TempDir(), fakeTool{name: "forbidden"}, func(context.Context, map[string]any) string { previews++; return "private content" })
			e.SetFullAccess(true)
			d, _ := mode.Resolve(mode.SuperAgent)
			if err := e.SetInteractionMode(d.Snapshot); err != nil {
				t.Fatal(err)
			}
			e.SetPreviewDecisionFn(func(string, string, map[string]any, string, protocol.ToolDecision) { emitted++ })
			call := core.ToolCallItem{ID: "forbidden", Name: "forbidden", Args: map[string]any{"_preview": "stale"}}
			if entryPoint == "prepare" {
				calls := []core.ToolCallItem{call}
				e.PreparePreviewsContext(t.Context(), calls)
				if calls[0].Args["_preview"] != nil {
					t.Error("blocked preview cached")
				}
			} else {
				results := make([]core.ToolCallResult, 1)
				e.executeSequential(t.Context(), []indexedCall{{idx: 0, call: call}}, results)
				if results[0].Error == "" {
					t.Fatal("blocked tool executed")
				}
			}
			if previews != 0 || emitted != 0 {
				t.Fatalf("blocked tool produced previews: generated=%d emitted=%d", previews, emitted)
			}
		})
	}
}

func TestCoordinatorOnlyMetadataCannotBeRelaxedByPlanOverrides(t *testing.T) {
	registry := tools.New()
	registry.RegisterWithOptions(fakeTool{name: "private-control"}, tools.RegistrationOptions{CoordinatorOnly: true, CoordinatorAllowed: true, PlanAllowed: true})
	e := NewExecutor(registry)
	e.SetFullAccess(true)
	e.SetPlanTools("private-control")
	for _, id := range []mode.ID{mode.Standard, mode.Plan} {
		d, _ := mode.Resolve(id)
		if err := e.SetInteractionMode(d.Snapshot); err != nil {
			t.Fatal(err)
		}
		if err := e.CheckToolPolicy("private-control"); err == nil {
			t.Fatalf("%s relaxed coordinator-only metadata", id)
		}
		if len(e.ToolDescriptors()) != 0 {
			t.Fatalf("%s exposed coordinator-only schema", id)
		}
	}
	registry.Register(fakeTool{name: "private-control"})
	d, _ := mode.Resolve(mode.Standard)
	if err := e.SetInteractionMode(d.Snapshot); err != nil {
		t.Fatal(err)
	}
	if err := e.CheckToolPolicy("private-control"); err != nil {
		t.Fatalf("replacement retained restriction: %v", err)
	}
	if len(e.ToolDescriptors()) != 1 {
		t.Fatal("replacement retained schema restriction")
	}
}
