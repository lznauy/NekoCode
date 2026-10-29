//go:build (!unix && !windows) || aix

package core

import (
	"fmt"
	"os"
)

func tryIndexLock(*os.File, bool) (bool, error) {
	return false, fmt.Errorf("project index locking is unsupported on this platform")
}

func shareIndexLock(file *os.File) (bool, error) { return tryIndexLock(file, false) }
