package openai

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"nekocode/bot/provider/types"
)

// writeTestImage saves a tiny valid PNG and returns its path plus content.
func writeTestImage(t *testing.T, dir string) (string, []byte) {
	t.Helper()
	// 1x1 opaque PNG.
	png := []byte{
		0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0x0d, 'I', 'H', 'D', 'R',
		0, 0, 0, 1, 0, 0, 0, 1, 8, 2, 0, 0, 0, 0x90, 0x77, 0x53, 0xde,
		0, 0, 0, 0x0c, 'I', 'D', 'A', 'T', 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0, 0,
		0x03, 0x01, 0x01, 0x1a, 0xdd, 0x8d, 0xb0,
		0, 0, 0, 0, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
	}
	path := filepath.Join(dir, "img.png")
	if err := os.WriteFile(path, png, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, png
}

func TestVisionUserMessageBecomesContentParts(t *testing.T) {
	dir := t.TempDir()
	path, png := writeTestImage(t, dir)

	c := New("", "", "test")
	c.SetVision(true)
	got := c.toAPIMessages([]types.Message{{
		Role:    "user",
		Content: "look [Image #1]",
		Images:  []types.MessageImage{{Path: path, MIME: "image/png", Width: 1, Height: 1}},
	}}, types.ReasoningSettings{})

	if len(got) != 1 {
		t.Fatalf("messages = %d", len(got))
	}
	parts, ok := got[0].Content.([]contentPart)
	if !ok {
		t.Fatalf("vision user content = %#v, want content parts", got[0].Content)
	}
	if len(parts) != 2 || parts[0].Type != "text" || parts[0].Text != "look [Image #1]" {
		t.Fatalf("parts = %+v", parts)
	}
	if parts[1].Type != "image_url" || parts[1].ImageURL == nil {
		t.Fatalf("image part = %+v", parts[1])
	}
	want := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	if parts[1].ImageURL.URL != want {
		t.Fatalf("data URL mismatch: got %q", parts[1].ImageURL.URL)
	}
	// The wire form must serialize cleanly.
	if _, err := json.Marshal(got); err != nil {
		t.Fatal(err)
	}
}

func TestVisionDisabledStripsImages(t *testing.T) {
	dir := t.TempDir()
	path, _ := writeTestImage(t, dir)

	c := New("", "", "test")
	c.SetVision(false)
	got := c.toAPIMessages([]types.Message{{
		Role:    "user",
		Content: "look [Image #1]",
		Images:  []types.MessageImage{{Path: path}},
	}}, types.ReasoningSettings{})

	if len(got) != 1 {
		t.Fatalf("messages = %d", len(got))
	}
	if content, ok := got[0].Content.(string); !ok || content != "look [Image #1]" {
		t.Fatalf("non-vision content = %#v, want plain text", got[0].Content)
	}
}

func TestVisionSkipsMissingImageFiles(t *testing.T) {
	c := New("", "", "test")
	c.SetVision(true)
	got := c.toAPIMessages([]types.Message{{
		Role:    "user",
		Content: "look [Image #1]",
		Images:  []types.MessageImage{{Path: filepath.Join(t.TempDir(), "missing.png")}},
	}}, types.ReasoningSettings{})

	if content, ok := got[0].Content.(string); !ok || content != "look [Image #1]" {
		t.Fatalf("content with no readable images = %#v, want plain text fallback", got[0].Content)
	}
}
