//go:build unix

package permission

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
)

func lockStoreFile(file *os.File) error {
	for {
		err := unix.Flock(int(file.Fd()), unix.LOCK_EX)
		if !errors.Is(err, unix.EINTR) {
			return err
		}
	}
}
