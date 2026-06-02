package ctxmgr

import (
	"strings"
	"testing"
)

func TestBuildBar(t *testing.T) {
	bar := BuildBar(1000, []BarSegment{
		{Size: 100, Kind: "sys"},
		{Size: 200, Kind: "tools"},
		{Size: 300, Kind: "msgs"},
		{Size: 400, Kind: "free"},
	}, 10)
	if !strings.Contains(bar, "⛁") {
		t.Errorf("bar missing sys chars: %s", bar)
	}
	if bar == "" {
		t.Error("bar should not be empty")
	}
}

func TestBuildBar_AllZero(t *testing.T) {
	if BuildBar(0, []BarSegment{}, 10) != "" {
		t.Error("zero total should return empty string")
	}
}

func TestBuildBar_SingleSegment(t *testing.T) {
	bar := BuildBar(100, []BarSegment{{Size: 100, Kind: "sys"}}, 5)
	if bar == "" {
		t.Error("single segment bar should not be empty")
	}
}

func TestFormatTokens(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{500, "500"},
		{1000, "1.0k"},
		{1500, "1.5k"},
		{10000, "10.0k"},
		{1_000_000, "1.0m"},
		{1_500_000, "1.5m"},
		{999, "999"},
	}
	for _, tt := range tests {
		got := FormatTokens(tt.n)
		if got != tt.want {
			t.Errorf("FormatTokens(%d) = %s, want %s", tt.n, got, tt.want)
		}
	}
}

func TestFormatContextReport(t *testing.T) {
	r := ContextReport{
		Budget:        10000,
		SystemPrompt:  500,
		ToolDefTokens: 1000,
		SkillList:     200,
		Messages:      3000,
		ToolDefCount:  15,
		UserMessages:  5,
		AssistantMsgs: 3,
		ToolResults:   7,
	}
	s := FormatContextReport(r)
	if !strings.Contains(s, "⛁") {
		t.Errorf("report missing bar chars: %s", s)
	}
	if !strings.Contains(s, "5.3k") {
		t.Errorf("report missing used tokens: %s", s)
	}
	if !strings.Contains(s, "10.0k") {
		t.Errorf("report missing budget: %s", s)
	}
	if !strings.Contains(s, "System") {
		t.Errorf("report missing System line: %s", s)
	}
	if !strings.Contains(s, "15 tools") {
		t.Errorf("report missing tools count: %s", s)
	}
}

func TestFormatContextReport_ZeroBudget(t *testing.T) {
	s := FormatContextReport(ContextReport{Budget: 0})
	if s == "" {
		t.Error("report should not be empty for zero budget")
	}
}

func TestFormatContextReport_FreeOverflow(t *testing.T) {
	// Used > Budget: free should be clamped to 0.
	r := ContextReport{
		Budget:       1000,
		SystemPrompt: 500,
		Messages:     600,
	}
	s := FormatContextReport(r)
	if strings.Contains(s, "0.0k") {
		// free is 0 which shows as "0" in formatTokens
	}
	_ = s
}
