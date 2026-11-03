//go:build !unix && !windows

package permission

import (
	"fmt"
	"os"
)

func lockStoreFile(*os.File) error {
	return fmt.Errorf("permission store locking is unsupported on this platform")
}
