package permission

import (
	"nekocode/bot/extension/tool/runtime/toolutil"
	"net/url"
	"path/filepath"
	"strings"
)

// BuildCallInfo extracts the matcher-relevant fields from a tool call's args
// into the callInfo map the engine passes to SpecifierMatcher.Match.
//
//   - bash / shell: {"command": args["command"]}
//   - file tools: {"path" or "paths", "workspace", "home"}
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
	case "image_understand":
		paths, err := toolutil.ImageUnderstandingPaths(args)
		if err != nil {
			return withTool(map[string]any{})
		}
		for i, path := range paths {
			paths[i] = LexicalPath(path, workspace, home)
		}
		return withTool(map[string]any{"paths": paths, "workspace": workspace, "home": home})
	case "read", "edit", "write", "grep", "list", "tree", "glob", "diff", "image_gen":
		targets := FileToolTargets(toolName, args)
		paths := make([]string, 0, len(targets))
		for _, target := range targets {
			paths = append(paths, LexicalPath(target.Path, workspace, home))
		}
		if toolName == "image_gen" && len(paths) == 0 {
			paths = append(paths, filepath.Join(home, ".nekocode", "images"))
		}
		if len(paths) == 1 {
			return withTool(CallInfoForPath(paths[0], workspace, home))
		}
		return withTool(map[string]any{"paths": paths, "workspace": workspace, "home": home})
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

// LexicalPath normalizes policy targets without touching the filesystem. Denied
// UNC paths must be rejected before any resolution can initiate network access.
func LexicalPath(path, workspace, home string) string {
	if path == "" {
		return ""
	}
	if path == "~" {
		path = home
	} else if strings.HasPrefix(path, "~/") {
		path = filepath.Join(home, path[2:])
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(workspace, path)
	}
	return filepath.Clean(path)
}
