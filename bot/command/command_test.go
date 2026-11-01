package command

import (
	"context"
	"strings"
	"testing"

	"nekocode/bot/config"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool"
)

func TestNewBuildsCommandHandler(t *testing.T) {
	handler := New(Deps{
		CtxMgr:       ctxmgr.New(ctxmgr.Config{}),
		ToolRegistry: tools.New(),
		GetConfigFn:  func() config.ModelConfig { return config.ModelConfig{} },
		SwitchModel:  func(string) error { return nil },
	})

	if len(handler.Names()) == 0 {
		t.Fatal("New registered no commands")
	}
	if response, handled := handler.Execute(context.Background(), "/help", ctxmgr.New(ctxmgr.Config{})); !handled || response == "" {
		t.Fatalf("help result = %q, handled = %v", response, handled)
	}
}

func TestNewCommandDelegatesReset(t *testing.T) {
	manager := ctxmgr.New(ctxmgr.Config{})
	calls := 0
	handler := New(Deps{
		CtxMgr:       manager,
		ToolRegistry: tools.New(),
		GetConfigFn:  func() config.ModelConfig { return config.ModelConfig{} },
		SwitchModel:  func(string) error { return nil },
		ResetConversation: func() (string, error) {
			calls++
			return "reset", nil
		},
	})
	if result, handled := handler.Execute(context.Background(), "/new", manager); !handled || result != "reset" {
		t.Fatalf("/new result=%q handled=%v", result, handled)
	}
	if calls != 1 {
		t.Fatalf("reset calls = %d, want 1", calls)
	}
	if result, handled := handler.Execute(context.Background(), "/clear", manager); !handled || !strings.Contains(result, "Unknown command") {
		t.Fatalf("removed /clear result=%q handled=%v", result, handled)
	}
}

func TestRewindCommandDelegatesOptionalTurn(t *testing.T) {
	manager := ctxmgr.New(ctxmgr.Config{})
	var got string
	handler := New(Deps{
		CtxMgr: manager, ToolRegistry: tools.New(),
		GetConfigFn: func() config.ModelConfig { return config.ModelConfig{} },
		Rewind: func(turn string) (string, error) {
			got = turn
			return "rewound", nil
		},
	})
	result, handled := handler.Execute(context.Background(), "/rewind 2", manager)
	if !handled || result != "rewound" || got != "2" {
		t.Fatalf("rewind result=%q handled=%v turn=%q", result, handled, got)
	}
}

func TestModelCommandPreservesSpacedName(t *testing.T) {
	manager := ctxmgr.New(ctxmgr.Config{})
	var got string
	handler := New(Deps{
		CtxMgr: manager, ToolRegistry: tools.New(),
		GetConfigFn: func() config.ModelConfig { return config.ModelConfig{} },
		SwitchModel: func(name string) error {
			got = name
			return nil
		},
	})
	if _, handled := handler.Execute(context.Background(), "/model deep reasoner", manager); !handled {
		t.Fatal("model command was not handled")
	}
	if got != "deep reasoner" {
		t.Fatalf("model name = %q, want %q", got, "deep reasoner")
	}
}

func TestRegisterSkillsReplacesDynamicCommands(t *testing.T) {
	manager := ctxmgr.New(ctxmgr.Config{})
	handler := New(Deps{
		CtxMgr: manager, ToolRegistry: tools.New(),
		GetConfigFn: func() config.ModelConfig { return config.ModelConfig{} },
	})
	loaded := false
	handler.RegisterSkills([]SkillRegistration{{
		Name: "review", Load: func() (string, bool) { return "review context", true }, MarkLoaded: func() { loaded = true },
	}})
	if _, handled := handler.Execute(context.Background(), "$review inspect", manager); handled {
		t.Fatal("skill with arguments should continue into the agent")
	}
	if !loaded {
		t.Fatal("skill loaded callback was not invoked")
	}

	handler.RegisterSkills(nil)
	if result, handled := handler.Execute(context.Background(), "$review", manager); !handled || result == "" {
		t.Fatal("replaced skill command remained registered")
	}
}

func TestSelectedAndCommandSkillsHaveExplicitWorkflowProvenance(t *testing.T) {
	manager := ctxmgr.New(ctxmgr.Config{ContextWindow: 64000})
	handler := New(Deps{CtxMgr: manager, ToolRegistry: tools.New(), GetConfigFn: func() config.ModelConfig { return config.ModelConfig{} }})
	handler.SelectSkill(manager, "selected workflow")
	if manager.Snapshot().Messages[0].IsConversationUser() {
		t.Fatal("selected workflow counted as user request")
	}
	manager.BuildRequest(ctxmgr.ModelRequest{})
	if manager.PrefixDiagnostics().SkillMessages != 1 {
		t.Fatal("selected workflow absent from diagnostic")
	}
	handler.ClearSkill(manager)
	handler.RegisterSkills([]SkillRegistration{{Name: "review", Load: func() (string, bool) { return "command workflow", true }}})
	if _, handled := handler.Execute(t.Context(), "$review actual request", manager); handled {
		t.Fatal("skill arguments did not start agent")
	}
	messages := manager.Snapshot().Messages
	if len(messages) != 2 || messages[0].IsConversationUser() || !messages[1].IsConversationUser() {
		t.Fatal("workflow and user argument provenance mixed")
	}
	manager.BuildRequest(ctxmgr.ModelRequest{})
	if manager.PrefixDiagnostics().SkillMessages != 1 {
		t.Fatal("command workflow absent from diagnostic")
	}
}
