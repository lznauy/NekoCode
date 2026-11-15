package mcp

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// stopLocked closes stdio, terminates the process tree and joins the writer.
// Failed cleanup keeps ownership and blocks Start until a retry succeeds.
// The caller must hold c.mu.
func (c *client) stopLocked(timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 100 * time.Millisecond
	}
	c.stopping = true
	var failures []error
	if c.stdin != nil {
		if err := c.stdin.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			failures = append(failures, fmt.Errorf("close MCP stdin: %w", err))
		}
	}
	hasProcess := c.cmd != nil && c.cmd.Process != nil
	if hasProcess && !c.treeStopped {
		kill := c.killTree
		if kill == nil {
			kill = killProcessTree
		}
		if err := kill(c.cmd.Process); err != nil {
			failures = append(failures, fmt.Errorf("kill MCP process tree: %w", err))
			if err := c.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				failures = append(failures, fmt.Errorf("kill MCP server: %w", err))
			}
			// Do not reap on tree-kill failure. Holding the original child prevents
			// its PID from being reused before a later process-group cleanup retry.
		} else {
			c.treeStopped = true
		}
	}
	if c.stdoutPipe != nil {
		if err := c.stdoutPipe.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			failures = append(failures, fmt.Errorf("close MCP stdout: %w", err))
		}
	}
	if c.writePending != nil {
		timer := time.NewTimer(timeout)
		select {
		case <-c.writePending:
			c.writePending = nil
		case <-timer.C:
			failures = append(failures, fmt.Errorf("MCP stdin writer did not stop within %s", timeout))
		}
		timer.Stop()
	}
	if c.readPending != nil {
		timer := time.NewTimer(timeout)
		select {
		case <-c.readPending:
			c.readPending = nil
		case <-timer.C:
			failures = append(failures, fmt.Errorf("MCP stdout reader did not stop within %s", timeout))
		}
		timer.Stop()
	}
	if hasProcess && c.treeStopped {
		if c.wait == nil {
			result := &processWait{done: make(chan struct{})}
			cmd := c.cmd
			c.wait = result
			go func() { result.err = cmd.Wait(); close(result.done) }()
		}
		if !waitForProcess(c.wait, timeout) {
			if err := c.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				failures = append(failures, fmt.Errorf("kill MCP server: %w", err))
			}
			if !waitForProcess(c.wait, timeout) {
				failures = append(failures, fmt.Errorf("MCP server did not exit within %s after forced termination", 2*timeout))
			}
		}
		select {
		case <-c.wait.done:
			var exit *exec.ExitError
			if err := c.wait.err; err != nil && !errors.As(err, &exit) {
				failures = append(failures, fmt.Errorf("wait for MCP server: %w", err))
			}
		default:
		}
	}
	if err := errors.Join(failures...); err != nil {
		return err
	}
	c.cmd, c.stdin, c.stdout, c.stdoutPipe = nil, nil, nil, nil
	c.wait = nil
	c.treeStopped, c.stopping = false, false
	return nil
}

func waitForProcess(result *processWait, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-result.done:
		return true
	case <-timer.C:
		return false
	}
}
