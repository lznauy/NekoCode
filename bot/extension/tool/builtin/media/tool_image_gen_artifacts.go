package media

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"nekocode/bot/extension/tool/runtime/authority"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (t *ImageGenTool) downloadImages(ctx context.Context, urls []string, dir string) (string, error) {
	var sb strings.Builder
	sb.WriteString("Generated images:\n")
	for i, imgURL := range urls {
		savePath := filepath.Join(dir, imageFilename(i, imageExtFromURL(imgURL)))
		if err := t.downloadFile(ctx, imgURL, savePath); err != nil {
			return "", fmt.Errorf("download image %d: %w", i+1, err)
		}
		fmt.Fprintf(&sb, "  %s\n  => %s\n", imgURL, savePath)
	}
	return sb.String(), nil
}

func (t *ImageGenTool) saveBase64Images(images []string, dir string) (string, error) {
	var sb strings.Builder
	sb.WriteString("Generated images:\n")
	for i, encoded := range images {
		data, err := base64.StdEncoding.DecodeString(stripDataURLPrefix(encoded))
		if err != nil {
			return "", fmt.Errorf("decode base64 image %d: %w", i+1, err)
		}
		savePath := filepath.Join(dir, imageFilename(i, imageExt(data)))
		if err := writeImageFile(savePath, func(f *os.File) error { _, err := f.Write(data); return err }); err != nil {
			return "", fmt.Errorf("save image %d: %w", i+1, err)
		}
		fmt.Fprintf(&sb, "  %s\n", savePath)
	}
	return sb.String(), nil
}

func imageFilename(idx int, ext string) string {
	return fmt.Sprintf("nekocode_img_%s_%d%s", time.Now().Format("20060102_150405"), idx+1, ext)
}

func imageExtFromURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err == nil {
		switch ext := strings.ToLower(filepath.Ext(parsed.Path)); ext {
		case ".png", ".jpg", ".jpeg", ".webp":
			return ext
		}
	}
	return ".jpg"
}

func stripDataURLPrefix(s string) string {
	if idx := strings.Index(s, ","); strings.HasPrefix(s, "data:") && idx >= 0 {
		return s[idx+1:]
	}
	return s
}

func imageExt(data []byte) string {
	switch http.DetectContentType(data) {
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".jpg"
	}
}

func (t *ImageGenTool) downloadFile(ctx context.Context, url, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return writeImageFile(path, func(f *os.File) error {
		_, err := io.Copy(f, io.LimitReader(resp.Body, 20<<20))
		return err
	})
}

// Stage writes beside the destination and replace it atomically. Existing
// symlinks are never opened for writing, including links outside the workspace.
func writeImageFile(path string, write func(*os.File) error) error {
	if err := authority.CheckWritable(path); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".nekocode-image-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	defer f.Close()
	if err := write(f); err != nil {
		return err
	}
	if err := f.Chmod(0644); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := authority.CheckWritable(path); err != nil {
		return err
	}
	return os.Rename(temp, path)
}
