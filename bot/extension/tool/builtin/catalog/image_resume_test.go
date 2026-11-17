package catalog

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"nekocode/bot/config"
	"nekocode/bot/extension/tool/runtime/workspace"
)

func TestImageResumeSurvivesRegistryRebuild(t *testing.T) {
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	submits, queries := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/new" {
			t.Error("recovery switched to new model configuration")
		}
		if r.URL.Query().Get("Action") == "CVSync2AsyncSubmitTask" {
			submits++
			w.Write([]byte(`{"code":10000,"data":{"task_id":"accepted-before-rebuild"}}`))
			return
		}
		queries++
		if queries == 1 {
			w.WriteHeader(503)
			return
		}
		w.Write([]byte(`{"code":10000,"data":{"status":"done","binary_data_base64":["` + base64.StdEncoding.EncodeToString(imageData.Bytes()) + `"]}}`))
	}))
	defer server.Close()
	dir := t.TempDir()
	cfg := ToolboxConfig{Workspace: workspace.New(dir, nil), ImageGenModels: []config.ImageGenConfig{{Name: "old", Provider: "jimeng", BaseURL: server.URL, APIKey: "fixture", SecretKey: "fixture"}}}
	box := NewToolboxWithConfig(cfg)
	defer box.Close()
	ctx := workspace.WithManager(t.Context(), box.Workspace())
	tool, _ := box.Registry.Get("image_gen")
	out, err := tool.Execute(ctx, map[string]any{"prompt": "generate", "output_dir": dir})
	if err == nil {
		t.Fatal("expected polling failure")
	}
	start := strings.Index(out.Text, `{"resume_id":`)
	if start < 0 {
		t.Fatalf("no recovery receipt: %q", out.Text)
	}
	end := strings.Index(out.Text[start:], "}") + start + 1
	var recovery map[string]any
	if err := json.Unmarshal([]byte(out.Text[start:end]), &recovery); err != nil {
		t.Fatal(err)
	}
	// Disabling and re-enabling the tool must also preserve accepted tasks.
	box.RebuildRegistryWithConfig(ToolboxConfig{})
	if box.Registry.Has("image_gen") {
		t.Fatal("disabled image tool still exposed")
	}
	cfg.ImageGenModels[0].Name, cfg.ImageGenModels[0].BaseURL = "new", server.URL+"/new"
	box.RebuildRegistryWithConfig(cfg)
	tool, _ = box.Registry.Get("image_gen")
	for _, parameter := range tool.Parameters() {
		if parameter.Name == "model" && !slices.Equal(parameter.Enum, []string{"new"}) {
			t.Fatalf("stale schema after rebuild: %v", parameter.Enum)
		}
	}
	out, err = tool.Execute(ctx, recovery)
	if err != nil || submits != 1 || queries != 2 {
		t.Fatalf("recovery failed or resubmitted: %q %v submits=%d queries=%d", out.Text, err, submits, queries)
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 1 {
		t.Fatalf("resumed image not saved: %v", files)
	}
}
