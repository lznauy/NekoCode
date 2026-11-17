package media

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"nekocode/bot/config"
	"nekocode/bot/extension/tool/runtime/workspace"
)

func generatedPNG(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	if err := png.Encode(&output, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

type generationTransport func(*http.Request) (*http.Response, error)

func (f generationTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestGenerationRejectsInvalidArgumentsBeforeSubmission(t *testing.T) {
	for _, extra := range []map[string]any{
		{"width": 0, "height": 512}, {"width": -1, "height": 512},
		{"width": 512}, {"height": 512}, {"width": 1.5, "height": 512},
		{"width": 32769, "height": 1}, {"width": 10000, "height": 10000},
		{"seed": -2}, {"prompt": "   "}, {"prompt": strings.Repeat("中", 801)},
		{"output_dir": 42}, {"output_dir": nil}, {"output_dir": ""},
	} {
		dir := t.TempDir()
		tool := NewImageGenTool([]config.ImageGenConfig{{Name: "test", Provider: "jimeng", BaseURL: "https://example.invalid", APIKey: "fixture", SecretKey: "fixture"}})
		calls := 0
		tool.client.Transport = generationTransport(func(*http.Request) (*http.Response, error) {
			calls++
			body := `{"code":10000,"data":{"task_id":"unexpected","status":"done","binary_data_base64":["` + base64.StdEncoding.EncodeToString(generatedPNG(t)) + `"]}}`
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
		})
		args := map[string]any{"prompt": "generate", "output_dir": filepath.Join(dir, "new-output")}
		for key, value := range extra {
			args[key] = value
		}
		ctx := workspace.WithManager(t.Context(), workspace.New(dir, nil))
		if _, err := tool.Execute(ctx, args); err == nil || calls != 0 {
			t.Errorf("invalid arguments reached provider: %#v calls=%d err=%v", extra, calls, err)
		}
		if _, err := os.Stat(filepath.Join(dir, "new-output")); !os.IsNotExist(err) {
			t.Errorf("invalid args created output directory: %#v", extra)
		}
	}
}

func TestGenerationRejectsNonImageWithoutReplacingFile(t *testing.T) {
	for _, body := range [][]byte{nil, []byte("<html>login required</html>"), []byte(`{"error":"expired"}`), generatedPNG(t)[:40]} {
		tool := NewImageGenTool(nil)
		tool.client.Transport = generationTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/png"}}, Body: io.NopCloser(bytes.NewReader(body))}, nil
		})
		dir := t.TempDir()
		path := filepath.Join(dir, "existing.png")
		if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := downloadToFile(t.Context(), tool, "https://example.invalid/image.png", path); err == nil {
			t.Errorf("invalid image accepted: %q", body)
		}
		if data, _ := os.ReadFile(path); string(data) != "keep" {
			t.Error("invalid image replaced previous file")
		}
		out, err := tool.saveBase64Images([]string{base64.StdEncoding.EncodeToString(body)}, dir)
		if err == nil || out != "" {
			t.Errorf("invalid base64 image reported success: %q %v", out, err)
		}
	}
}

func TestGenerationUsesContentExtension(t *testing.T) {
	tool := NewImageGenTool(nil)
	data := generatedPNG(t)
	tool.client.Transport = generationTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/plain"}}, Body: io.NopCloser(bytes.NewReader(data))}, nil
	})
	dir := t.TempDir()
	if _, err := tool.downloadImages(t.Context(), []string{"https://example.invalid/image.jpg"}, dir); err != nil {
		t.Fatal(err)
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 1 || filepath.Ext(files[0].Name()) != ".png" {
		t.Fatalf("extension did not reflect actual image: %v", files)
	}
}

func TestGenerationModelEnum(t *testing.T) {
	tool := NewImageGenTool([]config.ImageGenConfig{{Name: "first"}, {Name: "second"}})
	for _, parameter := range tool.Parameters() {
		if parameter.Name == "model" {
			if !slices.Equal(parameter.Enum, []string{"first", "second"}) {
				t.Fatalf("missing model enum: %v", parameter.Enum)
			}
			return
		}
	}
	t.Fatal("model parameter missing")
}

func TestGenerationValidArgumentsPreserveRequestedValues(t *testing.T) {
	for _, extra := range []map[string]any{{}, {"width": 512, "height": 768, "seed": 0, "prompt": strings.Repeat("中", 800)}} {
		dir := t.TempDir()
		tool := NewImageGenTool([]config.ImageGenConfig{{Name: "test", Provider: "jimeng", BaseURL: "https://example.invalid", APIKey: "fixture", SecretKey: "fixture"}})
		var submitted map[string]any
		tool.client.Transport = generationTransport(func(req *http.Request) (*http.Response, error) {
			body := `{"code":10000,"data":{"status":"done","binary_data_base64":["` + base64.StdEncoding.EncodeToString(generatedPNG(t)) + `"]}}`
			if req.URL.Query().Get("Action") == "CVSync2AsyncSubmitTask" {
				if err := json.NewDecoder(req.Body).Decode(&submitted); err != nil {
					t.Fatal(err)
				}
				body = `{"code":10000,"data":{"task_id":"accepted"}}`
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
		})
		args := map[string]any{"prompt": "generate", "output_dir": dir}
		for key, value := range extra {
			args[key] = value
		}
		ctx := workspace.WithManager(t.Context(), workspace.New(dir, nil))
		if _, err := tool.Execute(ctx, args); err != nil {
			t.Fatal(err)
		}
		width, height, seed := 1328.0, 1328.0, -1.0
		if len(extra) > 0 {
			width, height, seed = 512, 768, 0
		}
		if submitted["width"] != width || submitted["height"] != height || submitted["seed"] != seed || submitted["prompt"] != args["prompt"] {
			t.Fatalf("request was altered: %v", submitted)
		}
	}
}

func TestGenerationInvalidDownloadCanResumeWithoutResubmitting(t *testing.T) {
	dir := t.TempDir()
	tool := NewImageGenTool([]config.ImageGenConfig{{Name: "test", Provider: "jimeng", BaseURL: "https://example.invalid", APIKey: "fixture", SecretKey: "fixture"}})
	submits, downloads := 0, 0
	tool.client.Transport = generationTransport(func(req *http.Request) (*http.Response, error) {
		body := []byte(`{"code":10000,"data":{"status":"done","image_urls":["https://example.invalid/image.png"]}}`)
		if req.URL.Query().Get("Action") == "CVSync2AsyncSubmitTask" {
			submits++
			body = []byte(`{"code":10000,"data":{"task_id":"accepted"}}`)
		}
		if req.Method == http.MethodGet {
			downloads++
			body = []byte("<html>login required</html>")
			if downloads > 1 {
				body = generatedPNG(t)
			}
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(body))}, nil
	})
	ctx := workspace.WithManager(t.Context(), workspace.New(dir, nil))
	out, err := tool.Execute(ctx, map[string]any{"prompt": "generate", "output_dir": dir})
	if err == nil {
		t.Fatal("invalid download reported success")
	}
	start := strings.Index(out.Text, `{"resume_id":`)
	if start < 0 {
		t.Fatalf("no recovery receipt: %q", out.Text)
	}
	end := start + strings.Index(out.Text[start:], "}") + 1
	var recovery map[string]any
	if err := json.Unmarshal([]byte(out.Text[start:end]), &recovery); err != nil {
		t.Fatal(err)
	}
	if files, _ := os.ReadDir(dir); len(files) != 0 {
		t.Fatalf("invalid image committed: %v", files)
	}
	if _, err := tool.Execute(ctx, recovery); err != nil {
		t.Fatal(err)
	}
	if submits != 1 || downloads != 2 {
		t.Fatalf("unexpected retry: submits=%d downloads=%d", submits, downloads)
	}
}
