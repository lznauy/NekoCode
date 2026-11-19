//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris || windows

package media

import (
	"os"

	utilfs "nekocode/util/fs"
)

func openImageFile(path string) (*os.File, error) { return utilfs.OpenRegularNoFollow(path) }
