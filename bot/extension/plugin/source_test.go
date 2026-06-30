package plugin

import "testing"

func TestSourceToRawURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://github.com/user/repo", "https://raw.githubusercontent.com/user/repo/main/.claude-plugin/plugin.json"},
		{"https://github.com/user/repo/tree/main", "https://raw.githubusercontent.com/user/repo/main/.claude-plugin/plugin.json"},
		{"https://github.com/user/repo/tree/master", "https://raw.githubusercontent.com/user/repo/master/.claude-plugin/plugin.json"},
		{"https://github.com/user/repo/tree/develop", "https://raw.githubusercontent.com/user/repo/develop/.claude-plugin/plugin.json"},
		{"https://github.com/user/repo.git", "https://raw.githubusercontent.com/user/repo/main/.claude-plugin/plugin.json"},
		{"https://github.com/user/repo/", "https://raw.githubusercontent.com/user/repo/main/.claude-plugin/plugin.json"},
		{"https://raw.githubusercontent.com/user/repo/main", "https://raw.githubusercontent.com/user/repo/main/.claude-plugin/plugin.json"},
		{"https://raw.githubusercontent.com/user/repo/main/.claude-plugin/plugin.json", "https://raw.githubusercontent.com/user/repo/main/.claude-plugin/plugin.json"},
		{"https://gitlab.com/user/repo", ""},
		{"user/repo", ""},
		{"http://github.com/user/repo", "https://raw.githubusercontent.com/user/repo/main/.claude-plugin/plugin.json"},
	}
	for _, tt := range tests {
		got := SourceToRawURL(tt.input)
		if got != tt.want {
			t.Errorf("SourceToRawURL(%q) = %q, want %q", tt.input, got, tt.want)
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
		{"single-component", true},
		{"user/repo", false},
		{"https://github.com/x", false},
		{"org/project", false},
	}
	for _, tt := range tests {
		if got := IsLocalPath(tt.input); got != tt.want {
			t.Errorf("IsLocalPath(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestExpandPluginEnv(t *testing.T) {
	got := ExpandPluginEnv(map[string]string{"A": "${PLUGIN_ROOT}/bin", "B": "${CLAUDE_PLUGIN_ROOT}/lib"}, "/tmp/p")
	if got["A"] != "/tmp/p/bin" || got["B"] != "/tmp/p/lib" {
		t.Fatalf("expanded env = %#v", got)
	}
}

func TestExpandPluginMCPConfig(t *testing.T) {
	got := ExpandPluginMCPConfig(MCPServerConfig{
		Command: "${CLAUDE_PLUGIN_ROOT}/mcp/neko-devkit-mcp",
		Args:    []string{"--root", "${PLUGIN_ROOT}"},
		Env:     map[string]string{"PATH": "${PLUGIN_ROOT}/bin"},
	}, "/tmp/plugin")

	if got.Command != "/tmp/plugin/mcp/neko-devkit-mcp" {
		t.Fatalf("command = %q", got.Command)
	}
	if len(got.Args) != 2 || got.Args[1] != "/tmp/plugin" {
		t.Fatalf("args = %#v", got.Args)
	}
	if got.Env["PATH"] != "/tmp/plugin/bin" {
		t.Fatalf("env = %#v", got.Env)
	}
}
