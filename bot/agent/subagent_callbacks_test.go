package agent

import (
	"testing"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/protocol"
)

func TestSubagentCallbackLifecycle(t *testing.T) {
	for _, mode := range []string{"resolved", "legacy", "validation_error"} {
		t.Run(mode, func(t *testing.T) {
			r := newToolRunner(&Agent{deps: agentDeps{subSlotMgr: newSlotManager()}})
			var events []protocol.StepEvent
			cleanup, denied, calls := r.prepareSubagentCallbacks([]core.ToolCallItem{{
				Name: "task", Args: map[string]any{"profile": "reviewer"},
			}}, []int{0}, func(ev protocol.StepEvent) { events = append(events, ev) })
			if len(denied) != 0 || len(calls) != 1 || len(events) != 0 {
				t.Fatal("expected a reserved slot without premature start", denied, events)
			}
			delegated := calls[0].Args["_sub_callback"].(taskbridge.TaskCallback)
			cb := delegated.Callback
			wantProfile := "reviewer"
			if mode == "resolved" {
				wantProfile = "demo/reviewer"
				start := protocol.StepEvent{Action: protocol.StepActionSubAgentStart, SubAgentProfile: wantProfile}
				cb(start)
				cb(start) // Duplicate starts must not replace the running record.
			}
			if mode != "validation_error" {
				cb(protocol.StepEvent{Action: protocol.StepActionExecuteTool, ToolName: "read"})
			}
			cleanup()
			wantCount := 3
			if mode == "validation_error" {
				wantCount = 2
			}
			if len(events) != wantCount || events[0].Action != protocol.StepActionSubAgentStart || events[len(events)-1].Action != protocol.StepActionSubAgentEnd {
				t.Fatalf("unpaired lifecycle: %+v", events)
			}
			if events[0].SubAgentProfile != wantProfile || events[0].SubAgentID == "" {
				t.Fatalf("incorrect start metadata: %+v", events[0])
			}
			if delegated.ID != events[0].SubAgentID {
				t.Fatal("delegated interaction identity differs from event identity")
			}
			for _, ev := range events {
				if ev.SubAgentID != events[0].SubAgentID {
					t.Fatal("subagent ID changed")
				}
			}
		})
	}
}
