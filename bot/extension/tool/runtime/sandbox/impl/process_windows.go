//go:build windows

package impl

import (
	"nekocode/util/processtree"
	"os"
	"syscall"
)

func hostProcessAttributes() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

func signalProcessTree(process *os.Process, _ bool) error { return processtree.Kill(process) }
