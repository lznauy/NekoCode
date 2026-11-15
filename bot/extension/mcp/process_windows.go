//go:build windows

package mcp

import (
	"nekocode/util/processtree"
	"os"
	"os/exec"
	"syscall"
)

func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}
func killProcessTree(process *os.Process) error { return processtree.Kill(process) }
