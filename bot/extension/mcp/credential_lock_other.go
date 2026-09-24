//go:build !unix && !windows

package mcp

import (
	"fmt"
	"os"
)

func tryCredentialLock(*os.File) (bool, error) {
	return false, fmt.Errorf("MCP OAuth credential locking is unsupported on this platform")
}
func unlockCredential(*os.File) {}
