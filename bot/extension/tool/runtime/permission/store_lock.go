package permission

import (
	"fmt"
	"os"
	"path/filepath"

	"nekocode/bot/extension/tool/runtime/authority"
	"nekocode/util/filelock"
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
	unlock, err := filelock.Acquire(lockPath)
	if err != nil {
		return nil, fmt.Errorf("lock permission store: %w", err)
	}
	return unlock, nil
}
