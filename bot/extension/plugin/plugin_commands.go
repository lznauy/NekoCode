package plugin

import (
	"context"
	"fmt"
)

// Usage returns the /plugin command usage text.
func Usage() string {
	return "Usage: /plugin <subcommand> [args]\n\nSubcommands:\n  install <source> [--yes]   Install from GitHub URL, user/repo, or local path\n  uninstall <name>           Remove a plugin\n  list                       List installed plugins\n  enable <name>              Enable a disabled plugin\n  disable <name>             Disable a plugin (keeps files)\n  info <name>                Show plugin details\n  check [name]               Check static compatibility without executing plugins\n\n--yes skips the install preview and confirmation."
}

const InstallUsage = "Usage: /plugin install <source> [--yes]\n  source: GitHub URL | user/repo | ./local-path\n  --yes: Skip the install preview and confirmation"

// ListText renders the installed plugin list for command-line callers.
func (m *Manager) ListText() string {
	return formatList(m.reg.List())
}

// InfoText renders one installed plugin.
func (m *Manager) InfoText(name string) (string, bool) {
	p, ok := m.reg.Get(name)
	if !ok {
		return "", false
	}
	return formatInfo(p), true
}

// SetEnabled persists a plugin's enabled state without activating runtime
// extensions.
func (m *Manager) SetEnabled(name string, enable bool) (*Plugin, error) {
	p, ok := m.reg.Get(name)
	if !ok {
		return nil, fmt.Errorf("plugin not found: %s", name)
	}
	if enable {
		if err := m.reg.Enable(p.Name); err != nil {
			return nil, err
		}
		if next, ok := m.reg.Get(p.Name); ok {
			p = next
		}
	} else {
		if err := m.reg.Disable(p.Name); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// Preview resolves a local or remote plugin manifest without installing it.
func (m *Manager) Preview(ctx context.Context, source string) (p *Plugin, remote bool, err error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if err := validateSource(source); err != nil {
		return nil, false, err
	}
	if isLocalPath(source) {
		p, err = m.reg.PreviewFromPath(source)
		return p, false, err
	}
	p, err = fetchRemotePreview(ctx, source, fetchURL)
	return p, true, err
}

// PrepareInstall stages a plugin replacement for the parent extension
// manager to activate before commit.
func (m *Manager) PrepareInstall(ctx context.Context, source string) (*Installation, error) {
	return m.reg.PrepareInstall(ctx, source)
}

// Remove deletes a plugin and returns its pre-removal snapshot.
func (m *Manager) Remove(name string) (*Plugin, error) {
	p, ok := m.reg.Get(name)
	if !ok {
		return nil, fmt.Errorf("plugin %q not found", name)
	}
	if err := m.reg.Uninstall(name); err != nil {
		return nil, err
	}
	return p, nil
}

// fetchRemotePreview fetches a remote plugin's manifest for the install
// confirmation preview, without installing anything.
func fetchRemotePreview(ctx context.Context, source string, fetch func(context.Context, string) ([]byte, error)) (*Plugin, error) {
	rawURL := sourceToRawURL(source)
	if rawURL == "" {
		return nil, fmt.Errorf("fetch plugin info: preview URL not available for %s", source)
	}
	data, err := fetch(ctx, rawURL)
	if err != nil {
		return nil, fmt.Errorf("fetch plugin info: %w", err)
	}
	m, err := parseManifestData(data)
	if err != nil {
		return nil, fmt.Errorf("invalid plugin.json: %w", err)
	}
	return &Plugin{Manifest: *m, Dir: "", Source: source}, nil
}

// ConfirmSummary renders the install confirmation preview; remote installs
// carry an extra note that install.sh is never executed automatically.
func ConfirmSummary(p *Plugin, isRemote bool) string {
	summary := formatInstallPreview(p)
	if isRemote {
		summary += "\n(install.sh will not be executed automatically)"
	}
	return summary
}

// InstallResult renders the result of a completed install.
func InstallResult(p *Plugin) string {
	return formatInstallResult(p)
}

// IsLocalSource reports whether source should be read from the local file
// system rather than fetched as a repository.
func IsLocalSource(source string) bool {
	return isLocalPath(source)
}
