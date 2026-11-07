package web

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tools "nekocode/bot/extension/tool"
	readtool "nekocode/bot/extension/tool/builtin/filesystem/read"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/runner"
)

func TestWebSnapshotCanBeReadOnlyByOwningExecutor(t *testing.T) {
	fetch := &WebFetchTool{client: &http.Client{Transport: webRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return stubResponse(200, "<h1>Start</h1><p>"+strings.Repeat("body", 2000)+"</p><h2>Appendix</h2><p>exact recovered tail</p>"), nil
	})}}
	registry := tools.New(fetch, &readtool.ReadTool{})
	registry.Workspace().Configure(t.TempDir(), nil)
	executor := runner.NewExecutor(registry)
	executor.ExecutionState().OutputStore = core.NewOutputStore(t.TempDir())
	result := executor.ExecuteBatch(context.Background(), []core.ToolCallItem{{ID: "fetch", Name: "web_fetch", Args: map[string]any{"url": testTargetURL, "maxChars": 256}}})[0]
	if result.Error != "" || result.Artifact.Path == "" {
		t.Fatalf("fetch failed: %+v", result)
	}
	call := core.ToolCallItem{ID: "read", Name: "read", Args: map[string]any{"path": result.Artifact.Path, "startLine": 8.0, "endLine": 10.0}}
	read := executor.ExecuteBatch(context.Background(), []core.ToolCallItem{call})[0]
	if read.Error != "" || !strings.Contains(read.Output, "exact recovered tail") {
		t.Fatalf("cannot recover hidden source: %+v", read)
	}
	otherRegistry := tools.New(&readtool.ReadTool{})
	otherRegistry.Workspace().Configure(t.TempDir(), nil)
	other := runner.NewExecutor(otherRegistry)
	if got := other.ExecuteBatch(context.Background(), []core.ToolCallItem{call})[0]; got.Error == "" {
		t.Fatal("another executor read private snapshot")
	}
}

func TestWebResultPreservesContentBeforeFilteringAndTruncation(t *testing.T) {
	for _, prompt := range []string{"", "needle"} {
		store := core.NewOutputStore(t.TempDir())
		ctx := core.WithOutputStore(context.Background(), store)
		content := "# Overview\n\n" + strings.Repeat("正文", 1500) + "\n\n## Target\n\nneedle\n\n## Omitted appendix\n\nrecover this exact tail"
		out, err := webResult(ctx, "https://user:password@example.com/doc?page=2&token=SECRET", "jina-html", content, prompt, 1000, false)
		if err != nil {
			t.Fatal(err)
		}
		if len([]rune(out.Text)) > 1000 || out.Artifact.Path == "" || out.Artifact.Incomplete {
			t.Fatalf("bad bounded result: %+v", out)
		}
		if err := store.CheckArtifact(out.Artifact); err != nil {
			t.Fatal(err)
		}
		snapshot, err := os.ReadFile(out.Artifact.Path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(string(snapshot), content) || strings.Contains(string(snapshot), "password") || strings.Contains(string(snapshot), "SECRET") || !strings.Contains(string(snapshot), "page=2") {
			t.Fatal("snapshot lost content or retained URL credentials")
		}
		lines := strings.Split(string(snapshot), "\n")
		for i, line := range lines {
			if line == "## Target" && !strings.Contains(out.Text, strings.TrimSpace(webOutline(line, i+1, 100))) {
				t.Fatalf("section line reference wrong: %s", out.Text)
			}
		}
		other := core.NewOutputStore(t.TempDir())
		if err := other.CheckArtifact(out.Artifact); err == nil {
			t.Fatal("snapshot leaked across actor stores")
		}
	}
}

func TestWebResultSmallBudgetAndOutlineIgnoresCode(t *testing.T) {
	content := "# Real\n\n```md\n# Code heading\n```\n\n" + strings.Repeat("text", 1000)
	out, err := webResult(context.Background(), "https://example.com", "direct-http", content, "", 256, true)
	if err != nil || len([]rune(out.Text)) > 256 || !strings.Contains(out.Text, "Fallback") || out.Artifact.Path == "" {
		t.Fatalf("small budget: %q %v", out.Text, err)
	}
	if outline := webOutline(content, 4, 1000); strings.Contains(outline, "Code heading") || !strings.Contains(outline, "4: # Real") {
		t.Fatalf("bad outline: %s", outline)
	}
}

func TestWebResultPreservationFailureAndCancellation(t *testing.T) {
	dir := t.TempDir()
	invalid := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(invalid, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx := core.WithOutputStore(context.Background(), core.NewOutputStore(invalid))
	if _, err := webResult(ctx, "", "exa", strings.Repeat("x", 2000), "", 256, false); err == nil {
		t.Fatal("failed preservation must not silently lose source")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := webResult(ctx, "", "exa", "short", "", 256, false); err == nil {
		t.Fatal("canceled result was saved")
	}
}
