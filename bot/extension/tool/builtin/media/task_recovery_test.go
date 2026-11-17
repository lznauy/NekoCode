package media

import (
	"bytes"
	"context"
	"encoding/base64"
	"nekocode/bot/config"
	"nekocode/bot/extension/tool/runtime/workspace"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImageUnderstandRejectsInvalidExplicitModel(t *testing.T) {
	tool := NewImageUnderstandTool([]config.ImageUnderstandConfig{{Name: "first"}})
	for _, v := range []any{nil, 42, []any{"first"}, " "} {
		if _, err := tool.resolveUnderstandModel(map[string]any{"model": v}, imageUnderstandModeFast); err == nil {
			t.Fatalf("invalid model accepted: %#v", v)
		}
	}
}
func TestAcceptedImageTaskCanResumeWithoutResubmitting(t *testing.T) {
	image := generatedPNG(t)
	submissions, queries := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("Action") == "CVSync2AsyncSubmitTask" {
			submissions++
			w.Write([]byte(`{"code":10000,"data":{"task_id":"accepted-task"}}`))
			return
		}
		queries++
		if queries == 1 {
			w.WriteHeader(503)
			return
		}
		w.Write([]byte(`{"code":10000,"data":{"status":"done","binary_data_base64":["` + base64.StdEncoding.EncodeToString(image) + `"]}}`))
	}))
	defer server.Close()
	dir := t.TempDir()
	ctx := workspace.WithManager(context.Background(), workspace.New(dir, nil))
	tool := NewImageGenTool([]config.ImageGenConfig{{Name: "test", Provider: "jimeng", BaseURL: server.URL, APIKey: "fixture", SecretKey: "fixture"}})
	out, err := tool.Execute(ctx, map[string]any{"prompt": "generate", "output_dir": dir})
	if err == nil || !strings.Contains(out.Text, "accepted-task") || !strings.Contains(out.Text, "resume_id") {
		t.Fatalf("receipt missing: %q %v", out.Text, err)
	}
	var id string
	for key := range tool.pending {
		id = key
	}
	if _, err := tool.Execute(ctx, map[string]any{"resume_id": id, "prompt": "duplicate"}); err == nil {
		t.Fatal("resume allowed new submission arguments")
	}
	foreign := workspace.WithManager(context.Background(), workspace.New(t.TempDir(), nil))
	if _, err := tool.Execute(foreign, map[string]any{"resume_id": id}); err == nil {
		t.Fatal("receipt crossed workspace boundary")
	}
	out, err = tool.Execute(ctx, map[string]any{"resume_id": id})
	if err != nil || submissions != 1 || queries != 2 {
		t.Fatalf("resume failed or submitted again: %q %v submissions=%d queries=%d", out.Text, err, submissions, queries)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "nekocode_img_*"))
	if len(files) != 1 {
		t.Fatalf("resumed result not saved: %v", files)
	}
	if data, _ := os.ReadFile(files[0]); !bytes.Equal(data, image) {
		t.Fatal("wrong resumed image")
	}
}
