package toolpolicy

import (
	"strings"
	"testing"
)

func TestHasSufficientEditAnchor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args map[string]any
		want bool
	}{
		{
			name: "empty",
			args: map[string]any{"oldString": "   "},
			want: false,
		},
		{
			name: "short single line",
			args: map[string]any{"oldString": "return nil"},
			want: false,
		},
		{
			name: "long content",
			args: map[string]any{"oldString": strings.Repeat("x", 200)},
			want: true,
		},
		{
			name: "five non-empty lines",
			args: map[string]any{"oldString": "a\n\nb\nc\nd\ne"},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := HasSufficientEditAnchor(tt.args); got != tt.want {
				t.Fatalf("HasSufficientEditAnchor() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractTargetPath(t *testing.T) {
	t.Parallel()

	args := map[string]any{"path": "bot/app/app.go"}
	if got := ExtractTargetPath("edit", args); got != "bot/app/app.go" {
		t.Fatalf("edit target = %q", got)
	}
	if got := ExtractTargetPath("write", args); got != "bot/app/app.go" {
		t.Fatalf("write target = %q", got)
	}
	if got := ExtractTargetPath("read", args); got != "" {
		t.Fatalf("read target = %q, want empty", got)
	}
}
