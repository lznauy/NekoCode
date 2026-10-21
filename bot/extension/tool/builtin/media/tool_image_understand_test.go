package media

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if output.Render() != "a tiny test image" {
		t.Fatalf("unexpected output: %q", output.Render())
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
		{Name: "accurate", Provider: "openai", APIKey: "secret", Model: "glm-5.3-flash", BaseURL: server.URL},
	})
	output, err := tool.Execute(ctx, map[string]any{"path": path, "mode": "accurate"})
	if err != nil {
		t.Fatal(err)
	}
	if output.Render() != "careful result" {
		t.Fatalf("unexpected output: %q", output.Render())
	}
	if received["model"] != "glm-5.3-flash" {
		t.Fatalf("accurate mode selected model %#v", received["model"])
	}
	if thinking, ok := received["thinking"].(map[string]any); !ok || thinking["type"] != "disabled" {
		t.Fatalf("thinking was not disabled: %#v", received["thinking"])
	}
	if received["max_tokens"] != float64(1024) {
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

func TestImageUnderstandToolStreamingOnlyEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if body["stream"] != true {
			http.Error(w, "Non-stream chat request is currently not supported", http.StatusBadRequest)
			return
		}
		if body["model"] != "glm-5.3-flash" {
			t.Errorf("unexpected model: %v", body["model"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"private reasoning\"}}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{\"content\":\"red and \"}}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{\"content\":\"blue\"}}]}\n\n" +
			"data: [DONE]\n\n"))
	}))
	defer server.Close()
	ctx, path := imageUnderstandTestContext(t)
	tool := NewImageUnderstandTool([]config.ImageUnderstandConfig{{
		Name: "accurate", Provider: "tencent", Model: "glm-5.3-flash", BaseURL: server.URL,
	}})
	output, err := tool.Execute(ctx, map[string]any{"path": path, "mode": "accurate"})
	if err != nil {
		t.Fatal(err)
	}
	if output.Text != "red and blue" {
		t.Fatalf("unexpected output: %q", output.Text)
	}
}

func TestImageUnderstandingStreamErrors(t *testing.T) {
	for _, tc := range []struct {
		name, stream, want string
	}{
		{"provider error", "data: {\"error\":{\"message\":\"denied\"}}\n\n", "denied"},
		{"malformed", "data: invalid\n\n", "decode image model stream"},
		{"empty", "data: [DONE]\n\n", "empty response"},
		{"interrupted", "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n", "before completion"},
		{"oversized", strings.Repeat(": ping\n", (4<<20)/7+1), "response limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := readImageUnderstandingStream(strings.NewReader(tc.stream), nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}

func TestImageUnderstandingStreamFinishesWithoutWaitingForEOF(t *testing.T) {
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	go func() {
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial OCR\"},\"finish_reason\":\"length\"}]}\n\n")
	}()
	done := make(chan error, 1)
	go func() {
		text, err := readImageUnderstandingStream(r, nil)
		if err == nil && !strings.Contains(text, "increase max_tokens") {
			err = fmt.Errorf("missing truncation notice: %s", text)
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("finished stream waited for EOF")
	}
}

func TestImageUnderstandReportsTruncatedJSON(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic"} {
		t.Run(protocol, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if protocol == "openai" {
					_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"partial OCR"},"finish_reason":"length"}]}`)
				} else {
					_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"partial OCR"}],"stop_reason":"max_tokens"}`)
				}
			}))
			defer server.Close()
			ctx, path := imageUnderstandTestContext(t)
			tool := NewImageUnderstandTool([]config.ImageUnderstandConfig{{Name: "fast", Model: "vision", Protocol: protocol, BaseURL: server.URL}})
			out, err := tool.Execute(ctx, map[string]any{"path": path})
			if err != nil || !strings.Contains(out.Text, "increase max_tokens") {
				t.Fatalf("out=%q err=%v", out.Text, err)
			}
		})
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
		source := content[1].(map[string]any)["source"].(map[string]any)
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
	if output.Render() != "first\nsecond" {
		t.Fatalf("unexpected output: %q", output.Render())
	}
}

func TestImageUnderstandToolRejectsUnknownModel(t *testing.T) {
	tool := NewImageUnderstandTool([]config.ImageUnderstandConfig{{Name: "known"}})
	_, err := tool.Execute(context.Background(), map[string]any{"path": "image.png", "model": "missing"})
	if err == nil || !strings.Contains(err.Error(), `model "missing" is not configured`) {
		t.Fatalf("expected unknown model error, got %v", err)
	}
}

func TestImageUnderstandMultipleImages(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic"} {
		t.Run(protocol, func(t *testing.T) {
			ctx, first := imageUnderstandTestContext(t)
			second := filepath.Join(filepath.Dir(first), "second.png")
			secondData := append(append([]byte(nil), testPNG...), '2')
			if err := os.WriteFile(second, secondData, 0600); err != nil {
				t.Fatal(err)
			}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var body struct {
					MaxTokens int `json:"max_tokens"`
					Messages  []struct {
						Content []map[string]any `json:"content"`
					} `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				if body.MaxTokens != 2048 {
					t.Errorf("max_tokens=%d", body.MaxTokens)
				}
				content := body.Messages[0].Content
				if len(content) != 5 || content[1]["text"] != "Image 1:" || content[3]["text"] != "Image 2:" {
					t.Errorf("image labels/order missing: %#v", content)
					return
				}
				for i, want := range [][]byte{testPNG, secondData} {
					part := content[2+i*2]
					var encoded string
					if protocol == "openai" {
						encoded = strings.TrimPrefix(part["image_url"].(map[string]any)["url"].(string), "data:image/png;base64,")
					} else {
						encoded = part["source"].(map[string]any)["data"].(string)
					}
					if encoded != base64.StdEncoding.EncodeToString(want) {
						t.Errorf("image %d data/order mismatch", i+1)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				if protocol == "openai" {
					_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"two images"}}]}`))
				} else {
					_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"two images"}]}`))
				}
			}))
			defer server.Close()
			tool := NewImageUnderstandTool([]config.ImageUnderstandConfig{{Name: "accurate", Protocol: protocol, Model: "glm-5.3-flash", BaseURL: server.URL}})
			args := map[string]any{"paths": []any{first, second}, "mode": "accurate", "max_tokens": float64(2048)}
			plan := tool.PermissionPlan(args, filepath.Dir(first))
			if !strings.Contains(plan.Reason, second) {
				t.Fatal("second image missing from approval")
			}
			output, err := tool.Execute(ctx, args)
			if err != nil || output.Text != "two images" || calls != 1 {
				t.Fatalf("output=%q err=%v requests=%d", output.Text, err, calls)
			}
		})
	}
}

func TestImageUnderstandBatchApprovalRejectsChangedSecondImage(t *testing.T) {
	ctx, first := imageUnderstandTestContext(t)
	second := filepath.Join(filepath.Dir(first), "second.png")
	if err := os.WriteFile(second, testPNG, 0600); err != nil {
		t.Fatal(err)
	}
	tool := NewImageUnderstandTool([]config.ImageUnderstandConfig{{Name: "fast", Model: "glm-5.3-flash"}})
	args := map[string]any{"paths": []string{first, second}}
	tool.PermissionPlan(args, filepath.Dir(first))
	if err := os.WriteFile(second, append(append([]byte(nil), testPNG...), '2'), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := tool.Execute(ctx, args)
	if err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("expected changed image rejection, got %v", err)
	}
}

func TestImageUnderstandOutputTokens(t *testing.T) {
	for _, v := range []any{0, -1, 4097, 1.5, "1024"} {
		if _, err := imageUnderstandOutputTokens(map[string]any{"max_tokens": v}); err == nil {
			t.Fatalf("accepted %v", v)
		}
	}
	for _, v := range []any{1, 4096, float64(1024)} {
		if _, err := imageUnderstandOutputTokens(map[string]any{"max_tokens": v}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestImageUnderstandCombinedSizeLimit(t *testing.T) {
	ctx, first := imageUnderstandTestContext(t)
	data := make([]byte, maxImageUnderstandBytes/2+1)
	copy(data, testPNG)
	second := filepath.Join(filepath.Dir(first), "second.png")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	tool := NewImageUnderstandTool([]config.ImageUnderstandConfig{{Name: "accurate", Model: "glm-5.3-flash"}})
	_, err := tool.Execute(ctx, map[string]any{"paths": []string{first, second}, "mode": "accurate"})
	if err == nil || !strings.Contains(err.Error(), "combined images") {
		t.Fatalf("expected batch size rejection, got %v", err)
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
