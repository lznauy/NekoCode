package extension

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"nekocode/bot/extension/plugin"
)

type pluginCheckFiles struct {
	skills, agents []string
	hooks, mcp     string
}

// Unlike runtime's permissive discovery, diagnostics must surface IO failures.
// One bounded walk finds all component roots, without following symlink dirs.
func discoverPluginCheckFiles(ctx context.Context, root *os.Root, p *plugin.Plugin) (pluginCheckFiles, error) {
	var found pluginCheckFiles
	var skillRoots, agentRoots []string
	visits := 0
	inside := func(path string, roots []string) bool {
		for _, r := range roots {
			if r == "." && path != "." || strings.HasPrefix(path, r+"/") {
				return true
			}
		}
		return false
	}
	err := fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		visits++
		if visits > 8192 {
			return fmt.Errorf("plugin discovery exceeds 8192 entries")
		}
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if entry.IsDir() {
			name := entry.Name()
			if path == "." {
				name = filepath.Base(p.Dir)
			}
			if strings.EqualFold(name, "skills") && !inside(path, skillRoots) {
				skillRoots = append(skillRoots, path)
			}
			if strings.EqualFold(name, "agents") && !inside(path, agentRoots) {
				agentRoots = append(agentRoots, path)
			}
			return nil
		}
		if found.hooks == "" && strings.EqualFold(entry.Name(), "hooks.json") {
			found.hooks = filepath.FromSlash(path)
		}
		if found.mcp == "" && strings.EqualFold(entry.Name(), ".mcp.json") {
			found.mcp = filepath.FromSlash(path)
		}
		if strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			for _, dir := range agentRoots {
				if filepath.ToSlash(filepath.Dir(path)) == dir {
					found.agents = append(found.agents, filepath.Join(p.Dir, filepath.FromSlash(path)))
					break
				}
			}
		}
		return nil
	})
	resolve := func(path string) string {
		if filepath.IsAbs(path) {
			return path
		}
		return filepath.Join(p.Dir, path)
	}
	if len(p.Skills) > 0 {
		for _, path := range p.Skills {
			found.skills = append(found.skills, resolve(path))
		}
	} else {
		for _, path := range skillRoots {
			found.skills = append(found.skills, resolve(filepath.FromSlash(path)))
		}
	}
	if len(p.Agents) > 0 {
		found.agents = nil
		for _, path := range p.Agents {
			found.agents = append(found.agents, resolve(path))
		}
	}
	if p.Hooks != nil && p.Hooks.Source != "" {
		found.hooks = p.Hooks.Source
	}
	return found, err
}
