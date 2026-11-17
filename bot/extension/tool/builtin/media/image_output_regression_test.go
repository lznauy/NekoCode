package media

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImageFilenamesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		name := imageFilename(0, ".png")
		if seen[name] {
			t.Fatalf("image name reused: %s", name)
		}
		seen[name] = true
	}
}
func TestPartialImageSavePreservesPaths(t *testing.T) {
	dir := t.TempDir()
	tool := NewImageGenTool(nil)
	output, err := tool.saveBase64Images([]string{base64.StdEncoding.EncodeToString(generatedPNG(t)), "invalid-base64"}, dir)
	files, _ := os.ReadDir(dir)
	if err == nil || len(files) != 1 || !strings.Contains(output, filepath.Join(dir, files[0].Name())) {
		t.Fatalf("partial success lost: %q %v files=%v", output, err, files)
	}
}

// downloadToFile fetches and validates one remote image, then writes it to an
// exact path — the composition the retired downloadFile helper performed.
func downloadToFile(ctx context.Context, tool *ImageGenTool, rawURL, path string) error {
	data, _, err := tool.downloadImageData(ctx, rawURL)
	if err != nil {
		return err
	}
	return writeImageFile(path, func(f *os.File) error { _, err := f.Write(data); return err })
}

func TestImageDownloadRejectsOversizeWithoutReplacingFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(w, io.LimitReader(repeatedImageByte{}, (20<<20)+1))
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "image.png")
	os.WriteFile(path, []byte("previous image"), 0600)
	err := downloadToFile(context.Background(), NewImageGenTool(nil), server.URL, path)
	data, readErr := os.ReadFile(path)
	if err == nil || readErr != nil || string(data) != "previous image" {
		t.Fatalf("oversize download committed: err=%v size=%d readErr=%v", err, len(data), readErr)
	}
}

type repeatedImageByte struct{}

func (repeatedImageByte) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

func TestPartialImageDownloadPreservesPaths(t *testing.T) {
	image := generatedPNG(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad" {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Write(image)
	}))
	defer server.Close()
	dir := t.TempDir()
	output, err := NewImageGenTool(nil).downloadImages(context.Background(), []string{server.URL + "/good", server.URL + "/bad"}, dir)
	files, _ := os.ReadDir(dir)
	if err == nil || len(files) != 1 || !strings.Contains(output, filepath.Join(dir, files[0].Name())) {
		t.Fatalf("partial download lost: %q %v files=%v", output, err, files)
	}
}

func TestImageDownloadAcceptsExactLimit(t *testing.T) {
	image := generatedPNG(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(image)
		_, _ = io.Copy(w, io.LimitReader(repeatedImageByte{}, (20<<20)-int64(len(image))))
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "image.png")
	if err := downloadToFile(context.Background(), NewImageGenTool(nil), server.URL, path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() != 20<<20 {
		t.Fatalf("wrong download size: %v %v", info, err)
	}
}
