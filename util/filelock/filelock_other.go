//go:build !unix && !windows

package filelock

import (
	"errors"
	"os"
)

func lock(*os.File) error {
	return errors.New("file locking is unsupported on this platform")
}

// TryLock attempts an exclusive lock on an open file. Contention returns false
// without waiting; closing the file releases an acquired lock.
func TryLock(*os.File) (bool, error) {
	return false, errors.New("file locking is unsupported on this platform")
}
