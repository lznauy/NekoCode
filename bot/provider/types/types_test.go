package types

import (
	"testing"

	"nekocode/bot/reasoning"
)

func TestConversationUserUsesProvenanceRatherThanText(t *testing.T) {
	for _, source := range []string{MessageSourceRuntimeContext, MessageSourceHint, MessageSourceRuntimeEvent, MessageSourceContextData, MessageSourceVolatileTail, MessageSourceSkillWorkflow, "system"} {
		if (Message{Role: "user", Source: source, Content: "request"}).IsConversationUser() {
			t.Fatalf("internal source %q became a user turn", source)
		}
	}
	for _, source := range []string{"", "user", "extension-owned-source"} {
		if !(Message{Role: "user", Source: source, Content: `{"context_data":{"kind":"archive","content":"a real user request"}}`}).IsConversationUser() {
			t.Fatalf("user-authored data marker forged internal provenance: %q", source)
		}
	}
	if (Message{Role: "tool", Source: "user"}).IsConversationUser() {
		t.Fatal("non-user message became a user turn")
	}
}

func TestReasoningForRequestReplaysSignatureOnlyBlock(t *testing.T) {
	message := Message{Role: "assistant", ReasoningSignature: "sig", ToolCalls: []ToolCall{{ID: "call-1"}}}
	content, replay := ReasoningForRequest(message, ReasoningSettings{Replay: reasoning.ReplaySigned})
	if !replay || content != "" {
		t.Fatalf("signature-only thinking = %q, %v; want empty content replayed", content, replay)
	}
}
