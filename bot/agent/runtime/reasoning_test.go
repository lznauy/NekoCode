package runtime

import "testing"

func TestIsGarbledToolCall(t *testing.T) {
	for _, text := range []string{
		`<invoke name="read">`,
		`{"tool_calls":[{"name":"read"}]}`,
		`<tool_call>`,
	} {
		if !isGarbledToolCall(text) {
			t.Fatalf("expected garbled tool call for %q", text)
		}
	}
	if isGarbledToolCall("normal assistant text") {
		t.Fatal("normal text should not be garbled")
	}
}
