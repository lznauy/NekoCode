package read

import (
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
)

func (t *ReadTool) readImage(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open image: %w", err)
	}
	defer f.Close()
	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("[Image] %s — %s, %dx%d", filepath.Base(path), format, cfg.Width, cfg.Height), nil
}

func (t *ReadTool) readPDF(path string) (string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("[PDF] %s — %.1fKB. Use pdftotext to extract content.",
		filepath.Base(path), float64(st.Size())/1024), nil
}
