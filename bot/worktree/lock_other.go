//go:build !unix && !windows

package worktree

import (
	"fmt"
	"os"
)

func tryLock(*os.File) (bool, error) {
	return false, fmt.Errorf("worktree locking is unsupported on this platform")
}
