package subagent

import (
	"testing"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

func TestSubagentNonExecutionRecordsKeepAttemptAndEventIdentity(t *testing.T) {
	for _, stage := range []string{"profile", "completion_protocol"} {
		t.Run(stage, func(t *testing.T) {
			name := "inspect"
			if stage == "completion_protocol" {
				name = submitResultToolName
			}
			model := &scriptedLLM{scripts: [][]types.StreamToken{
				{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "same-call", Name: name, Arguments: `{}`}}}, completionScript(),
			}}
			var event ToolCallEvent
			var saved protocol.ToolNonExecution
			result, err := New(Config{LLM: model, Tools: tools.New(testProxyTool{name: "inspect"})}).Run(t.Context(), RunConfig{
				Prompt: "inspect", SubagentID: "child", AttemptID: "attempt", Profile: Profile{Name: "test"},
				OnToolCall: func(ev ToolCallEvent) {
					if ev.Action == protocol.StepActionToolBlocked {
						event = ev
					}
				},
				SaveContext: func(snapshot ctxmgr.ManagerSnapshot, _, _ string, _ AttemptUsage) error {
					for _, msg := range snapshot.Transcript {
						if msg.ToolCallID == "same-call" {
							saved = msg.NonExecution
							if msg.ToolEvidence.Schema != 0 {
								t.Fatal("blocked call acquired execution receipt")
							}
						}
					}
					return nil
				},
			})
			if err != nil || result.Status != StatusCompleted {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if saved.Schema != 1 || saved != event.NonExecution || saved.Stage != stage || saved.Execution.TaskID != "child" || saved.Execution.AttemptID != "attempt" {
				t.Fatalf("saved=%+v event=%+v", saved, event)
			}
		})
	}
}

func TestCompletionProtocolRejectionPreservesEffectiveToolIdentity(t *testing.T) {
	registry := tools.New()
	registry.RegisterWithOptions(testProxyTool{name: "capability"}, tools.RegistrationOptions{ResolveTarget: func(args map[string]any) (tools.CallTarget, bool) {
		return tools.CallTarget{Name: "mcp__demo__lookup", Args: args}, true
	}})
	manager := ctxmgr.New(ctxmgr.Config{})
	var event ToolCallEvent
	run := &engineRun{engine: New(Config{Tools: registry}), ctx: t.Context(), ctxMgr: manager, cfg: RunConfig{OnToolCall: func(ev ToolCallEvent) { event = ev }}}
	run.rejectToolBatch([]core.ToolCallItem{{ID: "call", Name: "capability"}}, "invalid completion batch")
	record := manager.Snapshot().Transcript[0].NonExecution
	if record.Tool != "mcp__demo__lookup" || event.ToolName != record.Tool || record != event.NonExecution {
		t.Fatalf("record=%+v event=%+v", record, event)
	}
}
