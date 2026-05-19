package command

import (
	"testing"
)

func TestParserParse(t *testing.T) {
	p := NewParser()

	tests := []struct {
		input    string
		wantName string
		wantArgs int
	}{
		{"/help", "help", 0},
		{"/plan do something", "plan", 2},
		{"not a command", "", 0},
		{"/STATS", "stats", 0},
	}
	for _, tt := range tests {
		cmd := p.Parse(tt.input)
		if cmd.Name != tt.wantName {
			t.Errorf("Parse(%q).Name = %q, want %q", tt.input, cmd.Name, tt.wantName)
		}
		if len(cmd.Args) != tt.wantArgs {
			t.Errorf("Parse(%q).Args len = %d, want %d", tt.input, len(cmd.Args), tt.wantArgs)
		}
	}
}

func TestParserExecute(t *testing.T) {
	p := NewParser()
	p.Register("test", func(cmd *Command) (string, bool) { return "ok", true })

	// Unknown command.
	msg, handled := p.Execute(&Command{Name: "unknown"})
	if !handled || msg != "Unknown command: /unknown. Type /help for available commands." {
		t.Errorf("unexpected: %q, %v", msg, handled)
	}

	// Known command.
	msg, handled = p.Execute(&Command{Name: "test"})
	if !handled || msg != "ok" {
		t.Errorf("unexpected: %q, %v", msg, handled)
	}

	// Empty command.
	_, handled = p.Execute(&Command{Name: ""})
	if handled {
		t.Error("empty command should not be handled")
	}
}

func TestParserCommands(t *testing.T) {
	p := NewParser()
	p.Register("a", nil)
	p.Register("b", nil)
	names := p.Commands()
	if len(names) != 2 {
		t.Errorf("expected 2 commands, got %d", len(names))
	}
}
