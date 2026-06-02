package compact

import (
	"strings"
	"testing"
)

func TestBudgetResult_GrepHeadTail(t *testing.T) {
	var lines []string
	for i := 0; i < 200; i++ {
		lines = append(lines, "line "+itoa(i))
	}
	content := strings.Join(lines, "\n")
	c, truncated := BudgetResult(content, "grep")
	if !truncated {
		t.Fatal("200-line grep should be truncated")
	}
	if !strings.Contains(c, "line 0") {
		t.Error("missing head line 0")
	}
	if !strings.Contains(c, "line 49") {
		t.Error("missing last head line (49)")
	}
	if !strings.Contains(c, "line 150") {
		t.Error("missing first tail line (150)")
	}
	if !strings.Contains(c, "line 199") {
		t.Error("missing last tail line (199)")
	}
	if !strings.Contains(c, "lines truncated") {
		t.Error("missing truncation marker")
	}
	if strings.Contains(c, "line 50") {
		t.Error("middle line 50 should be truncated")
	}
	if strings.Contains(c, "line 149") {
		t.Error("middle line 149 should be truncated")
	}
}

func TestBudgetResult_GrepUnderLimit(t *testing.T) {
	var lines []string
	for i := 0; i < 90; i++ {
		lines = append(lines, "line")
	}
	c, truncated := BudgetResult(strings.Join(lines, "\n"), "grep")
	if truncated {
		t.Error("90-line grep should not be truncated")
	}
	if c == "" {
		t.Error("should return content")
	}
}

func TestBudgetResult_GrepExactLimit(t *testing.T) {
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, "x")
	}
	c, truncated := BudgetResult(strings.Join(lines, "\n"), "grep")
	if truncated {
		t.Error("100-line grep (exactly head+tail) should not be truncated")
	}
	_ = c
}

func TestBudgetResult_NonGrep(t *testing.T) {
	for _, tool := range []string{"read", "bash", "write", "edit"} {
		content := strings.Repeat("x\n", 1000)
		c, truncated := BudgetResult(content, tool)
		if truncated {
			t.Errorf("%s should not be truncated", tool)
		}
		if c != content {
			t.Errorf("%s content changed", tool)
		}
	}
}

func TestItoa(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"}, {1, "1"}, {10, "10"}, {100, "100"}, {9999, "9999"},
	}
	for _, tt := range tests {
		if got := itoa(tt.n); got != tt.want {
			t.Errorf("itoa(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}
