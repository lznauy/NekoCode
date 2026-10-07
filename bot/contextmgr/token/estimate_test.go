package token

import (
	"testing"

	"nekocode/bot/provider/types"
	"nekocode/bot/reasoning"
)

func TestEstimateString_Empty(t *testing.T) {
	if n := EstimateString(""); n != 0 {
		t.Errorf("empty string = %d, want 0", n)
	}
}

func TestEstimateImageTokens(t *testing.T) {
	if got := EstimateImageTokens(types.MessageImage{Width: 1568, Height: 1568}); got != 1568*1568/750 {
		t.Fatalf("estimate = %d, want %d", got, 1568*1568/750)
	}
	if got := EstimateImageTokens(types.MessageImage{}); got != 1568*1568/750 {
		t.Fatalf("dimensionless estimate = %d, want the storage-cap fallback %d", got, 1568*1568/750)
	}
}

func TestEstimateTokensIncludesImages(t *testing.T) {
	without := EstimateTokens([]types.Message{{Role: "user", Content: "hi"}})
	with := EstimateTokens([]types.Message{{Role: "user", Content: "hi", Images: []types.MessageImage{{Path: "/tmp/a.png", Width: 800, Height: 600}}}})
	if with-without != 800*600/750 {
		t.Fatalf("image token delta = %d, want %d", with-without, 800*600/750)
	}
}

func TestEstimateModelTokensUsesReasoningReplayPolicy(t *testing.T) {
	plain := types.Message{Role: "assistant", Content: "answer", ReasoningContent: "a long private chain of thought"}
	toolCall := types.Message{Role: "assistant", ReasoningContent: "tool reasoning", ToolCalls: []types.ToolCall{{ID: "call-1"}}}
	settings := types.ReasoningSettings{Replay: reasoning.ReplayToolCalls}

	withoutReasoning := EstimateModelTokens([]types.Message{plain}, settings, false)
	if full := EstimateTokens([]types.Message{plain}); withoutReasoning >= full {
		t.Fatalf("plain reasoning was not excluded: model=%d full=%d", withoutReasoning, full)
	}
	if got, full := EstimateModelTokens([]types.Message{toolCall}, settings, false), EstimateTokens([]types.Message{toolCall}); got != full {
		t.Fatalf("tool-call reasoning estimate = %d, want full replay estimate %d", got, full)
	}
}

func TestEstimateModelTokensChargesImagesOnlyOnVision(t *testing.T) {
	msg := []types.Message{{Role: "user", Content: "hi", Images: []types.MessageImage{{Path: "/tmp/a.png", Width: 800, Height: 600}}}}
	nonVision := EstimateModelTokens(msg, types.ReasoningSettings{}, false)
	vision := EstimateModelTokens(msg, types.ReasoningSettings{}, true)
	if vision-nonVision != 800*600/750 {
		t.Fatalf("vision delta = %d, want %d", vision-nonVision, 800*600/750)
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
