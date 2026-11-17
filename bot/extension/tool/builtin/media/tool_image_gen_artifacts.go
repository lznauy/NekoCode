package media

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	_ "golang.org/x/image/webp"
	"image"
	_ "image/gif"
	"io"
	"nekocode/bot/extension/tool/runtime/authority"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxGeneratedImageBytes = 20 << 20
const maxGeneratedImagePixels = 25_000_000

func (t *ImageGenTool) downloadImages(ctx context.Context, urls []string, dir string) (string, error) {
	var sb strings.Builder
	for i, imgURL := range urls {
		data, ext, err := t.downloadImageData(ctx, imgURL)
		if err != nil {
			return sb.String(), fmt.Errorf("download image %d: %w; reuse any files already saved in the output before retrying generation", i+1, err)
		}
		savePath, err := saveGeneratedImage(dir, i, data, ext)
		if err != nil {
			return sb.String(), err
		}
		if i == 0 {
			sb.WriteString("Generated images:\n")
		}
		fmt.Fprintf(&sb, "  %s\n  => %s\n", imgURL, savePath)
	}
	return sb.String(), nil
}

func (t *ImageGenTool) saveBase64Images(images []string, dir string) (string, error) {
	var sb strings.Builder
	for i, encoded := range images {
		encoded = stripDataURLPrefix(encoded)
		if len(encoded) > base64.StdEncoding.EncodedLen(maxGeneratedImageBytes) {
			return sb.String(), fmt.Errorf("image %d exceeds the 20 MiB limit", i+1)
		}
		data, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return sb.String(), fmt.Errorf("decode base64 image %d: %w; reuse any files already saved in the output before retrying generation", i+1, err)
		}
		ext, err := validateGeneratedImage(data)
		if err != nil {
			return sb.String(), fmt.Errorf("validate image %d: %w", i+1, err)
		}
		savePath, err := saveGeneratedImage(dir, i, data, ext)
		if err != nil {
			return sb.String(), fmt.Errorf("%w; reuse any files already saved in the output before retrying generation", err)
		}
		if i == 0 {
			sb.WriteString("Generated images:\n")
		}
		fmt.Fprintf(&sb, "  %s\n", savePath)
	}
	return sb.String(), nil
}

// saveGeneratedImage writes one validated image and returns its path.
func saveGeneratedImage(dir string, idx int, data []byte, ext string) (string, error) {
	savePath := filepath.Join(dir, imageFilename(idx, ext))
	if err := writeImageFile(savePath, func(f *os.File) error { _, err := f.Write(data); return err }); err != nil {
		return "", fmt.Errorf("save image %d: %w", idx+1, err)
	}
	return savePath, nil
}

func imageFilename(idx int, ext string) string {
	return fmt.Sprintf("nekocode_img_%s_%s_%d%s", time.Now().Format("20060102_150405"), rand.Text(), idx+1, ext)
}

func stripDataURLPrefix(s string) string {
	if idx := strings.Index(s, ","); strings.HasPrefix(s, "data:") && idx >= 0 {
		return s[idx+1:]
	}
	return s
}

func (t *ImageGenTool) downloadImageData(ctx context.Context, url string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxGeneratedImageBytes+1))
	if err != nil {
		return nil, "", err
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	ext, err := validateGeneratedImage(data)
	return data, ext, err
}

func validateGeneratedDimensions(width, height int) error {
	// Divide instead of multiplying model-supplied integers, avoiding overflow.
	if width <= 0 || height <= 0 || width > 32768 || height > 32768 || width > maxGeneratedImagePixels/height {
		return fmt.Errorf("image dimensions must be positive, at most 32768 per side and 25 million pixels total")
	}
	return nil
}

func validateGeneratedImage(data []byte) (string, error) {
	if len(data) > maxGeneratedImageBytes {
		return "", fmt.Errorf("image exceeds the 20 MiB limit; no file was committed")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("response is not a supported image: %w", err)
	}
	if err := validateGeneratedDimensions(cfg.Width, cfg.Height); err != nil {
		return "", err
	}
	ext := ""
	switch format {
	case "png":
		ext = ".png"
	case "jpeg":
		ext = ".jpg"
	case "gif":
		ext = ".gif"
	case "webp":
		ext = ".webp"
	default:
		return "", fmt.Errorf("unsupported generated image format %q", format)
	}
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		return "", fmt.Errorf("generated image is incomplete or invalid: %w", err)
	}
	return ext, nil
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
