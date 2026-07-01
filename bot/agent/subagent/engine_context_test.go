package subagent

import (
	"strings"
	"testing"
)

func TestBuildSystemPromptAddsHandoff(t *testing.T) {
	cfg := RunConfig{
		AgentType: AgentType{
			Name:         "executor",
			SystemPrompt: "base prompt",
		},
		Cwd:            "/repo",
		ProjectContext: "project info",
		Handoff:        "prior findings",
	}

	got := buildSystemPrompt(cfg)
	for _, want := range []string{"base prompt", "<cwd>/repo</cwd>", "project info", "<handoff>", "prior findings"} {
		if !strings.Contains(got, want) {
			t.Fatalf("system prompt = %q, want %q", got, want)
		}
	}
}

func TestBuildSystemPromptExpandsDeepResearcher(t *testing.T) {
	cfg := RunConfig{
		AgentType: AgentType{
			Name:         "researcher",
			SystemPrompt: `Focus on the specific question. For "very thorough": search across multiple directories and naming conventions.`,
		},
		Thoroughness: thoroughDeep,
	}

	got := buildSystemPrompt(cfg)
	if !strings.Contains(got, "Search across ALL packages") {
		t.Fatalf("system prompt = %q, want deep researcher instruction", got)
	}
}
