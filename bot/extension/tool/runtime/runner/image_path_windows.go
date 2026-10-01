//go:build windows

package runner

import (
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func validateLocalImagePath(path string) error {
	trimmed := strings.TrimSpace(path)
	lower := strings.ToLower(strings.ReplaceAll(trimmed, "/", `\`))
	if strings.HasPrefix(lower, `\\`) || strings.HasPrefix(lower, `\??\`) {
		return fmt.Errorf("image path must be on a local filesystem")
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return fmt.Errorf("resolve image path: %w", err)
	}
	volume := filepath.VolumeName(abs)
	if len(volume) == 2 && volume[1] == ':' {
		root, err := windows.UTF16PtrFromString(volume + `\`)
		if err != nil {
			return fmt.Errorf("resolve image volume: %w", err)
		}
		if windows.GetDriveType(root) == windows.DRIVE_REMOTE {
			return fmt.Errorf("image path must not use a mapped network drive")
		}
	}
	return nil
}
