package runner

import (
	"path/filepath"
	"strings"
)

// pathRememberSpec derives a path specifier to remember for file tools.
// Workspace-relative paths stay relative ("/src/foo.go" → "/src/**" is too
// broad; instead remember the file's parent dir: "/src/**"). Home paths get
// "~/" prefix; absolute paths get "//".
func pathRememberSpec(p, workspace, home string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if !filepath.IsAbs(p) && workspace != "" {
		p = filepath.Join(workspace, p)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	rel := ""
	if workspace != "" {
		if r, err := filepath.Rel(workspace, abs); err == nil && !strings.HasPrefix(r, "..") {
			rel = "/" + filepath.ToSlash(r)
		}
	}
	if rel != "" {
		// Remember the parent directory as a writable tree. A file in the
		// workspace root stays exact; broadening it to "/" would allow the
		// entire workspace.
		dir := filepath.ToSlash(filepath.Dir(rel))
		if dir == "/." || dir == "/" {
			return rel
		}
		return dir + "/**"
	}
	if home != "" && strings.HasPrefix(abs, home+"/") {
		return "~/" + filepath.ToSlash(strings.TrimPrefix(abs, home+"/"))
	}
	return "//" + filepath.ToSlash(abs)
}
