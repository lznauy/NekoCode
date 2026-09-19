package command

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"nekocode/bot/config"
	"nekocode/protocol"
)

const (
	SlashPrefix  = "/"
	DollarPrefix = "$"
)

type Command struct {
	Prefix string
	Name   string
	Args   []string
	Raw    string
}

type HandlerFunc func(ctx context.Context, cmd *Command) (string, bool)
type MenuFunc func(ctx context.Context, cmd *Command) (protocol.CommandMenu, bool)

type commandKey struct {
	Prefix string
	Name   string
}

type commandEntry struct {
	DisplayName string
	Description string
	Handler     HandlerFunc
	Menu        MenuFunc
	// DuringTask marks commands that neither read nor mutate conversation
	// context and need no run lifecycle (status queries, local toggles).
	// They may execute immediately, even while a run is in progress —
	// Codex's available_during_task semantics.
	DuringTask bool
}

type Parser struct {
	mu       sync.RWMutex
	handlers map[commandKey]commandEntry
}

func NewParser() *Parser {
	return &Parser{handlers: make(map[commandKey]commandEntry)}
}

func (p *Parser) Register(name string, handler HandlerFunc) {
	p.RegisterInfo(name, "", handler)
}

// RegisterInfo registers a slash command and the short description shown by
// transport-neutral command pickers.
func (p *Parser) RegisterInfo(name, description string, handler HandlerFunc) {
	p.RegisterWithPrefix(SlashPrefix, name, description, handler)
}

// RegisterLocalInfo registers a slash command that is safe to run during a
// task: it does not touch conversation context and needs no run mutex.
// Such commands are executed immediately by frontends instead of going
// through a run.
func (p *Parser) RegisterLocalInfo(name, description string, handler HandlerFunc) {
	p.mu.Lock()
	defer p.mu.Unlock()
	key, ok := p.registerWithPrefixLocked(SlashPrefix, name, description, handler)
	if !ok {
		return
	}
	entry := p.handlers[key]
	entry.DuringTask = true
	p.handlers[key] = entry
}

// CommandAvailability reports whether input names a registered command and
// whether that command may execute while a task is in progress.
func (p *Parser) CommandAvailability(input string) (isCommand, duringTask bool) {
	cmd := p.Parse(input)
	if cmd.Name == "" {
		return false, false
	}
	entry, ok := p.lookup(commandKey{Prefix: normalizePrefix(cmd.Prefix), Name: cmd.Name})
	if !ok {
		return false, false
	}
	return true, entry.DuringTask
}

func (p *Parser) RegisterDynamic(name string, handler HandlerFunc) {
	p.RegisterDynamicInfo(name, "", handler)
}

func (p *Parser) RegisterDynamicInfo(name, description string, handler HandlerFunc) {
	p.RegisterWithPrefix(DollarPrefix, name, description, handler)
}

func (p *Parser) RegisterWithPrefix(prefix, name, description string, handler HandlerFunc) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.registerWithPrefixLocked(prefix, name, description, handler)
}

// registerWithPrefixLocked writes one command entry. Caller holds p.mu, so
// callers that also need to adjust the entry (e.g. DuringTask) can do so in
// the same critical section.
func (p *Parser) registerWithPrefixLocked(prefix, name, description string, handler HandlerFunc) (commandKey, bool) {
	prefix = normalizePrefix(prefix)
	displayName := strings.TrimSpace(name)
	if displayName == "" {
		return commandKey{}, false
	}
	key := commandKey{Prefix: prefix, Name: strings.ToLower(displayName)}
	entry := p.handlers[key]
	entry.DisplayName = displayName
	entry.Description = strings.TrimSpace(description)
	entry.Handler = handler
	p.handlers[key] = entry
	return key, true
}

// RegisterMenu adds a dynamic picker to an existing slash command. Commands
// without a finite set of choices keep their normal text behavior.
func (p *Parser) RegisterMenu(name string, menu MenuFunc) {
	p.mu.Lock()
	defer p.mu.Unlock()
	displayName := strings.TrimSpace(name)
	if displayName == "" {
		return
	}
	key := commandKey{Prefix: SlashPrefix, Name: strings.ToLower(displayName)}
	entry, exists := p.handlers[key]
	if !exists {
		return
	}
	entry.Menu = menu
	p.handlers[key] = entry
}

// ClearPrefix removes every command registered under one prefix.
func (p *Parser) ClearPrefix(prefix string) {
	p.replacePrefix(prefix, nil)
}

// replacePrefix publishes a privately prepared command set in one step.
func (p *Parser) replacePrefix(prefix string, next map[commandKey]commandEntry) {
	p.mu.Lock()
	defer p.mu.Unlock()
	prefix = normalizePrefix(prefix)
	for key := range p.handlers {
		if key.Prefix == prefix {
			delete(p.handlers, key)
		}
	}
	for key, entry := range next {
		if key.Prefix == prefix {
			p.handlers[key] = entry
		}
	}
}

// Callbacks execute after lookup releases the lock, allowing reentrant menus
// and commands such as /help to read the registry without deadlocking.
func (p *Parser) lookup(key commandKey) (commandEntry, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	entry, ok := p.handlers[key]
	return entry, ok
}

func (p *Parser) Commands() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	names := make([]string, 0, len(p.handlers))
	for key, entry := range p.handlers {
		names = append(names, key.Prefix+entry.DisplayName)
	}
	sort.Strings(names)
	return names
}

// RootMenu returns the registered commands under one prefix. Root choices
// never auto-submit: selecting one either expands its menu or fills the input
// for an explicit second confirmation.
func (p *Parser) RootMenu(prefix string) protocol.CommandMenu {
	p.mu.RLock()
	defer p.mu.RUnlock()
	prefix = normalizePrefix(prefix)
	items := make([]protocol.CommandMenuItem, 0, len(p.handlers))
	for key, entry := range p.handlers {
		if key.Prefix != prefix {
			continue
		}
		items = append(items, protocol.CommandMenuItem{
			Value:       prefix + entry.DisplayName,
			Label:       prefix + entry.DisplayName,
			Description: entry.Description,
			Submit:      false,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Value < items[j].Value })
	title := "Commands"
	if prefix == DollarPrefix {
		title = "Skills"
	}
	return protocol.CommandMenu{Title: title, Empty: "No commands available", Items: items}
}

func (p *Parser) Parse(input string) *Command {
	trimmed := strings.TrimSpace(input)
	prefix := commandPrefix(trimmed)
	if prefix == "" {
		return &Command{Name: "", Raw: input}
	}
	parts := strings.Fields(trimmed)
	if len(parts) == 0 {
		return &Command{Name: "", Raw: input}
	}
	name := strings.ToLower(strings.TrimPrefix(parts[0], prefix))
	args := []string{}
	if len(parts) > 1 {
		args = parts[1:]
	}
	return &Command{Prefix: prefix, Name: name, Args: args, Raw: input}
}

func (p *Parser) Execute(ctx context.Context, cmd *Command) (string, bool) {
	if cmd.Name == "" {
		return "", false
	}
	prefix := normalizePrefix(cmd.Prefix)
	name := strings.ToLower(strings.TrimSpace(cmd.Name))
	entry, exists := p.lookup(commandKey{Prefix: prefix, Name: name})
	if !exists {
		return "Unknown command: " + prefix + name + ". Type /help for available commands.", true
	}
	if err := ctx.Err(); err != nil {
		return "Command cancelled: " + err.Error(), true
	}
	return entry.Handler(ctx, cmd)
}

// Menu resolves the next finite set of choices for the current command input.
func (p *Parser) Menu(ctx context.Context, input string) (protocol.CommandMenu, bool) {
	trimmed := strings.TrimSpace(input)
	if trimmed == SlashPrefix || trimmed == DollarPrefix {
		return p.RootMenu(trimmed), true
	}
	cmd := p.Parse(input)
	if cmd.Name == "" {
		return protocol.CommandMenu{}, false
	}
	entry, exists := p.lookup(commandKey{Prefix: normalizePrefix(cmd.Prefix), Name: cmd.Name})
	if !exists || entry.Menu == nil || ctx.Err() != nil {
		return protocol.CommandMenu{}, false
	}
	return entry.Menu(ctx, cmd)
}

func normalizePrefix(prefix string) string {
	if prefix == DollarPrefix {
		return DollarPrefix
	}
	return SlashPrefix
}

func commandPrefix(input string) string {
	if strings.HasPrefix(input, SlashPrefix) {
		return SlashPrefix
	}
	if strings.HasPrefix(input, DollarPrefix) {
		return DollarPrefix
	}
	return ""
}

// RegisterDefaults registers the built-in slash commands using Deps directly.
func RegisterDefaults(p *Parser, deps Deps) {
	getConfig := func() string {
		selection := deps.GetConfigFn()
		return selection.Provider + "/" + selection.Model
	}

	p.RegisterLocalInfo("help", "Show available commands", func(_ context.Context, cmd *Command) (string, bool) {
		return formatCommandHelp(p.RootMenu(SlashPrefix), p.RootMenu(DollarPrefix)), true
	})

	p.RegisterLocalInfo("context", "Show context usage", func(_ context.Context, cmd *Command) (string, bool) {
		return ContextReport(deps.CtxMgr, deps.ToolRegistry.Descriptors()), true
	})

	p.RegisterInfo("compact", "Compact context now", func(ctx context.Context, cmd *Command) (string, bool) {
		result, err := ForceCompact(ctx, deps.CtxMgr, true)
		if err != nil {
			return "Compaction failed: " + err.Error(), true
		}
		return result, true
	})

	p.RegisterInfo("new", "Start a new conversation", func(_ context.Context, cmd *Command) (string, bool) {
		if deps.ResetConversation == nil {
			return "Conversation reset is unavailable.", true
		}
		result, err := deps.ResetConversation()
		if err != nil {
			return "Failed to start new conversation: " + err.Error(), true
		}
		return result, true
	})

	p.RegisterInfo("rewind", "Restore files to a user message", func(_ context.Context, cmd *Command) (string, bool) {
		if deps.Rewind == nil {
			return "Checkpoint rewind is unavailable.", true
		}
		if len(cmd.Args) > 1 {
			return "Usage: /rewind [checkpoint]", true
		}
		turn := ""
		if len(cmd.Args) == 1 {
			turn = cmd.Args[0]
		}
		result, err := deps.Rewind(turn)
		if err != nil {
			return "Rewind failed: " + err.Error(), true
		}
		return result, true
	})

	p.RegisterInfo("model", "Choose the active model", func(_ context.Context, cmd *Command) (string, bool) {
		if len(cmd.Args) == 0 {
			var sb strings.Builder
			fmt.Fprintf(&sb, "Current: %s\n", getConfig())
			if deps.ListModelsFn != nil {
				names := deps.ListModelsFn()
				sb.WriteString("Available:\n")
				for _, n := range names {
					fmt.Fprintf(&sb, "  %s\n", n)
				}
			}
			sb.WriteString("\n/model <name> to switch")
			return sb.String(), true
		}
		name := strings.Join(cmd.Args, " ")
		if name == deps.GetConfigFn().Name {
			return "Already using model '" + name + "'. Nothing to switch.", true
		}
		if err := deps.SwitchModel(name); err != nil {
			return err.Error(), true
		}
		return "Switched to " + getConfig(), true
	})

	p.RegisterInfo("effort", "Choose the reasoning effort", func(_ context.Context, cmd *Command) (string, bool) {
		model := deps.GetConfigFn()
		values := config.ReasoningCapabilityFor(model).Values()
		usage := "/effort [" + strings.Join(values, "|") + "]"
		current := model.ReasoningEffort
		if len(cmd.Args) == 0 {
			return "Effort: " + displayReasoningEffort(current), true
		}
		if len(cmd.Args) != 1 {
			return "Usage: " + usage, true
		}
		effort, ok := config.ParseReasoningEffort(cmd.Args[0])
		if !ok || !config.ReasoningCapabilityFor(model).Supports(effort) {
			return "Usage: " + usage, true
		}
		if deps.SetReasoningEffort == nil {
			return "Reasoning effort switching is unavailable.", true
		}
		if err := deps.SetReasoningEffort(effort); err != nil {
			return "Failed to switch reasoning effort: " + err.Error(), true
		}
		return "Effort: " + displayReasoningEffort(effort), true
	})

	// /permission: show or switch the permission mode. "manual" (the default)
	// prompts for approval on guarded calls; "full" is the full-takeover mode
	// that runs everything without approval. Local: it only flips an atomic
	// switch, so it may run while a task is in progress.
	p.RegisterLocalInfo("permission", "Show or switch the permission mode", func(_ context.Context, cmd *Command) (string, bool) {
		if deps.GetFullAccess == nil || deps.SetFullAccess == nil {
			return "Permission mode is unavailable.", true
		}
		if len(cmd.Args) == 0 {
			return permissionModeStatus(deps.GetFullAccess()), true
		}
		switch strings.ToLower(strings.Join(cmd.Args, " ")) {
		case "manual":
			deps.SetFullAccess(false)
			return "已切回手动审批模式。", true
		case "full":
			deps.SetFullAccess(true)
			return fullAccessWarning(), true
		default:
			return "Usage: /permission [manual|full]", true
		}
	})
}

func displayReasoningEffort(effort string) string {
	if strings.TrimSpace(effort) == "" {
		return "Auto"
	}
	return strings.ToLower(strings.TrimSpace(effort))
}

func permissionModeStatus(full bool) string {
	if full {
		return "Permission: FULL（全接管）· /permission manual 恢复审批"
	}
	return "Permission: manual · /permission [manual|full] 切换"
}

// fullAccessWarning is the one-line risk note shown on entering full-takeover
// mode. The persistent reminder lives in the status bar (Perm: FULL), so the
// command output stays short.
func fullAccessWarning() string {
	return "⚠ 全接管已开启：所有命令免审批直接执行（deny 规则仍生效），重启自动恢复。/permission manual 关闭。"
}

func formatCommandHelp(menus ...protocol.CommandMenu) string {
	var b strings.Builder
	for _, menu := range menus {
		if len(menu.Items) == 0 {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(menu.Title)
		b.WriteString(":\n")
		for _, item := range menu.Items {
			fmt.Fprintf(&b, "  %-14s", item.Label)
			if item.Description != "" {
				b.WriteString(item.Description)
			}
			b.WriteByte('\n')
		}
	}
	return strings.TrimSpace(b.String())
}
