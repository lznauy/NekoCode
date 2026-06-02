package token

import (
	"testing"

	"nekocode/llm/types"
)

func TestEstimateString_Empty(t *testing.T) {
	if n := EstimateString(""); n != 0 {
		t.Errorf("empty string = %d, want 0", n)
	}
}

func TestEstimateString_ASCII(t *testing.T) {
	// 16 ASCII chars → 16/4 = 4 tokens
	n := EstimateString("hello world test!")
	if n < 3 || n > 5 {
		t.Errorf("16-char ASCII = %d, want ~4", n)
	}
}

func TestEstimateString_CJK(t *testing.T) {
	// 3 CJK chars → (3*2+2)/3 = 8/3 ≈ 2 tokens
	n := EstimateString("你好吗")
	if n < 1 || n > 3 {
		t.Errorf("3-char CJK = %d, want ~2", n)
	}
}

func TestEstimateString_Mixed(t *testing.T) {
	n := EstimateString("hello世界")
	if n <= 0 {
		t.Error("mixed string should produce tokens > 0")
	}
}

func TestEstimateTokens_Empty(t *testing.T) {
	if n := EstimateTokens(nil); n != 0 {
		t.Errorf("nil = %d, want 0", n)
	}
	if n := EstimateTokens([]types.Message{}); n != 0 {
		t.Errorf("empty = %d, want 0", n)
	}
}

func TestEstimateTokens_WithToolCalls(t *testing.T) {
	msgs := []types.Message{
		{Role: "assistant", Content: "let me check", ToolCalls: []types.ToolCall{
			{ID: "tc1", Function: types.FunctionCall{Name: "read", Arguments: `{"path":"/x"}`}},
		}},
	}
	n := EstimateTokens(msgs)
	if n <= 0 {
		t.Error("messages with tool calls should have tokens")
	}
}

func TestEstimateTokens_MultipleMessages(t *testing.T) {
	msgs := []types.Message{
		{Role: "system", Content: "you are helpful"},
		{Role: "user", Content: "hello"},
	}
	n1 := EstimateTokens(msgs[:1])
	n2 := EstimateTokens(msgs)
	if n2 <= n1 {
		t.Errorf("2 msgs (%d) should have more tokens than 1 (%d)", n2, n1)
	}
}

