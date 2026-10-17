package connect

import (
	"context"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	sharedhttp "nekocode/util/http"
)

// maxOutboundImages caps how many referenced images one outbound message
// carries, mirroring the runtime's inbound attachment limit.
const maxOutboundImages = 8

// imageTokenRe matches absolute paths of common image extensions as they
// appear in model output (markdown links, backticks, or bare text).
var imageTokenRe = regexp.MustCompile(`(/[^\s` + "`" + `'")\]]+\.(?:png|jpe?g|gif|webp|bmp|tiff?)\b)`)

// LocalImagePaths extracts local image file paths mentioned in text: every
// candidate path that exists as a regular file is kept (up to
// MaxImageAttachments). It powers outbound image delivery — a run result
// that references a generated image file sends the picture itself to
// channels that can render images; the path stays in the text for the rest.
func LocalImagePaths(text string) []string {
	if !strings.Contains(text, "/") {
		return nil
	}
	var paths []string
	seen := make(map[string]bool)
	for _, match := range imageTokenRe.FindAllStringSubmatch(text, -1) {
		path := match[1]
		if seen[path] {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		seen[path] = true
		paths = append(paths, path)
		if len(paths) >= maxOutboundImages {
			break
		}
	}
	return paths
}

// DownloadImage fetches a remote image URL (IM-platform CDN links) to dst.
// A browser-like User-Agent is set because some CDN hosts reject the default
// Go client signature. The timeout keeps a hanging CDN from stalling the
// inbound message loop that calls this synchronously.
func DownloadImage(ctx context.Context, rawURL, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; NekoCode/1.0)")
	client := &http.Client{Transport: sharedhttp.SharedTransport, Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return sharedhttp.NewHTTPError(resp.StatusCode, string(data))
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	_, err = io.Copy(out, resp.Body)
	return err
}

// DownloadToTemp saves the result of a download into a uniquely named
// temp file and returns its path. The file outlives the run — the model may
// re-read it later via the image_understand tool — so callers must not
// delete it on success.
func DownloadToTemp(pattern string, download func(dst string) error) (string, error) {
	tmp, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	path := tmp.Name()
	_ = tmp.Close()
	if err := download(path); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

// DownloadImageToTemp downloads a remote image URL to a temp file.
func DownloadImageToTemp(ctx context.Context, rawURL, pattern string) (string, error) {
	return DownloadToTemp(pattern, func(dst string) error {
		return DownloadImage(ctx, rawURL, dst)
	})
}

// AppendMarker appends an image marker to the user text. The first marker of
// a bare image message becomes the whole text; further markers (and markers
// on captioned messages) join on new lines. The marker must appear in Text
// for the runtime's attachment envelope to accept the image.
func AppendMarker(text, marker string) string {
	if text == "" {
		return marker
	}
	return text + "\n" + marker
}
