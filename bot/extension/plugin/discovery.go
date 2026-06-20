package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

func walkFind(root string, matchName string, matchDir bool) []string {
	var results []string
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if matchDir && !d.IsDir() {
			return nil
		}
		if !matchDir && d.IsDir() {
			return nil
		}
		if strings.EqualFold(d.Name(), matchName) {
			results = append(results, path)
			if matchDir {
				return filepath.SkipDir
			}
			return filepath.SkipAll
		}
		return nil
	})
	return results
}

func (p *Plugin) autoDiscoverSkills() []string {
	return walkFind(p.Dir, "skills", true)
}

func (p *Plugin) autoDiscoverAgents() []string {
	var paths []string
	dirs := walkFind(p.Dir, "agents", true)
	for _, dir := range dirs {
		ents, _ := os.ReadDir(dir)
		for _, e := range ents {
			if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
				paths = append(paths, filepath.Join(dir, e.Name()))
			}
		}
	}
	return paths
}

func (p *Plugin) autoDiscoverHooks() (string, bool) {
	found := walkFind(p.Dir, "hooks.json", false)
	if len(found) > 0 {
		rel, _ := filepath.Rel(p.Dir, found[0])
		return rel, true
	}
	return "", false
}

func (p *Plugin) autoDiscoverMCP() map[string]MCPServerConfig {
	found := walkFind(p.Dir, ".mcp.json", false)
	for _, path := range found {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var cfg struct {
			MCPServers map[string]MCPServerConfig `json:"mcpServers"`
		}
		if json.Unmarshal(data, &cfg) == nil && len(cfg.MCPServers) > 0 {
			return cfg.MCPServers
		}
	}
	return nil
}

func resolvePath(base, rel string) string {
	rel = strings.TrimPrefix(rel, "./")
	if filepath.IsAbs(rel) {
		return rel
	}
	return filepath.Join(base, rel)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
