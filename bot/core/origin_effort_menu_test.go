package core

import (
	"context"
	"nekocode/bot/command"
	"nekocode/bot/config"
	"strings"
	"testing"
)

func TestRelayEffortMenuLabelsOriginReference(t *testing.T) {
	cfg := config.Config{Providers: []config.ProviderConfig{{Name: "relay", BaseURL: "https://copilot.tencent.com/v2/chat/completions", Protocol: "openai", Models: []config.ModelDefinition{{Model: "glm-5.3-flash"}}}}}
	b := &Bot{cfg: &cfg}
	p := command.NewParser()
	p.Register("effort", nil)
	b.registerCommandMenus(p)
	menu, ok := p.Menu(context.Background(), "/effort")
	if !ok || len(menu.Items) != 4 {
		t.Fatalf("relay menu still has no efforts: %+v", menu)
	}
	if menu.Items[1].Value != "/effort low" || menu.Items[3].Value != "/effort max" {
		t.Fatal(menu.Items)
	}
	for _, item := range menu.Items {
		if !strings.Contains(item.Description, "原厂参考") || !strings.Contains(item.Description, "当前服务未确认") {
			t.Fatalf("reference hidden: %+v", item)
		}
	}
}
