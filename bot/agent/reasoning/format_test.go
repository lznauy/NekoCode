package reasoning

import "testing"

func TestIsGarbledToolCall(t *testing.T) {
	for _, text := range []string{
		`<invoke name="read">`,
		`{"tool_calls":[{"name":"read"}]}`,
		`<tool_call>read</tool_call>`,
	} {
		if !IsGarbledToolCall(text) {
			t.Fatalf("expected garbled tool call for %q", text)
		}
	}
	if IsGarbledToolCall("normal assistant text") {
		t.Fatal("normal text should not be garbled")
	}
}
