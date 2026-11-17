package read

import (
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImageReadDoesNotClaimToDeliverPixels(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preview.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	f.Close()
	text, err := (&ReadTool{}).readImage(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "pixels not delivered") || !strings.Contains(text, "image_understand") || !strings.Contains(text, "2x3") {
		t.Fatal(text)
	}
	text, err = (&ReadTool{ImageUnderstandingUnavailable: true}).readImage(context.Background(), path)
	if err != nil || strings.Contains(text, "image_understand") || !strings.Contains(text, "Visual inspection is unavailable") || !strings.Contains(text, "2x3") {
		t.Fatalf("unconfigured image guidance: %q %v", text, err)
	}
}
