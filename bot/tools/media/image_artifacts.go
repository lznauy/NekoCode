package media

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
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
		if err := os.WriteFile(savePath, data, 0o644); err != nil {
			return "", fmt.Errorf("save image %d: %w", i+1, err)
		}
		fmt.Fprintf(&sb, "  %s\n", savePath)
	}
	return sb.String(), nil
}

func imageFilename(idx int, ext string) string {
	return fmt.Sprintf("nekocode_img_%s_%d%s", time.Now().Format("20060102_150405"), idx+1, ext)
}

func imageExtFromURL(imgURL string) string {
	ext := ".jpg"
	if idx := strings.LastIndex(imgURL, "."); idx != -1 {
		if e := strings.ToLower(imgURL[idx:]); strings.HasPrefix(e, ".png") || strings.HasPrefix(e, ".jpg") || strings.HasPrefix(e, ".jpeg") || strings.HasPrefix(e, ".webp") {
			if end := strings.Index(imgURL[idx:], "?"); end != -1 {
				return imgURL[idx : idx+end]
			}
			return imgURL[idx:]
		}
	}
	return ext
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
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, io.LimitReader(resp.Body, 20<<20))
	return err
}
