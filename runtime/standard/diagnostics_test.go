package standard

import (
	"fmt"
	"testing"

	"nekocode/bot/core"
	controlruntime "nekocode/runtime"
)

func assertFixtureModel(t *testing.T, bot *core.Bot, active, baseURL string) {
	t.Helper()
	cfg := bot.Configuration()
	model := cfg.ActiveModelConfig()
	if cfg.Active != active || model.BaseURL != baseURL {
		t.Fatalf("fixture model was not activated: active=%q base_url=%q; want active=%q base_url=%q", cfg.Active, model.BaseURL, active, baseURL)
	}
}

// Include terminal errors instead of reporting an empty task list as though
// admission, provider selection, model calls and task assembly were equivalent.
func runtimeTestDiagnostic(rt *controlruntime.Runtime) string {
	type runInfo struct {
		ID     controlruntime.RunID
		Status controlruntime.RunStatus
		Error  string
	}
	current, found := rt.CurrentRun()
	runs := []runInfo{}
	for _, run := range rt.Runs(6) {
		runs = append(runs, runInfo{run.ID, run.Status, run.Error})
	}
	tasks, taskErr := rt.BackgroundTasks("")
	return fmt.Sprintf("runtime=%+v current=%+v found=%t recent_runs=%+v tasks=%+v task_error=%v", rt.Status(), runInfo{current.ID, current.Status, current.Error}, found, runs, tasks, taskErr)
}
