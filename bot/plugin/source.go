package plugin

import (
	"fmt"
	"strings"

	"nekocode/common"
)

func Usage() string {
	return "Usage: /plugin <subcommand> [args]\n\nSubcommands:\n  install <source>   Install from GitHub URL, user/repo, or local path\n  uninstall <name>   Remove a plugin\n  list               List installed plugins\n  enable <name>      Enable a disabled plugin\n  disable <name>     Disable a plugin (keeps files)\n  info <name>        Show plugin details"
}

func SourceToRawURL(source string) string {
	s := source
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	if !strings.HasPrefix(s, "github.com/") && !strings.HasPrefix(s, "raw.githubusercontent.com/") {
		return ""
	}
	if strings.HasPrefix(s, "github.com/") {
		clean := strings.TrimSuffix(strings.TrimSuffix(s, ".git"), "/")
		parts := strings.SplitN(clean, "/", 6)
		if len(parts) < 3 {
			return ""
		}
		branch := "main"
		if len(parts) >= 5 && parts[3] == "tree" {
			branch = parts[4]
		}
		return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/.claude-plugin/plugin.json", parts[1], parts[2], branch)
	}
	if !strings.Contains(s, ".claude-plugin") {
		s = strings.TrimSuffix(s, "/") + "/.claude-plugin/plugin.json"
	}
	return "https://" + s
}

func IsLocalPath(s string) bool {
	return strings.HasPrefix(s, "./") || strings.HasPrefix(s, "/") || strings.HasPrefix(s, "~") ||
		(!strings.Contains(s, "://") && !common.LooksLikeGit(s))
}

func ExpandPluginEnv(env map[string]string, pluginRoot string) map[string]string {
	if env == nil {
		return nil
	}
	out := make(map[string]string, len(env))
	for k, v := range env {
		s := strings.ReplaceAll(v, "${CLAUDE_PLUGIN_ROOT}", pluginRoot)
		s = strings.ReplaceAll(s, "${PLUGIN_ROOT}", pluginRoot)
		out[k] = s
	}
	return out
}
