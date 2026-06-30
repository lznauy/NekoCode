package session

import (
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"strings"
)

// readImageDims reads width/height from an image file header.
// Returns nil if the file cannot be opened or decoded.
func readImageDims(path string) []int {
	ext := strings.ToLower(path[strings.LastIndexByte(path, '.'):])
	// webp is not supported by stdlib image.DecodeConfig, skip it.
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".gif" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return nil
	}
	return []int{cfg.Width, cfg.Height}
}
