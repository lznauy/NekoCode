//go:build unix

package filelock

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func lock(file *os.File) error {
	return flock(file, unix.LOCK_EX)
}

func flock(file *os.File, how int) error {
	for {
		err := unix.Flock(int(file.Fd()), how)
		if !errors.Is(err, unix.EINTR) {
			return err
		}
	}
}

// TryLock attempts an exclusive lock on an open file. Contention returns false
// without waiting; closing the file releases an acquired lock.
func TryLock(f *os.File) (bool, error) {
	err := flock(f, unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return false, nil
	}
	return err == nil, err
}
