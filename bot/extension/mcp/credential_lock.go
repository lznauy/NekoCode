package mcp

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

// Keep the lock inode separate from atomically replaced credential files.
// Every process coordinates refresh, replacement and logout on this file.
func withCredentialLock(ctx context.Context, path string, fn func() error) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		locked, err := tryCredentialLock(file)
		if err != nil {
			return err
		}
		if locked {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	defer unlockCredential(file)
	return fn()
}
