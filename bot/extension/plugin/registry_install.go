package plugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	utilfs "nekocode/util/fs"
	"nekocode/util/text"
)

// PreviewFromPath creates a Plugin from a local path without installing.
func (r *registry) PreviewFromPath(source string) (*Plugin, error) {
	abs, err := filepath.Abs(source)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}
	if !hasManifest(abs) {
		return nil, fmt.Errorf("no .claude-plugin/plugin.json found in %s", abs)
	}
	m, err := parseManifest(abs)
	if err != nil {
		return nil, err
	}
	return newPluginFromManifest(m, abs, source), nil
}

// Installation holds a prepared filesystem replacement until extension
// activation either commits or rolls it back.
type Installation struct {
	registry    *registry
	plugin      *Plugin
	previous    *Plugin
	hadRegistry bool
	backupDir   string
	hadPrevious bool
	once        sync.Once
	finishErr   error
}

func (i *Installation) Plugin() *Plugin {
	if i == nil {
		return nil
	}
	return i.plugin
}

func (i *Installation) Commit() error {
	if i == nil {
		return nil
	}
	i.once.Do(func() {
		i.registry.mu.Lock()
		i.registry.plugins[i.plugin.Name] = i.plugin
		err := i.registry.saveRegistryFile()
		if err != nil {
			if i.hadRegistry {
				i.registry.plugins[i.plugin.Name] = i.previous
			} else {
				delete(i.registry.plugins, i.plugin.Name)
			}
		}
		i.registry.mu.Unlock()
		if err != nil {
			i.finishErr = i.rollbackFiles()
			if i.finishErr == nil {
				i.finishErr = err
			} else {
				i.finishErr = fmt.Errorf("%w (restore files: %v)", err, i.finishErr)
			}
		} else if i.hadPrevious {
			_ = os.RemoveAll(i.backupDir)
		}
		i.registry.installMu.Unlock()
	})
	return i.finishErr
}

func (i *Installation) Rollback() error {
	if i == nil {
		return nil
	}
	i.once.Do(func() {
		i.finishErr = i.rollbackFiles()
		i.registry.installMu.Unlock()
	})
	return i.finishErr
}

func (i *Installation) rollbackFiles() error {
	if err := os.RemoveAll(i.plugin.Dir); err != nil {
		return err
	}
	if i.hadPrevious {
		return os.Rename(i.backupDir, i.plugin.Dir)
	}
	return nil
}

// Install clones or copies a plugin and commits it immediately.
func (r *registry) Install(ctx context.Context, source string) (*Plugin, error) {
	installation, err := r.PrepareInstall(ctx, source)
	if err != nil {
		return nil, err
	}
	if err := installation.Commit(); err != nil {
		return nil, err
	}
	return installation.Plugin(), nil
}

// PrepareInstall stages and swaps plugin files while retaining the previous
// version for rollback.
func (r *registry) PrepareInstall(ctx context.Context, source string) (*Installation, error) {
	if err := validateSource(source); err != nil {
		return nil, err
	}
	r.installMu.Lock()
	installation, err := r.prepareInstall(ctx, source)
	if err != nil {
		r.installMu.Unlock()
		return nil, err
	}
	return installation, nil
}

func (r *registry) prepareInstall(ctx context.Context, source string) (*Installation, error) {
	userDir, err := userPluginDir()
	if err != nil {
		return nil, fmt.Errorf("user plugin dir: %w", err)
	}
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		return nil, fmt.Errorf("create plugin dir: %w", err)
	}

	stagingDir, err := os.MkdirTemp(userDir, ".install-")
	if err != nil {
		return nil, fmt.Errorf("create plugin staging dir: %w", err)
	}
	stagingOwned := true
	defer func() {
		if stagingOwned {
			_ = os.RemoveAll(stagingDir)
		}
	}()

	if err := r.stageSource(ctx, source, stagingDir); err != nil {
		return nil, err
	}
	m, err := parseManifest(stagingDir)
	if err != nil {
		return nil, fmt.Errorf("parse installed manifest: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	pluginDir := filepath.Join(userDir, m.Name)
	backupDir := stagingDir + "-backup"
	hadPrevious := false
	if _, err := os.Stat(pluginDir); err == nil {
		if err := os.Rename(pluginDir, backupDir); err != nil {
			return nil, fmt.Errorf("backup installed plugin: %w", err)
		}
		hadPrevious = true
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect installed plugin: %w", err)
	}
	if err := os.Rename(stagingDir, pluginDir); err != nil {
		if hadPrevious {
			_ = os.Rename(backupDir, pluginDir)
		}
		return nil, fmt.Errorf("commit plugin install: %w", err)
	}
	stagingOwned = false

	p := newPluginFromManifest(m, pluginDir, sanitizeSource(source))
	r.mu.RLock()
	previous, hadRegistry := r.plugins[m.Name]
	r.mu.RUnlock()
	return &Installation{
		registry: r, plugin: p, previous: previous, hadRegistry: hadRegistry,
		backupDir: backupDir, hadPrevious: hadPrevious,
	}, nil
}

func (r *registry) stageSource(ctx context.Context, source, stagingDir string) error {
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") ||
		strings.Contains(source, "github.com") || strings.Contains(source, "gitlab.com") {
		return runGit(ctx, "", "clone", "--depth", "1", source, stagingDir)
	}
	if text.LooksLikeGit(source) {
		url := "https://github.com/" + source
		return runGit(ctx, "", "clone", "--depth", "1", url, stagingDir)
	}

	abs, err := filepath.Abs(source)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	if !hasManifest(abs) {
		return fmt.Errorf("no .claude-plugin/plugin.json found in %s", abs)
	}
	if err := copyDir(ctx, abs, stagingDir); err != nil {
		return fmt.Errorf("copy plugin: %w", err)
	}
	return nil
}

const gitTimeout = 60 * time.Second

func runGit(ctx context.Context, dir string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	return cmd.Run()
}

func copyDir(ctx context.Context, src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		if d.Type()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return copyFile(ctx, path, target)
	})
}

func copyFile(ctx context.Context, src, dst string) error {
	in, err := utilfs.OpenRegularFile(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, contextReader{ctx: ctx, reader: in})
	if copyErr == nil {
		copyErr = out.Chmod(info.Mode().Perm())
	}
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(dst)
		return errors.Join(copyErr, closeErr)
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
