package read

import (
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	utilfs "nekocode/util/fs"
	"os"
	"path/filepath"
)

func (t *ReadTool) readImage(ctx context.Context, path string) (string, error) {
	f, err := utilfs.OpenRegularFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to open image: %w", err)
	}
	defer func() { _ = f.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = f.Close() })
	defer stop()
	cfg, format, err := image.DecodeConfig(io.LimitReader(f, maxReadFileBytes))
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
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
	if !st.Mode().IsRegular() {
		return "", utilfs.ErrNotRegular
	}
	return fmt.Sprintf("[PDF] %s — %.1fKB. Use pdftotext to extract content.",
		filepath.Base(path), float64(st.Size())/1024), nil
}
