package pluginops

import (
	"errors"
	"strings"
	"testing"
)

func TestParseInstallArgs(t *testing.T) {
	if got := ParseInstallArgs(nil); got.OK {
		t.Fatal("empty args should not parse")
	}
	got := ParseInstallArgs([]string{"owner/repo", "--yes"})
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
