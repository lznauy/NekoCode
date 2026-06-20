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
		Handoff: "prior findings",
	}

	got := buildSystemPrompt(cfg)
	if !strings.Contains(got, "base prompt") || !strings.Contains(got, "<handoff>") || !strings.Contains(got, "prior findings") {
		t.Fatalf("system prompt = %q, want base prompt with handoff", got)
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
