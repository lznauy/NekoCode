package compact

import (
	"strings"
	"testing"

	"nekocode/bot/llm/types"
)

func TestExtractXMLBlock(t *testing.T) {
	raw := "<summary>compressed content here</summary>"
	if s := extractXMLBlock(raw, "summary"); s != "compressed content here" {
		t.Errorf("extractXMLBlock = %q", s)
	}
}

func TestExtractXMLBlock_Missing(t *testing.T) {
	if s := extractXMLBlock("no tags here", "summary"); s != "" {
		t.Errorf("expected empty for missing tag: %q", s)
	}
}

func TestExtractXMLBlock_Unclosed(t *testing.T) {
	if s := extractXMLBlock("<summary>no closing", "summary"); s != "" {
		t.Errorf("expected empty for unclosed tag: %q", s)
	}
}

func TestExtractXMLBlock_Nested(t *testing.T) {
	raw := "<summary>text with <inner>stuff</inner></summary>"
	s := extractXMLBlock(raw, "summary")
	if !strings.Contains(s, "<inner>") {
		t.Errorf("nested content preserved: %q", s)
	}
}

func TestFormatCompactSummary(t *testing.T) {
	raw := "prefix<summary>the summary</summary>suffix"
	if s := FormatCompactSummary(raw); s != "the summary" {
		t.Errorf("FormatCompactSummary = %q", s)
	}
}

func TestFormatCompactSummary_Empty(t *testing.T) {
	if s := FormatCompactSummary("no summary tag"); s != "" {
		t.Errorf("expected empty: %q", s)
	}
}

func TestFormatMessages(t *testing.T) {
	msgs := []types.Message{
		{Role: "user", Content: "hello"},
		{Role: "tool", Content: ClearedMarker},
		{Role: "assistant", Content: "world"},
	}
	s := FormatMessages(msgs)
	if !strings.Contains(s, "[user]: hello") {
		t.Error("should contain user message")
	}
	if !strings.Contains(s, "[assistant]: world") {
		t.Error("should contain assistant message")
	}
	if strings.Contains(s, ClearedMarker) {
		t.Error("should skip cleared markers")
	}
}

func TestBuildPrompt(t *testing.T) {
	msgs := []types.Message{
		{Role: "user", Content: "find the bug"},
		{Role: "assistant", Content: "I found it in main.go"},
	}
	p := BuildPrompt(msgs, "previous summary")
	if !strings.Contains(p, "find the bug") {
		t.Error("prompt should contain message content")
	}
	if !strings.Contains(p, "previous summary") {
		t.Error("prompt should contain previous summary")
	}
	if !strings.Contains(p, NO_TOOLS_PREAMBLE) {
		t.Error("prompt should contain no-tools preamble")
	}
}

func TestBuildPrompt_SkipsCleared(t *testing.T) {
	msgs := []types.Message{
		{Role: "tool", Content: ClearedMarker},
		{Role: "user", Content: "real content"},
	}
	p := BuildPrompt(msgs, "")
	if strings.Contains(p, ClearedMarker) {
		t.Error("prompt should skip cleared markers")
	}
	if !strings.Contains(p, "real content") {
		t.Error("prompt should include real content")
	}
}

func TestBuildPrompt_SkipsDot(t *testing.T) {
	msgs := []types.Message{
		{Role: "assistant", Content: "."},
		{Role: "user", Content: "hello"},
	}
	p := BuildPrompt(msgs, "")
	if strings.Contains(p, "[assistant]: .") {
		t.Error("prompt should skip dot-only content")
	}
}

func TestBuildPrompt_Empty(t *testing.T) {
	p := BuildPrompt(nil, "")
	if !strings.Contains(p, NO_TOOLS_PREAMBLE) {
		t.Error("empty prompt should still have preamble")
	}
}
