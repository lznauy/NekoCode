package catalog

import (
	"testing"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/runner"
	"nekocode/mode"
)

func TestBackgroundToolsAreCoordinatorOnly(t *testing.T) {
	box := NewToolboxWithConfig(ToolboxConfig{})
	t.Cleanup(func() { _ = box.Close() })
	for _, id := range []mode.ID{mode.Standard, mode.Plan, mode.SuperAgent} {
		t.Run(string(id), func(t *testing.T) {
			e := runner.NewExecutor(box.Registry)
			e.SetFullAccess(true)
			d, _ := mode.Resolve(id)
			if err := e.SetInteractionMode(d.Snapshot); err != nil {
				t.Fatal(err)
			}
			visible := map[string]bool{}
			for _, descriptor := range e.ToolDescriptors() {
				visible[descriptor.Name] = true
			}
			for _, name := range []string{"task_submit", "task_cancel"} {
				if visible[name] != (id == mode.SuperAgent) {
					t.Errorf("%s visible=%v", name, visible[name])
				}
				if err := e.CheckToolPolicy(name); (err == nil) != (id == mode.SuperAgent) {
					t.Errorf("%s policy: %v", name, err)
				}
				if id != mode.SuperAgent {
					result := e.ExecuteBatch(t.Context(), []core.ToolCallItem{{ID: name, Name: name}})[0]
					if result.NonExecution.Stage != "mode" {
						t.Errorf("%s bypassed mode boundary: %+v", name, result)
					}
				}
			}
			if visible["task_findings"] {
				t.Fatal("review workflow tool exposed by generic toolbox")
			}
			if !visible["task_result"] {
				t.Error("result reader hidden")
			}
			if id != mode.SuperAgent && (!visible["read"] || !visible["write"]) {
				t.Error("legacy Standard/Plan tool view changed")
			}
		})
	}
}

func TestTaskBatchOnlyInStandardMode(t *testing.T) {
	box := NewToolboxWithConfig(ToolboxConfig{})
	t.Cleanup(func() { _ = box.Close() })
	e := runner.NewExecutor(box.Registry)
	e.SetFullAccess(true)
	e.SetPlanTools("task_batch", "task")
	for _, id := range []mode.ID{mode.Standard, mode.Plan, mode.SuperAgent} {
		t.Run(string(id), func(t *testing.T) {
			d, _ := mode.Resolve(id)
			if err := e.SetInteractionMode(d.Snapshot); err != nil {
				t.Fatal(err)
			}
			visible := false
			for _, descriptor := range e.ToolDescriptors() {
				if descriptor.Name == "task_batch" {
					visible = true
				}
			}
			allowed := id == mode.Standard
			if visible != allowed {
				t.Errorf("visible=%v, want %v", visible, allowed)
			}
			if err := e.CheckToolPolicy("task_batch"); (err == nil) != allowed {
				t.Errorf("check=%v, want allowed=%v", err, allowed)
			}
			if !allowed {
				result := e.ExecuteBatch(t.Context(), []core.ToolCallItem{{ID: "batch", Name: "task_batch"}})[0]
				if result.NonExecution.Stage != "mode" {
					t.Errorf("batch reached the tool implementation: %+v", result)
				}
			}
		})
	}
}
