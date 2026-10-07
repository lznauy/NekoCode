package anthropic

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"nekocode/bot/provider/types"
)

func TestVisionUserMessageBecomesImageBlocks(t *testing.T) {
	png := []byte{
		0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0x0d, 'I', 'H', 'D', 'R',
		0, 0, 0, 1, 0, 0, 0, 1, 8, 2, 0, 0, 0, 0x90, 0x77, 0x53, 0xde,
		0, 0, 0, 0x0c, 'I', 'D', 'A', 'T', 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0, 0,
		0x03, 0x01, 0x01, 0x1a, 0xdd, 0x8d, 0xb0,
		0, 0, 0, 0, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
	}
	path := filepath.Join(t.TempDir(), "img.png")
	if err := os.WriteFile(path, png, 0o600); err != nil {
		t.Fatal(err)
	}

	c := New("", "", "test")
	c.SetVision(true)
	got, _ := c.toMessages([]types.Message{{
		Role:    "user",
		Content: "look [Image #1]",
		Images:  []types.MessageImage{{Path: path, MIME: "image/png", Width: 1, Height: 1}},
	}}, types.ReasoningSettings{})

	if len(got) != 1 {
		t.Fatalf("messages = %d", len(got))
	}
	blocks, ok := got[0].Content.([]contentBlock)
	if !ok {
		t.Fatalf("vision user content = %#v, want content blocks", got[0].Content)
	}
	if len(blocks) != 2 {
		t.Fatalf("blocks = %+v", blocks)
	}
	// Images precede the text block per Anthropic convention.
	if blocks[0].Type != "image" || blocks[0].Source == nil ||
		blocks[0].Source.Type != "base64" || blocks[0].Source.MediaType != "image/png" ||
		blocks[0].Source.Data != base64.StdEncoding.EncodeToString(png) {
		t.Fatalf("image block = %+v", blocks[0])
	}
	if blocks[1].Type != "text" || blocks[1].Text != "look [Image #1]" {
		t.Fatalf("text block = %+v", blocks[1])
	}
}

func TestVisionDisabledStripsImages(t *testing.T) {
	c := New("", "", "test")
	c.SetVision(false)
	got, _ := c.toMessages([]types.Message{{
		Role:    "user",
		Content: "look [Image #1]",
		Images:  []types.MessageImage{{Path: "/nonexistent/img.png"}},
	}}, types.ReasoningSettings{})

	if len(got) != 1 {
		t.Fatalf("messages = %d", len(got))
	}
	if content, ok := got[0].Content.(string); !ok || content != "look [Image #1]" {
		t.Fatalf("non-vision content = %#v, want plain text", got[0].Content)
	}
}
