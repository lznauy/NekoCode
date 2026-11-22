package command

import (
	"context"
	"strings"
	"testing"

	"nekocode/bot/config"
	"nekocode/protocol"
)

func TestParserParse(t *testing.T) {
	p := NewParser()

	tests := []struct {
		input      string
		wantPrefix string
		wantName   string
		wantArgs   int
	}{
		{"/help", "/", "help", 0},
		{"/plan do something", "/", "plan", 2},
		{"not a command", "", "", 0},
		{"/STATS", "/", "stats", 0},
		{"$review fix this", "$", "review", 2},
	}
	for _, tt := range tests {
		cmd := p.Parse(tt.input)
		if cmd.Prefix != tt.wantPrefix {
			t.Errorf("Parse(%q).Prefix = %q, want %q", tt.input, cmd.Prefix, tt.wantPrefix)
		}
		if cmd.Name != tt.wantName {
			t.Errorf("Parse(%q).Name = %q, want %q", tt.input, cmd.Name, tt.wantName)
		}
		if len(cmd.Args) != tt.wantArgs {
			t.Errorf("Parse(%q).Args len = %d, want %d", tt.input, len(cmd.Args), tt.wantArgs)
		}
	}
}

func TestParserMenuIsOptionalAndDynamic(t *testing.T) {
	p := NewParser()
	p.Register("model", func(context.Context, *Command) (string, bool) { return "", true })
	p.RegisterMenu("model", func(_ context.Context, cmd *Command) (protocol.CommandMenu, bool) {
		if len(cmd.Args) != 0 {
			return protocol.CommandMenu{}, false
		}
		return protocol.CommandMenu{Title: "Models", Items: []protocol.CommandMenuItem{{Value: "/model fast"}}}, true
	})

	menu, ok := p.Menu(context.Background(), "/model")
	if !ok || menu.Title != "Models" || len(menu.Items) != 1 {
		t.Fatalf("model menu = %+v, %v", menu, ok)
	}
	if _, ok := p.Menu(context.Background(), "/model fast"); ok {
		t.Fatal("model menu remained open after a model argument")
	}
	if _, ok := p.Menu(context.Background(), "/help"); ok {
		t.Fatal("help should not expose a command menu")
	}
}

func TestRootMenuNeverAutoSubmitsCommands(t *testing.T) {
	p := NewParser()
	p.Register("new", nil)
	p.Register("model", nil)
	p.RegisterMenu("model", func(context.Context, *Command) (protocol.CommandMenu, bool) {
		return protocol.CommandMenu{}, true
	})
	for _, item := range p.RootMenu(SlashPrefix).Items {
		if item.Submit {
			t.Fatalf("root item %q auto-submits", item.Value)
		}
	}
}

func TestParserExecute(t *testing.T) {
	p := NewParser()
	p.Register("test", func(_ context.Context, cmd *Command) (string, bool) { return "ok", true })
	p.RegisterDynamic("review", func(_ context.Context, cmd *Command) (string, bool) { return "dynamic", true })

	// Unknown command.
	msg, handled := p.Execute(context.Background(), &Command{Name: "unknown"})
	if !handled || msg != "Unknown command: /unknown. Type /help for available commands." {
		t.Errorf("unexpected: %q, %v", msg, handled)
	}

	// Known command.
	msg, handled = p.Execute(context.Background(), &Command{Name: "test"})
	if !handled || msg != "ok" {
		t.Errorf("unexpected: %q, %v", msg, handled)
	}

	// Dynamic command.
	msg, handled = p.Execute(context.Background(), &Command{Prefix: "$", Name: "review"})
	if !handled || msg != "dynamic" {
		t.Errorf("unexpected: %q, %v", msg, handled)
	}

	// Empty command.
	_, handled = p.Execute(context.Background(), &Command{Name: ""})
	if handled {
		t.Error("empty command should not be handled")
	}
}

func TestParserExecuteHonorsCancellation(t *testing.T) {
	p := NewParser()
	called := false
	p.Register("test", func(_ context.Context, _ *Command) (string, bool) {
		called = true
		return "ok", true
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	msg, handled := p.Execute(ctx, &Command{Name: "test"})
	if !handled || msg != "Command cancelled: context canceled" {
		t.Fatalf("unexpected cancellation result: %q, %v", msg, handled)
	}
	if called {
		t.Fatal("handler ran after command context was cancelled")
	}
}

func TestParserCommands(t *testing.T) {
	p := NewParser()
	p.Register("a", nil)
	p.RegisterDynamic("b", nil)
	names := p.Commands()
	want := []string{"$b", "/a"}
	if len(names) != len(want) {
		t.Fatalf("expected %d commands, got %d: %v", len(want), len(names), names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("Commands()[%d] = %q, want %q (all: %v)", i, names[i], want[i], names)
		}
	}
}

func TestModelCommandRefusesReselectingActiveModel(t *testing.T) {
	switched := ""
	p := New(Deps{
		GetConfigFn: func() config.ModelConfig {
			return config.ModelConfig{Name: "flash", Provider: "glm", Model: "glm-5.3-flash"}
		},
		ListModelsFn: func() []string { return []string{"flash", "pro"} },
		SwitchModel: func(name string) error {
			switched = name
			return nil
		},
	})

	out, handled := p.Execute(protocol.WithCommandStatusDisplay(context.Background()), "/model flash", nil)
	if !handled || !strings.Contains(out, "Already using model 'flash'") {
		t.Fatalf("reselect active model: output=%q handled=%v", out, handled)
	}
	if switched != "" {
		t.Fatal("SwitchModel should not run when re-selecting the active model")
	}

	out, handled = p.Execute(protocol.WithCommandStatusDisplay(context.Background()), "/model pro", nil)
	if !handled || switched != "pro" || out != "" {
		t.Fatalf("switch model: output=%q handled=%v switched=%q", out, handled, switched)
	}
}

func TestReasoningEffortCommand(t *testing.T) {
	current := ""
	p := New(Deps{
		GetConfigFn: func() config.ModelConfig {
			return config.ModelConfig{Provider: "deepseek", Model: "deepseek-v4-flash", ReasoningEffort: current}
		},
		SetReasoningEffort: func(effort string) error {
			current = effort
			return nil
		},
	})

	out, handled := p.Execute(protocol.WithCommandStatusDisplay(context.Background()), "/effort high", nil)
	if !handled || out != "" || current != "high" {
		t.Fatalf("set effort: output=%q handled=%v current=%q", out, handled, current)
	}
	out, handled = p.Execute(protocol.WithCommandStatusDisplay(context.Background()), "/effort auto", nil)
	if !handled || out != "" || current != "" {
		t.Fatalf("reset effort: output=%q handled=%v current=%q", out, handled, current)
	}
	out, handled = p.Execute(protocol.WithCommandStatusDisplay(context.Background()), "/effort", nil)
	if !handled || out != "Effort: Auto" {
		t.Fatalf("query effort: output=%q handled=%v", out, handled)
	}
	out, handled = p.Execute(protocol.WithCommandStatusDisplay(context.Background()), "/effort extreme", nil)
	if !handled || !strings.HasPrefix(out, "Usage: /effort") || current != "" {
		t.Fatalf("invalid effort: output=%q handled=%v current=%q", out, handled, current)
	}
}

func TestAbsolutePathFallsThroughCommandRouting(t *testing.T) {
	p := NewParser()
	for _, input := range []string{"/tmp/example.py", "/tmp/path with spaces/file"} {
		if _, handled := p.Execute(context.Background(), p.Parse(input)); handled {
			t.Errorf("path handled as command: %q", input)
		}
	}
}
