package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nekocode/common"
)

// PreviewFromPath creates a Plugin from a local path without installing.
func (r *Registry) PreviewFromPath(source string) (*Plugin, error) {
	abs, err := filepath.Abs(source)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}
	if !HasManifest(abs) {
		return nil, fmt.Errorf("no .claude-plugin/plugin.json found in %s", abs)
	}
	m, err := ParseManifest(abs)
	if err != nil {
		return nil, err
	}
	return newPluginFromManifest(m, abs, source), nil
}

// Install clones or copies a plugin from source into user plugin dir.
func (r *Registry) Install(source string) (*Plugin, error) {
	userDir, err := userPluginDir()
	if err != nil {
		return nil, fmt.Errorf("user plugin dir: %w", err)
	}
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		return nil, fmt.Errorf("create plugin dir: %w", err)
	}

	pluginDir, err := r.installToUserDir(userDir, source)
	if err != nil {
		return nil, err
	}

	m, err := ParseManifest(pluginDir)
	if err != nil {
		return nil, fmt.Errorf("parse installed manifest: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	p := newPluginFromManifest(m, pluginDir, source)
	r.plugins[m.Name] = p
	r.saveRegistryFile()
	return p, nil
}

func (r *Registry) installToUserDir(userDir, source string) (string, error) {
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") ||
		strings.Contains(source, "github.com") || strings.Contains(source, "gitlab.com") {
		pluginDir := filepath.Join(userDir, repoName(source))
		if err := r.gitClone(source, pluginDir); err != nil {
			return "", err
		}
		return pluginDir, nil
	}
	if common.LooksLikeGit(source) {
		url := "https://github.com/" + source
		pluginDir := filepath.Join(userDir, strings.ReplaceAll(source, "/", "-"))
		if err := r.gitClone(url, pluginDir); err != nil {
			return "", err
		}
		return pluginDir, nil
	}

	abs, err := filepath.Abs(source)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	if !HasManifest(abs) {
		return "", fmt.Errorf("no .claude-plugin/plugin.json found in %s", abs)
	}
	m, err := ParseManifest(abs)
	if err != nil {
		return "", fmt.Errorf("parse manifest: %w", err)
	}
	pluginDir := filepath.Join(userDir, m.Name)
	if err := copyDir(abs, pluginDir); err != nil {
		return "", fmt.Errorf("copy plugin: %w", err)
	}
	return pluginDir, nil
}

func (r *Registry) gitClone(url, dest string) error {
	if _, err := os.Stat(dest); err == nil {
		return runGit(dest, "pull", "--ff-only")
	}
	return runGit("", "clone", "--depth", "1", url, dest)
}

func repoName(url string) string {
	s := strings.TrimSuffix(url, ".git")
	s = strings.TrimSuffix(s, "/")
	parts := strings.Split(s, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "-" + parts[len(parts)-1]
	}
	return s
}
