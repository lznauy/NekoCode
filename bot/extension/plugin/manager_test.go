package plugin

import (
	"errors"
	"strings"
	"testing"
)

func TestParseInstallArgs(t *testing.T) {
	if got := parseInstallArgs(nil); got.OK {
		t.Fatal("empty args should not parse")
	}
	got := parseInstallArgs([]string{"owner/repo", "--yes"})
	if !got.OK || got.Source != "owner/repo" || !got.Confirmed {
		t.Fatalf("unexpected args: %+v", got)
	}
}

func TestFetchRemotePreview(t *testing.T) {
	p, err := FetchRemotePreview("https://github.com/owner/repo", func(url string) ([]byte, error) {
		if !strings.Contains(url, "owner/repo") {
			t.Fatalf("unexpected URL: %s", url)
		}
		return []byte(`{"name":"demo","version":"1.0.0"}`), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "demo" || p.Source != "https://github.com/owner/repo" {
		t.Fatalf("unexpected plugin: %+v", p)
	}
}

func TestFetchRemotePreviewErrors(t *testing.T) {
	if _, err := FetchRemotePreview("https://github.com/owner/repo", func(string) ([]byte, error) {
		return nil, errors.New("boom")
	}); err == nil || !strings.Contains(err.Error(), "fetch plugin info") {
		t.Fatalf("expected fetch error, got %v", err)
	}
	if _, err := FetchRemotePreview("https://github.com/owner/repo", func(string) ([]byte, error) {
		return []byte(`{}`), nil
	}); err == nil || !strings.Contains(err.Error(), "invalid plugin.json") {
		t.Fatalf("expected manifest error, got %v", err)
	}
	if _, err := FetchRemotePreview("owner/repo", func(string) ([]byte, error) {
		return []byte(`{"name":"unused"}`), nil
	}); err == nil || !strings.Contains(err.Error(), "preview URL not available") {
		t.Fatalf("expected preview URL error, got %v", err)
	}
}

func TestConfirmSummary(t *testing.T) {
	p, err := FetchRemotePreview("https://github.com/owner/repo", func(string) ([]byte, error) {
		return []byte(`{"name":"demo"}`), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ConfirmSummary(p, true), "install.sh will not be executed") {
		t.Fatal("remote summary missing install.sh note")
	}
	if strings.Contains(ConfirmSummary(p, false), "install.sh will not be executed") {
		t.Fatal("local summary should not include remote note")
	}
}

func TestRequirePlugin(t *testing.T) {
	if got := requirePlugin(nil, nil, "usage"); got.OK || got.Message != "usage" {
		t.Fatalf("empty args = %+v", got)
	}
	lookup := func(name string) (*Plugin, bool) {
		if name == "ok" {
			return &Plugin{Manifest: Manifest{Name: "ok"}}, true
		}
		return nil, false
	}
	if got := requirePlugin([]string{"missing"}, lookup, "usage"); got.OK || got.Message != `Plugin "missing" not found.` {
		t.Fatalf("missing = %+v", got)
	}
	if got := requirePlugin([]string{"ok"}, lookup, "usage"); !got.OK || got.Plugin.Name != "ok" {
		t.Fatalf("found = %+v", got)
	}
}

func TestManageMessages(t *testing.T) {
	err := errors.New("boom")
	checks := []string{
		alreadyEnabled("p"),
		alreadyDisabled("p"),
		enabled("p"),
		disabled("p"),
		uninstalled("p"),
		installFailed(err),
		uninstallFailed(err),
		enableFailed(err),
		disableFailed(err),
	}
	for _, msg := range checks {
		if msg == "" {
			t.Fatal("empty message")
		}
	}
}

func TestIsMCPToolForClient(t *testing.T) {
	if !IsMCPToolForClient("srv__tool", "srv") {
		t.Fatal("expected MCP tool match")
	}
	if IsMCPToolForClient("srv2__tool", "srv") {
		t.Fatal("should not match another server prefix")
	}
	if IsMCPToolForClient("srv_tool", "srv") {
		t.Fatal("should require double underscore separator")
	}
}
