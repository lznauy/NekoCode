//go:build !unix

package plugin

import "os/exec"

// CommandContext still cancels the direct process on unsupported platforms.
func setupHookProcess(cmd *exec.Cmd) {}

func finishHookProcess(cmd *exec.Cmd) {}
