package subagent

import (
	"nekocode/bot/extension/agentprofile"
	"nekocode/bot/extension/tool"
	"nekocode/bot/provider/types"
	"strings"
	"testing"
)

func TestCoreContractIncludedForEveryProfile(t *testing.T) {
	profiles := append(agentprofile.Builtins(), Profile{Name: "plugin/review", SystemPrompt: "Review carefully."})
	for _, p := range profiles {
		got := buildSystemPrompt(RunConfig{Profile: p})
		for _, want := range []string{"通用子 Agent", "实际可见工具是权限边界", "最终消息", "Completion protocol", p.SystemPrompt} {
			if !strings.Contains(got, want) {
				t.Fatalf("%s missing %q", p.Name, want)
			}
		}
		if strings.Count(got, builtinPrompt) != 1 {
			t.Fatalf("duplicate core contract: %s", p.Name)
		}
	}
}

func TestProfileStepLimit(t *testing.T) {
	llm := &scriptedLLM{scripts: [][]types.StreamToken{
		{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "one", Name: "inspect", Arguments: `{}`}}},
		{{Content: "still investigating"}},
	}}
	inspect := &countingTool{name: "inspect"}
	engine := New(Config{LLM: llm, Tools: tools.New(inspect)})
	result, err := engine.Run(t.Context(), RunConfig{Prompt: "Inspect", Profile: Profile{Name: "bounded", Tools: []string{"inspect"}, MaxSteps: 1}})
	if err != nil || result.Status != StatusPartial || llm.calls != 2 || inspect.calls != 1 {
		t.Fatalf("step limit result=%+v err=%v calls=%d", result, err, llm.calls)
	}
	for _, limit := range []int{-1, agentprofile.MaxSteps + 1} {
		if _, err := (&Engine{}).Run(t.Context(), RunConfig{Profile: Profile{MaxSteps: limit}}); err == nil {
			t.Fatalf("accepted %d", limit)
		}
	}
}

func TestProfileAcceptsRaisedStepLimit(t *testing.T) {
	llm := &scriptedLLM{scripts: [][]types.StreamToken{
		{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "result", Name: submitResultToolName, Arguments: `{"summary":"done","evidence":[],"files":[],"verification":"not run","unfinished":[],"risks":[]}`}}},
	}}
	engine := New(Config{LLM: llm, Tools: tools.New()})
	result, err := engine.Run(t.Context(), RunConfig{Prompt: "Inspect", Profile: Profile{Name: "extended", MaxSteps: agentprofile.DefaultMaxSteps + 1}})
	if err != nil || result.Status != StatusCompleted {
		t.Fatalf("raised step limit result=%+v err=%v", result, err)
	}
}

func TestStepLimitAllowsFinalStructuredResult(t *testing.T) {
	llm := &scriptedLLM{scripts: [][]types.StreamToken{
		{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "one", Name: "inspect", Arguments: `{}`}}},
		{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "result", Name: submitResultToolName, Arguments: `{"summary":"completed evidence","evidence":["inspection output"],"files":[],"verification":"not run","unfinished":[],"risks":[]}`}}},
	}}
	inspect := &countingTool{name: "inspect"}
	engine := New(Config{LLM: llm, Tools: tools.New(inspect)})
	result, err := engine.Run(t.Context(), RunConfig{Prompt: "Inspect", Profile: Profile{Name: "bounded", Tools: []string{"inspect"}, MaxSteps: 1}})
	if err != nil || result.Status != StatusCompleted || result.Handoff == nil || result.Handoff.Summary != "completed evidence" {
		t.Fatalf("step limit result=%+v err=%v", result, err)
	}
	if llm.calls != 2 || inspect.calls != 1 {
		t.Fatalf("LLM calls=%d tool calls=%d", llm.calls, inspect.calls)
	}
	if len(llm.tools[1]) != 1 || !toolDefsContain(llm.tools[1], submitResultToolName) {
		t.Fatalf("finalization tools = %+v, want only submit_result", llm.tools[1])
	}
}
