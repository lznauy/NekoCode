package command

import (
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

type Callbacks struct {
	ClearHistory   func()
	GetConfig      func() string
	ForceSummarize func() (string, error)
	ContextStats   func() string
	ContextReport  func() string
	FreshStart     func() (string, error)
}

func RegisterDefaults(p *Parser, callbacks *Callbacks) {
	p.Register("help", func(cmd *Command) (string, bool) {
		return `Available commands:
  /help        Show this help message
  /new         Start a new conversation (keeps summary)
  /clear       Clear all conversation history
  /stats       Show context stats (messages, tokens, summary)
  /summarize   Force context compression now
  /context     Show detailed context window breakdown
  /config      Show current provider and model
  /plugin      Manage plugins (install, list, uninstall, etc.)
  /sessions    Manage saved sessions
`, true
	})

	p.Register("clear", func(cmd *Command) (string, bool) {
		if callbacks.ClearHistory != nil {
			callbacks.ClearHistory()
		}
		return "Conversation history cleared.", true
	})

	p.Register("stats", func(cmd *Command) (string, bool) {
		if callbacks.ContextStats != nil {
			return callbacks.ContextStats(), true
		}
		return "Stats unavailable", true
	})

	p.Register("summarize", func(cmd *Command) (string, bool) {
		if callbacks.ForceSummarize != nil {
			result, err := callbacks.ForceSummarize()
			if err != nil {
				return "Summarize failed: " + err.Error(), true
			}
			return result, true
		}
		return "Summarize unavailable", true
	})

	p.Register("new", func(cmd *Command) (string, bool) {
		if callbacks.FreshStart != nil {
			result, err := callbacks.FreshStart()
			if err != nil {
				return "Failed to start new conversation: " + err.Error(), true
			}
			return result, true
		}
		return "Fresh start unavailable", true
	})

	p.Register("context", func(cmd *Command) (string, bool) {
		if callbacks.ContextReport != nil {
			return callbacks.ContextReport(), true
		}
		return "Context report unavailable", true
	})

	p.Register("config", func(cmd *Command) (string, bool) {
		if callbacks.GetConfig != nil {
			return callbacks.GetConfig(), true
		}
		return "", true
	})
}
