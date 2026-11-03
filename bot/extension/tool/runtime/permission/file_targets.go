package permission

import "strings"

// FileTarget identifies an argument that names a real filesystem target.
// Display labels and inline diff text are deliberately absent.
type FileTarget struct {
	Key    string
	Path   string
	Prefix string
}

func FileToolTargets(tool string, args map[string]any) []FileTarget {
	if tool == "diff" {
		var targets []FileTarget
		for _, key := range []string{"old", "new"} {
			value, _ := args[key].(string)
			if strings.HasPrefix(value, "path:") {
				targets = append(targets, FileTarget{Key: key, Path: strings.TrimSpace(strings.TrimPrefix(value, "path:")), Prefix: "path:"})
			}
		}
		return targets
	}
	key := "path"
	switch tool {
	case "image_gen":
		key = "output_dir"
	case "read", "write", "edit", "grep", "glob", "list", "tree":
	default:
		return nil
	}
	path, _ := args[key].(string)
	if path == "" && (tool == "grep" || tool == "glob") {
		path = "."
	}
	if path == "" {
		return nil
	}
	return []FileTarget{{Key: key, Path: path}}
}

// SetFileTarget copies args so policy normalization never mutates the caller.
func SetFileTarget(args map[string]any, target FileTarget, path string) map[string]any {
	next := make(map[string]any, len(args)+1)
	for key, value := range args {
		next[key] = value
	}
	next[target.Key] = target.Prefix + path
	return next
}
