//go:build unix && !aix

package core

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
)

func tryIndexLock(file *os.File, exclusive bool) (bool, error) {
	flags := unix.LOCK_SH | unix.LOCK_NB
	if exclusive {
		flags = unix.LOCK_EX | unix.LOCK_NB
	}
	err := unix.Flock(int(file.Fd()), flags)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return false, nil
	}
	return err == nil, err
}

func shareIndexLock(file *os.File) (bool, error) { return tryIndexLock(file, false) }
