//go:build !windows

package impl

import (
	"os"
	"syscall"
)

func hostProcessAttributes() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

func signalProcessTree(process *os.Process, force bool) error {
	signal := syscall.SIGTERM
	if force {
		signal = syscall.SIGKILL
	}
	return syscall.Kill(-process.Pid, signal)
}
