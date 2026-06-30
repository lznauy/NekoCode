package command

import (
	"fmt"
	"strings"
)

type Command struct {
	Name string
	Args []string
	Raw  string
}

type Handler func(cmd *Command) (string, bool)

type Parser struct {
	handlers map[string]Handler
}

func NewParser() *Parser {
	return &Parser{handlers: make(map[string]Handler)}
}

func (p *Parser) Register(name string, handler Handler) {
	p.handlers[name] = handler
}

func (p *Parser) Commands() []string {
	names := make([]string, 0, len(p.handlers))
	for name := range p.handlers {
		names = append(names, name)
	}
	return names
}

func (p *Parser) Parse(input string) *Command {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "/") {
		return &Command{Name: "", Raw: input}
	}
	parts := strings.Fields(trimmed)
	if len(parts) == 0 {
		return &Command{Name: "", Raw: input}
	}
	name := strings.ToLower(strings.TrimPrefix(parts[0], "/"))
	args := []string{}
	if len(parts) > 1 {
		args = parts[1:]
	}
	return &Command{Name: name, Args: args, Raw: input}
}

func (p *Parser) Execute(cmd *Command) (string, bool) {
	if cmd.Name == "" {
		return "", false
	}
	handler, exists := p.handlers[cmd.Name]
	if !exists {
		return "Unknown command: /" + cmd.Name + ". Type /help for available commands.", true
	}
	return handler(cmd)
}

// RegisterDefaults registers the built-in slash commands using Deps directly.
func RegisterDefaults(p *Parser, deps Deps) {
	getConfig := func() string { pr, m := deps.GetConfigFn(); return pr + "/" + m }

	p.Register("help", func(cmd *Command) (string, bool) {
		return `Available commands:
  /help        Show this help message
  /new         Start a new conversation (keeps summary)
  /clear       Clear all conversation history
  /stats       Show context stats (messages, tokens, summary)
  /summarize   Force context compression now
  /context     Show detailed context window breakdown
  /config      Show current provider and model
  /model       List or switch models (/model <name>)
  /plan        Read-only exploration mode, approve before execution
  /plugin      Manage plugins (install, list, uninstall, etc.)
  /sessions    Manage saved sessions
  /export      Export conversation context to JSON file
`, true
	})

	p.Register("clear", func(cmd *Command) (string, bool) {
		deps.CtxMgr.Clear()
		return "Conversation history cleared.", true
	})

	p.Register("stats", func(cmd *Command) (string, bool) {
		return ContextStats(deps.CtxMgr), true
	})

	p.Register("summarize", func(cmd *Command) (string, bool) {
		result, err := ForceSummarize(deps.CtxMgr)
		if err != nil {
			return "Summarize failed: " + err.Error(), true
		}
		return result, true
	})

	p.Register("new", func(cmd *Command) (string, bool) {
		result, err := deps.FreshStart()
		if err != nil {
			return "Failed to start new conversation: " + err.Error(), true
		}
		return result, true
	})

	p.Register("context", func(cmd *Command) (string, bool) {
		return ContextReport(deps.CtxMgr, deps.ToolRegistry.Descriptors()), true
	})

	p.Register("config", func(cmd *Command) (string, bool) {
		return getConfig(), true
	})

	p.Register("model", func(cmd *Command) (string, bool) {
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
		model, provider, err := deps.SwitchModel(cmd.Args[0])
		if err != nil {
			return err.Error(), true
		}
		return fmt.Sprintf("Switched to %s/%s", provider, model), true
	})
}
