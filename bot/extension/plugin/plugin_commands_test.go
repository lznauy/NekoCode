package plugin

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestInstallUsageDocumentsYes(t *testing.T) {
	for _, usage := range []string{Usage(), InstallUsage} {
		if !strings.Contains(usage, "install <source> [--yes]") {
			t.Fatalf("usage missing install options: %s", usage)
		}
		if !strings.Contains(usage, "--yes") || !strings.Contains(usage, "preview and confirmation") {
			t.Fatalf("usage missing --yes behavior: %s", usage)
		}
	}
}

func TestFetchRemotePreview(t *testing.T) {
	p, err := fetchRemotePreview(context.Background(), "https://github.com/owner/repo", func(_ context.Context, url string) ([]byte, error) {
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
	if _, err := fetchRemotePreview(context.Background(), "https://github.com/owner/repo", func(context.Context, string) ([]byte, error) {
		return nil, errors.New("boom")
	}); err == nil || !strings.Contains(err.Error(), "fetch plugin info") {
		t.Fatalf("expected fetch error, got %v", err)
	}
	if _, err := fetchRemotePreview(context.Background(), "https://github.com/owner/repo", func(context.Context, string) ([]byte, error) {
		return []byte(`{}`), nil
	}); err == nil || !strings.Contains(err.Error(), "invalid plugin.json") {
		t.Fatalf("expected manifest error, got %v", err)
	}
	if _, err := fetchRemotePreview(context.Background(), "owner/repo", func(context.Context, string) ([]byte, error) {
		return []byte(`{"name":"unused"}`), nil
	}); err == nil || !strings.Contains(err.Error(), "preview URL not available") {
		t.Fatalf("expected preview URL error, got %v", err)
	}
}

func TestConfirmSummary(t *testing.T) {
	p, err := fetchRemotePreview(context.Background(), "https://github.com/owner/repo", func(context.Context, string) ([]byte, error) {
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

func TestIsLocalSource(t *testing.T) {
	if !IsLocalSource("./plugin") {
		t.Fatal("relative path should be local")
	}
	if IsLocalSource("owner/repo") {
		t.Fatal("repository shorthand should be remote")
	}
}
