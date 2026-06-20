package textutil

import "testing"

func TestStripAnsi(t *testing.T) {
	tests := []struct{ in, want string }{
		{"hello", "hello"},
		{"\x1b[31mred\x1b[0m", "red"},
		{"no ansi here", "no ansi here"},
	}
	for _, tt := range tests {
		if got := StripAnsi(tt.in); got != tt.want {
			t.Errorf("StripAnsi(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNormalizeText(t *testing.T) {
	got := NormalizeText("\x1b[31ma\r\nb\x1b[0m")
	if got != "a\nb" {
		t.Fatalf("NormalizeText() = %q", got)
	}
}
