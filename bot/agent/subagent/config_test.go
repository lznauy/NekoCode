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
	}}
	inspect := &countingTool{name: "inspect"}
	engine := New(Config{LLM: llm, Tools: tools.New(inspect)})
	result, err := engine.Run(t.Context(), RunConfig{Prompt: "Inspect", Profile: Profile{Name: "bounded", Tools: []string{"inspect"}, MaxSteps: 1}})
	if err != nil || result.Status != StatusPartial || llm.calls != 1 || inspect.calls != 1 {
		t.Fatalf("step limit result=%+v err=%v calls=%d", result, err, llm.calls)
	}
	for _, limit := range []int{-1, maxSubAgentSteps + 1} {
		if _, err := (&Engine{}).Run(t.Context(), RunConfig{Profile: Profile{MaxSteps: limit}}); err == nil {
			t.Fatalf("accepted %d", limit)
		}
	}
}
