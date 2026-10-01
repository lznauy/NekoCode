package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"time"

	"golang.design/x/clipboard"
)

const (
	clipboardHelperEnv        = "NEKOCODE_CLIPBOARD_HELPER"
	clipboardHelperImage      = "image"
	clipboardHelperText       = "text"
	clipboardHelperNoDataExit = 3
)

// RunClipboardHelper handles the isolated clipboard-reader subprocess used by
// the TUI. The parent caps stdout, so oversized clipboard owners cannot force
// the interactive process to materialize an unbounded payload.
func RunClipboardHelper() (bool, int) {
	mode := os.Getenv(clipboardHelperEnv)
	if mode == "" {
		return false, 0
	}
	debug.SetMemoryLimit(64 << 20)
	if err := clipboard.Init(); err != nil {
		_, _ = fmt.Fprint(os.Stderr, err)
		return true, 2
	}
	format := clipboard.FmtText
	if mode == clipboardHelperImage {
		format = clipboard.FmtImage
	} else if mode != clipboardHelperText {
		_, _ = fmt.Fprint(os.Stderr, "unsupported clipboard helper format")
		return true, 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	data, err := clipboard.Read(ctx, format)
	if errors.Is(err, clipboard.ErrNoData) || (err == nil && len(data) == 0) {
		return true, clipboardHelperNoDataExit
	}
	if err != nil {
		_, _ = fmt.Fprint(os.Stderr, err)
		return true, 2
	}
	if _, err := os.Stdout.Write(data); err != nil {
		_, _ = fmt.Fprint(os.Stderr, err)
		return true, 2
	}
	return true, 0
}
