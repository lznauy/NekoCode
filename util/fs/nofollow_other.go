//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package fs

import (
	"errors"
	"os"
)

func OpenRegularNoFollow(path string) (*os.File, error) { return nil, errors.ErrUnsupported }
