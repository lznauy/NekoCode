package permission

import (
	"net/url"
	"strings"
)

// BuildCallInfo extracts the matcher-relevant fields from a tool call's args
// into the callInfo map the engine passes to SpecifierMatcher.Match.
//
//   - bash / shell: {"command": args["command"]}
//   - file tools (read/edit/write/glob/grep/list/tree): {"path", "workspace", "home"}
//   - web_fetch: {"domain": host extracted from args["url"]}
//
// Tools without a known matcher just get an empty map (the engine treats a
// scoped rule with no matcher as non-matching; a bare rule still applies).
func BuildCallInfo(toolName string, args map[string]any, workspace, home string) map[string]any {
	withTool := func(info map[string]any) map[string]any {
		info["tool"] = toolName
		return info
	}
	switch toolName {
	case "bash", "shell":
		if cmd, _ := args["command"].(string); cmd != "" {
			return withTool(CallInfoForBash(cmd))
		}
	case "read", "edit", "write", "grep", "list", "tree":
		path, _ := args["path"].(string)
		return withTool(CallInfoForPath(path, workspace, home))
	case "glob":
		path, _ := args["path"].(string)
		if path == "" {
			path, _ = args["pattern"].(string)
		}
		return withTool(CallInfoForPath(path, workspace, home))
	case "web_fetch", "webfetch", "web_extract":
		if u, _ := args["url"].(string); u != "" {
			return withTool(CallInfoForDomain(hostFromURL(u)))
		}
	}
	// Unknown tool or missing field: empty map → a scoped rule can't match
	// (no matcher), but a bare "Tool" rule still applies.
	return map[string]any{"tool": toolName}
}

// hostFromURL extracts the hostname from a URL string; returns the input
// unchanged if it is not a parseable URL.
func hostFromURL(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if !strings.Contains(s, "://") {
		// bare host or "example.com/path" → take the first segment
		if i := strings.IndexByte(s, '/'); i > 0 {
			return s[:i]
		}
		return s
	}
	u, err := url.Parse(s)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	return u.Hostname()
}
