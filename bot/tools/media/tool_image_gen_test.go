package media

import (
	"context"
	"strings"
	"testing"

	"nekocode/bot/config"
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
	cfg := tool.resolveModel(map[string]any{"model": "stable"})
	if cfg.Name != "stable" {
		t.Errorf("expected stable, got %s", cfg.Name)
	}

	// Unknown name falls back to first
	cfg = tool.resolveModel(map[string]any{"model": "unknown"})
	if cfg.Name != "jimeng" {
		t.Errorf("expected fallback to jimeng, got %s", cfg.Name)
	}

	// No model arg uses first
	cfg = tool.resolveModel(map[string]any{})
	if cfg.Name != "jimeng" {
		t.Errorf("expected default jimeng, got %s", cfg.Name)
	}

	// No models returns empty
	empty := &ImageGenTool{}
	cfg = empty.resolveModel(map[string]any{})
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
