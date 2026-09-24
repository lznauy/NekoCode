package core

import (
	"fmt"
	"reflect"

	"nekocode/bot/command"
	"nekocode/bot/config"
	"nekocode/bot/contextmgr"
	"nekocode/bot/contextmgr/memory"
	"nekocode/bot/prompt"
	providertypes "nekocode/bot/provider/types"
	"nekocode/protocol"
)

func resolvedReasoning(model config.ModelConfig) providertypes.ReasoningSettings {
	settings, _ := config.ResolveReasoning(model)
	return settings
}

func (b *Bot) environment() prompt.Environment {
	env := prompt.Environment{Cwd: b.cwd}
	if b.toolbox == nil {
		return env
	}
	for _, root := range b.toolbox.Workspace().Snapshot() {
		env.Roots = append(env.Roots, prompt.Root{Path: root.Path, Access: string(root.Access)})
	}
	env.ManagedProcesses = b.toolbox.ManagedProcessSummary()
	return env
}

type Memory struct {
	Path    string
	Content string
}

func (b *Bot) model() config.ModelConfig {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cfg.ActiveModelConfig()
}

// persistConfigLocked writes the current in-memory configuration to disk.
// Callers must hold b.mu. The config is cloned first so config.Validate's
// in-place fixes cannot mutate the shared configuration.
func (b *Bot) persistConfigLocked() error {
	return config.Save(b.cfg.Clone())
}

func (b *Bot) SwitchModel(name string) error {
	return b.switchModel(name, true)
}

// SwitchModelRuntime changes the active model without writing the user's
// persisted configuration. Interaction transports use it for session-scoped
// selections that must not leak into later processes.
func (b *Bot) SwitchModelRuntime(name string) error {
	return b.switchModel(name, false)
}

func (b *Bot) switchModel(name string, persist bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.cfg.SwitchModel(name) {
		return fmt.Errorf("model %q not found. Available: %v", name, b.cfg.AllModelNames())
	}

	b.rebuildAgentLocked()
	if persist {
		// The in-memory switch already took effect, so a save failure must not
		// be silent.
		if err := b.persistConfigLocked(); err != nil {
			return fmt.Errorf("switched to %q but failed to persist config: %w", name, err)
		}
	}
	return nil
}

// SetReasoningEffort changes and persists the active model's effort. An empty
// value restores the provider/model default ("Auto").
func (b *Bot) SetReasoningEffort(effort string) error {
	return b.setReasoningEffort(effort, true)
}

// SetReasoningEffortRuntime changes reasoning depth without persisting it.
func (b *Bot) SetReasoningEffortRuntime(effort string) error {
	return b.setReasoningEffort(effort, false)
}

func (b *Bot) setReasoningEffort(effort string, persist bool) error {
	var ok bool
	effort, ok = config.ParseReasoningEffort(effort)
	if !ok {
		return fmt.Errorf("unsupported reasoning effort %q", effort)
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	active := b.cfg.Active
	for i := range b.cfg.Models {
		if b.cfg.Models[i].Name != active {
			continue
		}
		capability := config.ReasoningCapabilityFor(b.cfg.Models[i])
		if !capability.Supports(effort) {
			return fmt.Errorf("model %q supports reasoning effort: %v", b.cfg.Models[i].Model, capability.Values())
		}
		b.cfg.Models[i].ReasoningEffort = effort
		b.rebuildAgentLocked()
		if persist {
			if err := b.persistConfigLocked(); err != nil {
				return fmt.Errorf("effort set to %q but failed to persist config: %w", effort, err)
			}
		}
		return nil
	}
	return fmt.Errorf("active model %q not found", active)
}

func (b *Bot) rebuildAgentLocked() {
	_, completion := b.ag.TokenUsage()
	b.initAgent()
	b.ag.AddCompletionTokens(completion)
}

func (b *Bot) Metrics() protocol.Metrics {
	agent := b.getAgent()
	return agent.Metrics()
}

func (b *Bot) Configuration() config.Config {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cfg.Clone()
}

func (b *Bot) ApplyConfiguration(next config.Config) error {
	next = next.Clone()
	if err := config.Validate(&next); err != nil {
		return err
	}

	b.mu.Lock()
	authorityChanged := sandboxAuthorityChanged(b.cfg, next)
	oldCompletion := 0
	if b.ag != nil {
		_, oldCompletion = b.ag.TokenUsage()
	}
	b.mu.Unlock()

	// Revoke processes launched under the old sandbox authority before the new
	// authority is persisted or installed. If termination cannot be confirmed,
	// leave the active and persisted configuration unchanged.
	if authorityChanged && b.sess != nil {
		if err := b.closeSessionRuntime(b.sess.CurrentID()); err != nil {
			return err
		}
	}
	if err := config.Save(next); err != nil {
		return err
	}

	b.mu.Lock()
	b.cfg = &next
	b.mu.Unlock()
	return b.reloadRuntime(oldCompletion)
}

func sandboxAuthorityChanged(current *config.Config, next config.Config) bool {
	return current == nil ||
		!reflect.DeepEqual(current.Permissions, next.Permissions) ||
		!reflect.DeepEqual(current.Workspaces, next.Workspaces)
}

func (b *Bot) reloadRuntime(oldCompletion int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.rebuildRuntime(); err != nil {
		return err
	}
	if b.ag != nil {
		b.ag.AddCompletionTokens(oldCompletion)
	}
	return nil
}

func (b *Bot) ContextReport() contextmgr.ContextReport {
	b.mu.Lock()
	defer b.mu.Unlock()

	report := b.ctxMgr.Report()
	descriptors := b.toolbox.Registry.Descriptors()
	report.ToolDefCount = len(descriptors)
	report.ToolDefTokens = command.EstimateToolDefTokens(descriptors)
	return report
}

func (b *Bot) Memory() Memory {
	b.mu.Lock()
	defer b.mu.Unlock()
	return Memory{Path: memory.DefaultPath(), Content: b.ctxMgr.Snapshot().Memory}
}

// ToolNames is the configured public tool catalog, including extensions.
func (b *Bot) ToolNames() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	descriptors := b.toolbox.Registry.Descriptors()
	names := make([]string, 0, len(descriptors))
	for _, d := range descriptors {
		names = append(names, d.Name)
	}
	return names
}

// Rewind restores the current session's checkpoint and persists the workspace event.
func (b *Bot) Rewind(id string) (string, error) { return b.rewindCheckpoint(id) }
