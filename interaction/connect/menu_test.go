package connect

import (
	"context"
	"testing"

	"nekocode/protocol"
	controlruntime "nekocode/runtime"
)

type menuRuntime struct{}

func (menuRuntime) StartRun(context.Context, controlruntime.Input) (controlruntime.RunID, error) {
	return "", nil
}
func (menuRuntime) CancelRun(context.Context, controlruntime.RunID) error { return nil }
func (menuRuntime) DecideApproval(context.Context, string, controlruntime.ApprovalDecision) error {
	return nil
}
func (menuRuntime) AnswerQuestion(context.Context, string, protocol.QuestionReply) error { return nil }
func (menuRuntime) Events(context.Context, controlruntime.EventFilter) (<-chan controlruntime.Event, error) {
	return make(chan controlruntime.Event), nil
}
func (menuRuntime) ReportConnectorStatus(controlruntime.ConnectorStatusPayload) {}
func (menuRuntime) ExecuteLocalCommand(context.Context, string) (string, controlruntime.LocalCommandResult) {
	return "", controlruntime.LocalCommandNotCommand
}
func (menuRuntime) CommandMenu(_ context.Context, input string) (protocol.CommandMenu, bool) {
	switch input {
	case "/":
		return protocol.CommandMenu{Title: "Commands", Items: []protocol.CommandMenuItem{
			{Value: "/model", Label: "/model"},
			{Value: "/new", Label: "/new", Submit: true},
		}}, true
	case "/model":
		return protocol.CommandMenu{Title: "Models", Items: []protocol.CommandMenuItem{
			{Value: "/model fast", Label: "fast", Submit: true},
		}}, true
	default:
		return protocol.CommandMenu{}, false
	}
}

func TestCommandMenusResolveNestedAndNumericChoices(t *testing.T) {
	menus := NewCommandMenus()
	rt := menuRuntime{}
	root := menus.HandleText(context.Background(), rt, "chat-1", "/help")
	if root.Prompt == nil || len(root.Prompt.Choices) != 2 {
		t.Fatalf("root = %+v", root)
	}
	if invalid := menus.HandleText(context.Background(), rt, "chat-1", "99"); !invalid.Handled || invalid.Message == "" {
		t.Fatalf("invalid numeric selection = %+v", invalid)
	}

	nested := menus.Select(context.Background(), rt, "chat-1", root.Prompt.Choices[0].Token)
	if nested.Prompt == nil || nested.Prompt.Title != "Models" {
		t.Fatalf("nested = %+v", nested)
	}

	selected := menus.HandleText(context.Background(), rt, "chat-1", "1")
	if selected.Command != "/model fast" {
		t.Fatalf("numeric selection = %+v", selected)
	}
	replayed := menus.Select(context.Background(), rt, "chat-1", nested.Prompt.Choices[0].Token)
	if replayed.Message == "" || replayed.Command != "" {
		t.Fatalf("consumed command token replay = %+v", replayed)
	}
}

func TestCommandMenusRejectCrossConversationToken(t *testing.T) {
	menus := NewCommandMenus()
	root := menus.HandleText(context.Background(), menuRuntime{}, "chat-1", "/")
	got := menus.Select(context.Background(), menuRuntime{}, "chat-2", root.Prompt.Choices[1].Token)
	if got.Message == "" || !got.Handled {
		t.Fatalf("cross-conversation selection = %+v", got)
	}
}

func TestConnectorCommandMenusExposeOnlyRealFiniteActions(t *testing.T) {
	tests := []struct {
		platform string
		actions  []struct {
			name   string
			submit bool
		}
	}{
		{platform: "telegram", actions: []struct {
			name   string
			submit bool
		}{{"connect", true}, {"add", false}, {"token", false}, {"profiles", true}, {"list", true}, {"use", false}, {"pair", false}, {"unpair", false}, {"remove", false}, {"delete", false}, {"reset", true}, {"status", true}, {"disconnect", true}, {"stop", true}}},
		{platform: "feishu", actions: []struct {
			name   string
			submit bool
		}{{"connect", true}, {"add", false}, {"pair", true}, {"unpair", true}, {"reset", true}, {"status", true}, {"disconnect", true}, {"stop", true}}},
		{platform: "wecom", actions: []struct {
			name   string
			submit bool
		}{{"connect", true}, {"add", false}, {"pair", true}, {"unpair", true}, {"reset", true}, {"start", true}, {"status", true}, {"disconnect", true}, {"stop", true}}},
		{platform: "qqbot", actions: []struct {
			name   string
			submit bool
		}{{"add", false}, {"sandbox", false}, {"start", true}, {"stop", true}, {"disconnect", true}, {"status", true}}},
	}
	for _, test := range tests {
		t.Run(test.platform, func(t *testing.T) {
			menu, ok := ConnectorCommandMenu(test.platform, nil, []string{"personal"})
			if !ok || len(menu.Items) != len(test.actions) {
				t.Fatalf("menu = %+v, ok=%v", menu, ok)
			}
			for i, want := range test.actions {
				item := menu.Items[i]
				if item.Label != want.name || item.Value != "/connect "+test.platform+" "+want.name || item.Submit != want.submit {
					t.Errorf("item %d = %+v, want %s submit=%v", i, item, want.name, want.submit)
				}
			}
		})
	}
}

func TestConnectorCommandMenusResolveProfilesAndSandbox(t *testing.T) {
	for _, action := range []string{"use", "pair", "unpair", "remove", "delete"} {
		menu, ok := ConnectorCommandMenu("telegram", []string{action}, []string{"work", "personal"})
		if !ok || len(menu.Items) != 2 {
			t.Fatalf("%s menu = %+v, ok=%v", action, menu, ok)
		}
		for i, name := range []string{"personal", "work"} {
			item := menu.Items[i]
			if item.Value != "/connect telegram "+action+" "+name || item.Label != name || !item.Submit {
				t.Errorf("%s item %d = %+v", action, i, item)
			}
		}
	}
	menu, ok := ConnectorCommandMenu("telegram", []string{"use"}, nil)
	if !ok || len(menu.Items) != 0 || menu.Empty == "" {
		t.Fatalf("empty profile menu = %+v, ok=%v", menu, ok)
	}
	menu, ok = ConnectorCommandMenu("qqbot", []string{"sandbox"}, nil)
	if !ok || len(menu.Items) != 2 || menu.Items[0].Value != "/connect qqbot sandbox on" || menu.Items[1].Value != "/connect qqbot sandbox off" || !menu.Items[0].Submit || !menu.Items[1].Submit {
		t.Fatalf("sandbox menu = %+v, ok=%v", menu, ok)
	}
}

func TestConnectorCommandMenusDoNotCaptureDirectArguments(t *testing.T) {
	for _, args := range [][]string{{"add"}, {"status"}, {"add", "123:secret"}, {"token", "personal", "123:secret"}, {"use", "personal"}} {
		if menu, ok := ConnectorCommandMenu("telegram", args, []string{"personal"}); ok {
			t.Fatalf("direct args %q returned menu %+v", args, menu)
		}
	}
}

func TestFormatMenuIncludesDescriptionsAndFallbackHint(t *testing.T) {
	text := FormatMenu(&MenuPrompt{Title: "Models", Choices: []MenuChoice{{Label: "fast", Description: "low latency"}}})
	if text != "Models\n\n1. fast — low latency\n\n回复序号进行选择。" {
		t.Fatalf("FormatMenu() = %q", text)
	}
}

func TestCommandMenusPaginateLargeRemoteMenus(t *testing.T) {
	menus := NewCommandMenus()
	items := make([]protocol.CommandMenuItem, 10)
	for i := range items {
		items[i] = protocol.CommandMenuItem{Value: "/pick " + string(rune('a'+i)), Label: string(rune('A' + i)), Submit: true}
	}
	first := menus.open("chat-1", protocol.CommandMenu{Title: "Choices", Items: items})
	if len(first.Choices) != menuPageSize+1 || first.Choices[len(first.Choices)-1].Label != "Next →" {
		t.Fatalf("first page = %+v", first)
	}
	second := menus.Select(context.Background(), menuRuntime{}, "chat-1", first.Choices[len(first.Choices)-1].Token)
	if second.Prompt == nil || second.Prompt.Title != "Choices · 2/2" {
		t.Fatalf("second page = %+v", second)
	}
	selected := menus.HandleText(context.Background(), menuRuntime{}, "chat-1", "1")
	if selected.Command != "/pick i" {
		t.Fatalf("second-page numeric selection = %+v", selected)
	}
}
