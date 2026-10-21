package media

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nekocode/bot/config"
	"nekocode/bot/extension/tool/runtime/workspace"
)

// This test sends a generated image to the locally configured accurate model.
// Opt in explicitly because it requires network access and incurs API usage.
func TestImageUnderstandLiveAccurate(t *testing.T) {
	if os.Getenv("NEKOCODE_TEST_IMAGE_UNDERSTAND_LIVE") != "1" {
		t.Skip("set NEKOCODE_TEST_IMAGE_UNDERSTAND_LIVE=1 to call the configured API")
	}
	data, err := os.ReadFile(config.Path())
	if err != nil {
		t.Fatal(err)
	}
	var cfg config.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	var selected *config.ImageUnderstandConfig
	for i := range cfg.ImageUnderstandModels {
		if cfg.ImageUnderstandModels[i].Name == "accurate" {
			selected = &cfg.ImageUnderstandModels[i]
			break
		}
	}
	if selected == nil {
		t.Fatal("no accurate image understanding model configured")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "red-blue.png")
	img := image.NewRGBA(image.Rect(0, 0, 256, 128))
	for y := 0; y < 128; y++ {
		for x := 0; x < 256; x++ {
			c := color.RGBA{R: 255, A: 255}
			if x >= 128 {
				c = color.RGBA{B: 255, A: 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	encodeErr := png.Encode(f, img)
	closeErr := f.Close()
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	// A second, visibly different image checks input ordering in a real batch.
	second := filepath.Join(dir, "green-yellow.png")
	for y := 0; y < 128; y++ {
		for x := 0; x < 256; x++ {
			c := color.RGBA{G: 255, A: 255}
			if x >= 128 {
				c.R = 255
			}
			img.SetRGBA(x, y, c)
		}
	}
	f, err = os.Create(second)
	if err != nil {
		t.Fatal(err)
	}
	encodeErr = png.Encode(f, img)
	closeErr = f.Close()
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	for _, tc := range []struct {
		name   string
		args   map[string]any
		colors []string
	}{
		{"single_accurate", map[string]any{"path": path, "mode": "accurate"}, []string{"red", "blue"}},
		{"batch_fast", map[string]any{"paths": []string{path, second}, "mode": "fast"}, []string{"red", "blue", "green", "yellow"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			defer cancel()
			ctx = workspace.WithManager(ctx, workspace.New(dir, nil))
			tool := NewImageUnderstandTool([]config.ImageUnderstandConfig{*selected})
			tc.args["model"] = selected.Name
			tc.args["prompt"] = "For each image in input order, identify its left and right colors. Reply in English with one short numbered sentence per image."
			tool.PermissionPlan(tc.args, dir)
			t.Logf("Calling model %q via %s at %s", selected.Model, selected.Protocol, selected.BaseURL)
			started := time.Now()
			output, err := tool.Execute(ctx, tc.args)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("Elapsed: %s; response: %s", time.Since(started), output.Text)
			answer := strings.ToLower(output.Text)
			last := -1
			for _, color := range tc.colors {
				index := strings.Index(answer, color)
				if index <= last {
					t.Fatalf("expected ordered colors %v, got %q", tc.colors, output.Text)
				}
				last = index
			}
		})
	}
}
