//go:build !unix && !windows

package session

import (
	"fmt"
	"os"
)

func trySubagentLock(*os.File) (bool, error) {
	return false, fmt.Errorf("subagent session locking is unsupported on this platform")
}
