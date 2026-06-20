package plugincli

import "strings"

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
