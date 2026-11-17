package media

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/config"
	"nekocode/bot/extension/tool/runtime/workspace"
)

func TestImageGenTool_MissingPrompt(t *testing.T) {
	tool := &ImageGenTool{models: []config.ImageGenConfig{{Name: "test"}}}
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "prompt") {
		t.Errorf("expected prompt error, got: %v", err)
	}
}

func TestImageGenTool_NoModels(t *testing.T) {
	tool := &ImageGenTool{}
	_, err := tool.Execute(context.Background(), map[string]any{"prompt": "test"})
	if err == nil || !strings.Contains(err.Error(), "no image gen models configured") {
		t.Errorf("expected config error, got: %v", err)
	}
}

func TestImageGenTool_Interface(t *testing.T) {
	tool := NewImageGenTool([]config.ImageGenConfig{
		{Name: "jimeng", Provider: "jimeng"},
	})
	if tool.Name() != "image_gen" {
		t.Errorf("expected Name 'image_gen', got %q", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("Description should not be empty")
	}
	if len(tool.Parameters()) < 1 {
		t.Error("should have at least 1 parameter")
	}
}

func TestImageGenTool_ResolveModel(t *testing.T) {
	models := []config.ImageGenConfig{
		{Name: "jimeng", Provider: "jimeng", Model: "jimeng_t2i_v31"},
		{Name: "stable", Provider: "stable_diffusion", Model: "sdxl"},
	}

	tool := &ImageGenTool{models: models}

	// Match by name
	cfg, err := tool.resolveModel(map[string]any{"model": "stable"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "stable" {
		t.Errorf("expected stable, got %s", cfg.Name)
	}

	// An explicit unknown model must not select another model.
	cfg, err = tool.resolveModel(map[string]any{"model": "unknown"})
	if err == nil || cfg.Name != "" || !strings.Contains(err.Error(), "jimeng") || !strings.Contains(err.Error(), "stable") {
		t.Fatalf("unknown model silently changed: %+v %v", cfg, err)
	}
	for _, value := range []any{"", nil, 42} {
		if _, err := tool.resolveModel(map[string]any{"model": value}); err == nil {
			t.Fatalf("invalid model accepted: %v", value)
		}
	}

	// No model arg uses first
	cfg, err = tool.resolveModel(map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "jimeng" {
		t.Errorf("expected default jimeng, got %s", cfg.Name)
	}

	// No models returns empty
	empty := &ImageGenTool{}
	cfg, err = empty.resolveModel(map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "" {
		t.Errorf("expected empty config, got %s", cfg.Name)
	}
}

func TestImageGenTool_UnsupportedProvider(t *testing.T) {
	tool := &ImageGenTool{models: []config.ImageGenConfig{
		{Name: "unknown", Provider: "unknown_provider"},
	}}
	_, err := tool.Execute(context.Background(), map[string]any{"prompt": "test"})
	if err == nil || !strings.Contains(err.Error(), "unsupported image gen provider") {
		t.Errorf("expected unsupported provider error, got: %v", err)
	}
}

func TestResolveOutputDirRequiresWorkspaceWriteAccess(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	ctx := workspace.WithManager(context.Background(), workspace.New(root, nil))
	if _, err := resolveOutputDir(ctx, map[string]any{"output_dir": filepath.Join(outside, "generated")}); err == nil {
		t.Fatal("outside output directory was accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "generated")); !os.IsNotExist(err) {
		t.Fatalf("unauthorized output directory was created: %v", err)
	}
	got, err := resolveOutputDir(ctx, map[string]any{"output_dir": "generated"})
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(root, "generated") {
		t.Fatalf("relative output directory = %q", got)
	}
}

func TestUnknownImageModelFailsBeforeCreatingOutputDirectory(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "generated")
	tool := NewImageGenTool([]config.ImageGenConfig{{Name: "available-model", Provider: "unsupported"}})
	ctx := workspace.WithManager(context.Background(), workspace.New(root, nil))
	_, err := tool.Execute(ctx, map[string]any{"prompt": "example", "model": "typo-model", "output_dir": destination})
	if err == nil || !strings.Contains(err.Error(), "unknown image model") || !strings.Contains(err.Error(), "available-model") {
		t.Fatalf("unknown model did not produce actionable error: %v", err)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("invalid model created output directory: %v", err)
	}
}
