package bot

import "testing"

func TestSourceToRawURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// github.com without branch → defaults to main.
		{
			"https://github.com/user/repo",
			"https://raw.githubusercontent.com/user/repo/main/.claude-plugin/plugin.json",
		},
		// github.com with tree/main branch.
		{
			"https://github.com/user/repo/tree/main",
			"https://raw.githubusercontent.com/user/repo/main/.claude-plugin/plugin.json",
		},
		// github.com with tree/master branch.
		{
			"https://github.com/user/repo/tree/master",
			"https://raw.githubusercontent.com/user/repo/master/.claude-plugin/plugin.json",
		},
		// github.com with tree/custom-branch.
		{
			"https://github.com/user/repo/tree/develop",
			"https://raw.githubusercontent.com/user/repo/develop/.claude-plugin/plugin.json",
		},
		// github.com with .git suffix.
		{
			"https://github.com/user/repo.git",
			"https://raw.githubusercontent.com/user/repo/main/.claude-plugin/plugin.json",
		},
		// github.com with trailing slash.
		{
			"https://github.com/user/repo/",
			"https://raw.githubusercontent.com/user/repo/main/.claude-plugin/plugin.json",
		},
		// Already raw URL — add plugin.json path.
		{
			"https://raw.githubusercontent.com/user/repo/main",
			"https://raw.githubusercontent.com/user/repo/main/.claude-plugin/plugin.json",
		},
		// Already raw URL with full path — no change needed.
		{
			"https://raw.githubusercontent.com/user/repo/main/.claude-plugin/plugin.json",
			"https://raw.githubusercontent.com/user/repo/main/.claude-plugin/plugin.json",
		},
		// Non-GitHub URL returns empty.
		{
			"https://gitlab.com/user/repo",
			"",
		},
		// Just user/repo string returns empty (not a URL).
		{
			"user/repo",
			"",
		},
		// http scheme.
		{
			"http://github.com/user/repo",
			"https://raw.githubusercontent.com/user/repo/main/.claude-plugin/plugin.json",
		},
	}
	for _, tt := range tests {
		got := sourceToRawURL(tt.input)
		if got != tt.want {
			t.Errorf("sourceToRawURL(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsLocalPath(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"./local/dir", true},
		{"/absolute/path", true},
		{"~/home/dir", true},
		{"single-component", true},     // no slash, not git-like
		{"user/repo", false},            // looks like git user/repo
		{"https://github.com/x", false}, // URL
		{"org/project", false},          // looks like git user/repo
	}
	for _, tt := range tests {
		if got := isLocalPath(tt.input); got != tt.want {
			t.Errorf("isLocalPath(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
