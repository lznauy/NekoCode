package agent

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"nekocode/bot/agent/subagent"
	"nekocode/bot/contextmgr"
	tools "nekocode/bot/extension/tool"
	tasktool "nekocode/bot/extension/tool/builtin/task"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

type evidenceSequenceLLM struct {
	fakeLLM
	scripts [][]types.StreamToken
}

func (m *evidenceSequenceLLM) ChatStream(ctx context.Context, messages []types.Message, defs []types.ToolDef) (<-chan types.StreamToken, <-chan error) {
	m.tokens = m.scripts[m.calls]
	return m.fakeLLM.ChatStream(ctx, messages, defs)
}

type evidenceCountingTool struct{ calls int }

func (*evidenceCountingTool) Name() string                 { return "inspect" }
func (*evidenceCountingTool) Description() string          { return "fixture" }
func (*evidenceCountingTool) Parameters() []core.Parameter { return nil }
func (*evidenceCountingTool) ExecutionMode(map[string]any) core.ExecutionMode {
	return core.ModeParallel
}
func (f *evidenceCountingTool) Execute(context.Context, map[string]any) (core.ToolOutput, error) {
	f.calls++
	return core.ToolOutput{Text: "executed"}, nil
}

func TestChildSaveFailureSurvivesTaskDeliveryAndParentAnswer(t *testing.T) {
	for _, storageFailure := range []bool{true, false} {
		t.Run(fmt.Sprint(storageFailure), func(t *testing.T) {
			cause := errors.New("fixture failure")
			childTool := &evidenceCountingTool{}
			childModel := &evidenceSequenceLLM{scripts: [][]types.StreamToken{{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "child-call", Name: "inspect", Arguments: `{}`}}}}}
			task := tasktool.NewTaskTool()
			task.Wire(func(ctx context.Context, _ taskbridge.TaskSpec) (*taskbridge.TaskResult, error) {
				if !storageFailure {
					return &taskbridge.TaskResult{Status: taskbridge.TaskStatusFailed}, cause
				}
				_, err := subagent.New(subagent.Config{LLM: childModel, Tools: tools.New(childTool)}).Run(ctx, subagent.RunConfig{
					SessionID: "parent", SubagentID: "child", AttemptID: "attempt", Profile: subagent.Profile{Name: "custom", Tools: []string{"inspect"}},
					SaveContext: func(snapshot contextmgr.ManagerSnapshot, _, _ string, _ subagent.AttemptUsage) error {
						for _, msg := range snapshot.Transcript {
							if len(msg.ToolEvidence.Executions) > 0 {
								return cause
							}
						}
						return nil
					},
				})
				return &taskbridge.TaskResult{Status: taskbridge.TaskStatusFailed}, err
			})
			model := &evidenceSequenceLLM{scripts: [][]types.StreamToken{
				{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "task-call", Name: "task", Arguments: `{"profile":"explore","prompt":"inspect"}`}}},
				{{Content: "parent answer"}}, {{Content: "next answer"}},
			}}
			agent := New(t.Context(), Config{Model: model, Context: contextmgr.New(contextmgr.Config{ContextWindow: 128000}), Tools: tools.New(task)})
			result := agent.Run("run", nil, nil)
			if result.FinalOutput != "parent answer" || protocol.EvidenceIncomplete(result.Error) != storageFailure || (storageFailure && !errors.Is(result.Error, cause)) || (!storageFailure && result.Error != nil) {
				t.Fatalf("result=%+v", result)
			}
			if storageFailure && (childTool.calls != 1 || childModel.calls != 1) {
				t.Fatal("child tool was replayed")
			}
			if next := agent.Run("next", nil, nil); next.Error != nil || next.FinalOutput != "next answer" {
				t.Fatalf("previous run error leaked: %+v", next)
			}
		})
	}
}
