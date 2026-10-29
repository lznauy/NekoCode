//go:build windows

package core

import (
	"errors"
	"golang.org/x/sys/windows"
	"os"
)

func tryIndexLock(file *os.File, exclusive bool) (bool, error) {
	flags := uint32(windows.LOCKFILE_FAIL_IMMEDIATELY)
	if exclusive {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	err := windows.LockFileEx(windows.Handle(file.Fd()), flags, 0, 1, 0, &windows.Overlapped{})
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}
	return err == nil, err
}

// The exclusive rebuild gate remains held throughout this non-atomic
// conversion, so no other Manager can open or move the database in between.
func shareIndexLock(file *os.File) (bool, error) {
	if err := windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &windows.Overlapped{}); err != nil {
		return false, err
	}
	return tryIndexLock(file, false)
}
