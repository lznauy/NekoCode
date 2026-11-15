//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package mcp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestFailedTreeKillIsReportedAndCanBeRetried(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	configureProcess(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = killProcessTree(cmd.Process); _ = cmd.Wait() }()
	c := &client{cmd: cmd, killTree: func(*os.Process) error { return syscall.EPERM }}
	err := c.stopLocked(20 * time.Millisecond)
	if !errors.Is(err, syscall.EPERM) {
		t.Fatalf("tree cleanup failure swallowed: %v", err)
	}
	if c.cmd == nil {
		t.Fatal("failed cleanup discarded process ownership")
	}
	if err := c.Start(t.Context()); !errors.Is(err, syscall.EPERM) {
		t.Fatalf("incomplete cleanup admitted a replacement: %v", err)
	}
	c.killTree = killProcessTree
	if err := c.Close(); err != nil {
		t.Fatalf("cleanup retry failed: %v", err)
	}
	if c.cmd != nil {
		t.Fatal("successful cleanup retained process")
	}
}

func TestCancellationPreservesTreeCleanupFailure(t *testing.T) {
	for _, read := range []bool{false, true} {
		t.Run(fmt.Sprint(read), func(t *testing.T) {
			cmd := exec.Command("sleep", "30")
			configureProcess(cmd)
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			c := &client{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout), stdoutPipe: stdout, killTree: func(*os.Process) error { return syscall.EPERM }}
			defer func() { c.killTree = killProcessTree; _ = c.Close() }()
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer cancel()
			if read {
				_, err = c.sendRequest(ctx, "blocked", nil)
			} else {
				err = c.writeMessage(ctx, make(chan time.Time), "blocked", time.Second, []byte(strings.Repeat("x", 1<<20)))
			}
			if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, syscall.EPERM) {
				t.Fatalf("cancellation or cleanup cause lost: %v", err)
			}
			if c.cmd == nil || !c.stopping {
				t.Fatal("incomplete process cleanup was forgotten")
			}
		})
	}
}

func TestMCPAlreadyExitedProcessClosesCleanly(t *testing.T) {
	cmd := exec.Command("go", "version")
	configureProcess(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if _, err = io.ReadAll(stdout); err != nil {
		t.Fatal(err)
	}
	c := &client{cmd: cmd, stdoutPipe: stdout}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
}
