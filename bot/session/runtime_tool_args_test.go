package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/bot/policy/ledger"
	"nekocode/protocol"
)

func TestSaveFailedTaskExcludesExecutionCallbacks(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(map[bool]string{false: "error", true: "blocked"}[blocked], func(t *testing.T) {
			invoked := false
			callback := taskbridge.TaskCallback{Callback: func(protocol.StepEvent) { invoked = true }}
			args := map[string]any{"prompt": "inspect", "_id": "user", "_preview": "display", "_sub_callback": callback}
			audit := ledger.New()
			audit.RecordTool(ledger.ToolEvent{Name: "task", Args: args, Error: "task failed", Blocked: blocked})
			snapshot := newSnapshot(t.TempDir())
			snapshot.Ledger = audit.Snapshot()
			root := t.TempDir()
			if err := snapshot.saveAt(root); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(root, snapshot.ID, "session.json"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "_sub_callback") || strings.Contains(string(data), "_preview") {
				t.Fatalf("execution metadata persisted: %s", data)
			}
			loaded, err := loadModeAt(root, snapshot.ID, false)
			if err != nil {
				t.Fatal(err)
			}
			events := loaded.Ledger.ToolErrors
			if blocked {
				events = loaded.Ledger.BlockedTools
			}
			if len(events) != 1 || events[0].Args["prompt"] != "inspect" || events[0].Args["_id"] != "user" {
				t.Fatalf("lost audit args: %+v", events)
			}
			args["_sub_callback"].(taskbridge.TaskCallback).Callback(protocol.StepEvent{})
			if !invoked {
				t.Fatal("execution callback lost")
			}
		})
	}
}
