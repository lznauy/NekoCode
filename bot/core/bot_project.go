package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nekocode/bot/command"
	"nekocode/bot/config"
	"nekocode/bot/extension"
	"nekocode/bot/project"
	"nekocode/logger"
)

// reloadStart is a consistent snapshot captured under b.mu before the reload
// work runs. Copying the config and project keeps the lock-free phase
// independent from later mutations.
type reloadStart struct {
	project *project.Project
	config  config.Config
	ext     *extension.Manager
	cwd     string
}

// prepareReload captures the starting point for a project reload. It always
// releases b.mu via defer, so a panic while reading extension state cannot
// leave the lock held.
func (b *Bot) prepareReload() reloadStart {
	b.mu.Lock()
	defer b.mu.Unlock()

	var start reloadStart
	if b.project != nil {
		// Refresh replaces maps/slices rather than mutating their contents;
		// retaining the last good values on errors is safe in this copy.
		copy := *b.project
		start.project = &copy
	}
	if b.cfg != nil {
		start.config = b.cfg.Clone()
	}
	start.ext, start.cwd = b.ext, b.cwd
	if start.ext != nil {
		previous := b.extensionsLocked()
		b.reloadView = &previous
	}
	return start
}

// reloadProject prepares configuration and extensions without holding the Bot
// state lock. Readers keep the previous complete view until publication.
// Runtime still serializes these mutations with agent runs and other settings.
func (b *Bot) reloadProject() {
	b.projectReloadMu.Lock()
	defer b.projectReloadMu.Unlock()

	start := b.prepareReload()
	if start.ext != nil {
		// Views serve the previous snapshot until publication below. Clear the
		// guard even if the refresh panics, so reads never stay stuck on it.
		defer b.clearReloadView()
	}

	if start.project != nil {
		for _, err := range start.project.Refresh() {
			fmt.Fprintf(os.Stderr, "project: %v (keeping previous valid configuration)\n", err)
			logger.Log("project: %v", err)
		}
	}
	if start.ext != nil {
		start.ext.ReloadWithMCP(resolveMCPServers(&start.config, start.project, start.cwd))
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.project = start.project
	if b.promptBuilder != nil {
		if start.project != nil {
			b.promptBuilder.SetProjectInstructions(start.project.InstructionsPath(), start.project.Instructions)
		}
		if b.ctxMgr != nil {
			b.ctxMgr.SetSystemPrompt(b.promptBuilder.BuildStatic())
		}
	}
	b.reloadView = nil
}

// clearReloadView drops the stale-view guard. It is idempotent and only
// unlocks b.mu briefly, so it is safe as a deferred safety net.
func (b *Bot) clearReloadView() {
	b.mu.Lock()
	b.reloadView = nil
	b.mu.Unlock()
}

func (b *Bot) registerWorkspaceCommands(parser *command.Parser) {
	parser.RegisterInfo("workspace", "Show project configuration; /workspace reload to refresh", func(ctx context.Context, cmd *command.Command) (string, bool) {
		if err := ctx.Err(); err != nil {
			return "Workspace command cancelled: " + err.Error(), true
		}
		if len(cmd.Args) > 1 || len(cmd.Args) == 1 && cmd.Args[0] != "reload" {
			return "Usage: /workspace [reload]", true
		}
		if len(cmd.Args) == 1 {
			b.RefreshExtensions()
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		p := b.project
		if p == nil {
			return "No project configuration loaded.", true
		}
		var out strings.Builder
		fmt.Fprintf(&out, "Workspace: %s\nSkills: %s\nInstructions: %s (%d bytes loaded)\nMCP: %s (%d project definitions)\n",
			p.Root, filepath.Join(p.Root, ".nekocode", "skills"), p.InstructionsPath(), len(p.Instructions), p.MCPPath(), len(p.Servers))
		for _, diagnostic := range p.Diagnostics {
			fmt.Fprintf(&out, "Warning: %s; keeping previous valid configuration.\n", diagnostic)
		}
		return strings.TrimSpace(out.String()), true
	})
}
