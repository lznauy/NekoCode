package model

import "testing"

func TestIsGarbledToolCall(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"<invoke name=\"read\"></invoke>", true},
		{"{\"tool_calls\":[{}]}", true},
		{"normal answer", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsGarbledToolCall(tc.text); got != tc.want {
			t.Fatalf("IsGarbledToolCall(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}
