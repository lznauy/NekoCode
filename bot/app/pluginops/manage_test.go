package pluginops

import (
	"errors"
	"testing"

	"nekocode/bot/extension/plugin"
)

func TestRequirePlugin(t *testing.T) {
	if got := RequirePlugin(nil, nil, "usage"); got.OK || got.Message != "usage" {
		t.Fatalf("empty args = %+v", got)
	}
	lookup := func(name string) (*plugin.Plugin, bool) {
		if name == "ok" {
			return &plugin.Plugin{Manifest: plugin.Manifest{Name: "ok"}}, true
		}
		return nil, false
	}
	if got := RequirePlugin([]string{"missing"}, lookup, "usage"); got.OK || got.Message != `Plugin "missing" not found.` {
		t.Fatalf("missing = %+v", got)
	}
	if got := RequirePlugin([]string{"ok"}, lookup, "usage"); !got.OK || got.Plugin.Name != "ok" {
		t.Fatalf("found = %+v", got)
	}
}

func TestManageMessages(t *testing.T) {
	err := errors.New("boom")
	checks := []string{
		AlreadyEnabled("p"),
		AlreadyDisabled("p"),
		Enabled("p"),
		Disabled("p"),
		Uninstalled("p"),
		InstallFailed(err),
		UninstallFailed(err),
		EnableFailed(err),
		DisableFailed(err),
	}
	for _, msg := range checks {
		if msg == "" {
			t.Fatal("empty message")
		}
	}
}
