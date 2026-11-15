//go:build windows

// Package processtree provides platform process-tree termination shared by
// execution hosts. A successful parent kill never hides a failed tree kill.
package processtree

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

// Kill attempts to terminate a Windows process tree. A failed tree kill stays
// an error even if the parent has already exited; descendants may still live.
func Kill(process *os.Process) error {
	// taskkill /T terminates descendants as well as the direct process.
	if err := exec.Command("taskkill", "/PID", strconv.Itoa(process.Pid), "/T", "/F").Run(); err != nil {
		killErr := process.Kill()
		return errors.Join(fmt.Errorf("taskkill process tree: %w", err), killErr)
	}
	return nil
}
