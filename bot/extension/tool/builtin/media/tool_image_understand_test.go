package media

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/config"
	"nekocode/bot/extension/tool/runtime/workspace"
)

var testPNG = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0, 'I', 'H', 'D', 'R'}

func imageUnderstandTestContext(t *testing.T) (context.Context, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.png")
	if err := os.WriteFile(path, testPNG, 0o600); err != nil {
		t.Fatal(err)
	}
	return workspace.WithManager(context.Background(), workspace.New(dir, nil)), path
}

func TestImageUnderstandToolOpenAI(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("unexpected authorization: %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"a tiny test image"}}]}`))
	}))
	defer server.Close()

	ctx, path := imageUnderstandTestContext(t)
	tool := NewImageUnderstandTool([]config.ImageUnderstandConfig{{
		Name: "vision", Provider: "openai", Protocol: "openai", APIKey: "secret", Model: "vision-1", BaseURL: server.URL,
	}})
	output, err := tool.Execute(ctx, map[string]any{"path": path, "prompt": "What is shown?"})
	if err != nil {
		t.Fatal(err)
	}
	if output != "a tiny test image" {
		t.Fatalf("unexpected output: %q", output)
	}
	if received["model"] != "vision-1" {
		t.Fatalf("unexpected model: %#v", received["model"])
	}
	if received["max_tokens"] != float64(fastImageMaxTokens) {
		t.Fatalf("unexpected fast mode max_tokens: %#v", received["max_tokens"])
	}
	messages := received["messages"].([]any)
	content := messages[0].(map[string]any)["content"].([]any)
	imageURL := content[1].(map[string]any)["image_url"].(map[string]any)["url"].(string)
	if !strings.HasPrefix(imageURL, "data:image/png;base64,") {
		t.Fatalf("image was not encoded as a PNG data URL: %.40s", imageURL)
	}
}

func TestImageUnderstandToolAccurateModeSelectsAccurateModel(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"careful result"}}]}`))
	}))
	defer server.Close()

	ctx, path := imageUnderstandTestContext(t)
	tool := NewImageUnderstandTool([]config.ImageUnderstandConfig{
		{Name: "fast", Provider: "openai", APIKey: "secret", Model: "fast-model", BaseURL: server.URL},
		{Name: "accurate", Provider: "openai", APIKey: "secret", Model: "accurate-model", BaseURL: server.URL},
	})
	output, err := tool.Execute(ctx, map[string]any{"path": path, "mode": "accurate"})
	if err != nil {
		t.Fatal(err)
	}
	if output != "careful result" {
		t.Fatalf("unexpected output: %q", output)
	}
	if received["model"] != "accurate-model" {
		t.Fatalf("accurate mode selected model %#v", received["model"])
	}
	if received["max_tokens"] != float64(accurateImageMaxTokens) {
		t.Fatalf("unexpected accurate mode max_tokens: %#v", received["max_tokens"])
	}
}

func TestImageUnderstandToolAccurateModeRequiresModel(t *testing.T) {
	ctx, path := imageUnderstandTestContext(t)
	tool := NewImageUnderstandTool([]config.ImageUnderstandConfig{{Name: "vision", Model: "vision-1"}})
	_, err := tool.Execute(ctx, map[string]any{"path": path, "mode": "accurate"})
	if err == nil || !strings.Contains(err.Error(), `model is not configured`) {
		t.Fatalf("expected missing accurate model error, got %v", err)
	}
}

func TestImageUnderstandToolRejectsUnknownMode(t *testing.T) {
	tool := NewImageUnderstandTool([]config.ImageUnderstandConfig{{Name: "fast", Model: "vision-1"}})
	_, err := tool.Execute(context.Background(), map[string]any{"path": "image.png", "mode": "turbo"})
	if err == nil || !strings.Contains(err.Error(), `use fast or accurate`) {
		t.Fatalf("expected unknown mode error, got %v", err)
	}
}

func TestPrepareImageForUnderstandingResizesFastMode(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 2000, 1000))
	source.Set(1999, 999, color.RGBA{R: 255, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}

	got, mediaType, err := prepareImageForUnderstanding(encoded.Bytes(), "image/png", imageUnderstandModeFast)
	if err != nil {
		t.Fatal(err)
	}
	if mediaType != "image/png" {
		t.Fatalf("media type = %q", mediaType)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(got))
	if err != nil {
		t.Fatal(err)
	}
	if config.Width != 1280 || config.Height != 640 {
		t.Fatalf("resized dimensions = %dx%d, want 1280x640", config.Width, config.Height)
	}

	accurate, accurateType, err := prepareImageForUnderstanding(encoded.Bytes(), "image/png", imageUnderstandModeAccurate)
	if err != nil {
		t.Fatal(err)
	}
	if accurateType != "image/png" || !bytes.Equal(accurate, encoded.Bytes()) {
		t.Fatal("accurate mode did not preserve the original image")
	}
}

func TestFastImageDimensionsRejectDecompressionBomb(t *testing.T) {
	if err := validateFastImageDimensions(100_000, 100_000); err == nil {
		t.Fatal("fast mode accepted image dimensions that could exhaust process memory")
	}
	if err := validateFastImageDimensions(5000, 5000); err != nil {
		t.Fatalf("fast mode rejected a 25 megapixel image: %v", err)
	}
}

func TestImageUnderstandToolAnthropic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "secret" {
			t.Errorf("unexpected api key: %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		messages := body["messages"].([]any)
		content := messages[0].(map[string]any)["content"].([]any)
		source := content[0].(map[string]any)["source"].(map[string]any)
		if source["media_type"] != "image/png" || source["data"] == "" {
			t.Fatalf("unexpected image source: %#v", source)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"first"},{"type":"text","text":"second"}]}`))
	}))
	defer server.Close()

	ctx, path := imageUnderstandTestContext(t)
	tool := NewImageUnderstandTool([]config.ImageUnderstandConfig{{
		Name: "claude", Provider: "anthropic", APIKey: "secret", Model: "claude-vision", BaseURL: server.URL,
	}})
	output, err := tool.Execute(ctx, map[string]any{"path": path})
	if err != nil {
		t.Fatal(err)
	}
	if output != "first\nsecond" {
		t.Fatalf("unexpected output: %q", output)
	}
}

func TestImageUnderstandToolRejectsUnknownModel(t *testing.T) {
	tool := NewImageUnderstandTool([]config.ImageUnderstandConfig{{Name: "known"}})
	_, err := tool.Execute(context.Background(), map[string]any{"path": "image.png", "model": "missing"})
	if err == nil || !strings.Contains(err.Error(), `model "missing" is not configured`) {
		t.Fatalf("expected unknown model error, got %v", err)
	}
}

func TestImageUnderstandToolRejectsNonImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("not an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := workspace.WithManager(context.Background(), workspace.New(dir, nil))
	tool := NewImageUnderstandTool([]config.ImageUnderstandConfig{{Name: "vision", Provider: "openai", Model: "vision-1"}})
	_, err := tool.Execute(ctx, map[string]any{"path": path})
	if err == nil || !strings.Contains(err.Error(), "unsupported image type") {
		t.Fatalf("expected image type error, got %v", err)
	}
}

func TestImageUnderstandPermissionPlanNamesFileAndDestination(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.png")
	if err := os.WriteFile(path, testPNG, 0o600); err != nil {
		t.Fatal(err)
	}
	tool := NewImageUnderstandTool([]config.ImageUnderstandConfig{{
		Name: "vision", Provider: "openai", Model: "vision-1", BaseURL: "https://vision.example/v1",
	}})
	plan := tool.PermissionPlan(map[string]any{"path": "sample.png"}, dir)
	if plan.Scope != "once" || !strings.Contains(plan.Reason, path) || !strings.Contains(plan.Reason, "vision.example") {
		t.Fatalf("permission plan does not identify one-time file upload: %+v", plan)
	}
}

func TestImageUnderstandRejectsFileChangedAfterPermissionPlan(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"unexpected"}}]}`))
	}))
	defer server.Close()
	ctx, path := imageUnderstandTestContext(t)
	tool := NewImageUnderstandTool([]config.ImageUnderstandConfig{{
		Name: "vision", Provider: "openai", APIKey: "secret", Model: "vision-1", BaseURL: server.URL,
	}})
	args := map[string]any{"path": path}
	tool.PermissionPlan(args, filepath.Dir(path))
	changed := append(append([]byte(nil), testPNG...), byte(1))
	if err := os.WriteFile(path, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := tool.Execute(ctx, args)
	if err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("Execute error = %v", err)
	}
	if called {
		t.Fatal("changed image was sent to the remote model")
	}
}

func TestImageUnderstandToolRejectsInsecureRemoteEndpoint(t *testing.T) {
	ctx, path := imageUnderstandTestContext(t)
	tool := NewImageUnderstandTool([]config.ImageUnderstandConfig{{
		Name: "vision", Provider: "openai", Model: "vision-1", BaseURL: "http://example.com/v1",
	}})
	_, err := tool.Execute(ctx, map[string]any{"path": path})
	if err == nil || !strings.Contains(err.Error(), "requires HTTPS") {
		t.Fatalf("expected insecure endpoint error, got %v", err)
	}
}

func TestImageUnderstandToolDoesNotFollowRedirects(t *testing.T) {
	redirected := false
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirected = true
	}))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()

	ctx, path := imageUnderstandTestContext(t)
	tool := NewImageUnderstandTool([]config.ImageUnderstandConfig{{
		Name: "vision", Provider: "openai", APIKey: "secret", Model: "vision-1", BaseURL: server.URL,
	}})
	_, err := tool.Execute(ctx, map[string]any{"path": path})
	if err == nil || !strings.Contains(err.Error(), "307") {
		t.Fatalf("expected redirect rejection, got %v", err)
	}
	if redirected {
		t.Fatal("image request followed redirect to an unapproved destination")
	}
}
