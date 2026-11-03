package permission

import (
	"fmt"
	"nekocode/bot/extension/tool/runtime/authority"
	"os"
	"path/filepath"
)

// The sidecar is never renamed or removed: locking the JSON itself would leave
// concurrent writers holding different inodes after the first atomic save.
func lockStore(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(filepath.Dir(path), "permissions.lock")
	resolved, err := authority.CanonicalPath(lockPath)
	if err != nil {
		return nil, err
	}
	if resolved != lockPath {
		return nil, fmt.Errorf("permission store lock must not be a symbolic link")
	}
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := lockStoreFile(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("lock permission store: %w", err)
	}
	return func() { _ = file.Close() }, nil
}
