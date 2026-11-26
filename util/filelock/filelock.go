// Package filelock coordinates local processes through stable lock files.
package filelock

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

// Acquire waits for exclusive access to path. Call the returned function to
// release the lock. The file must remain in place across releases so that all
// waiters lock the same file, including while application data is replaced.
func Acquire(path string) (func(), error) {
	return AcquireContext(context.Background(), path)
}

// AcquireContext stops waiting for the lock when ctx is cancelled.
func AcquireContext(ctx context.Context, path string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := wait(ctx, file); err != nil {
		file.Close()
		return nil, err
	}
	return func() { file.Close() }, nil
}

func wait(ctx context.Context, file *os.File) error {
	if ctx.Done() == nil {
		return lock(file)
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		locked, err := TryLock(file)
		if err != nil || locked {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}
