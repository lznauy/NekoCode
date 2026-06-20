package mcp

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// Start launches the MCP server process and performs the initialize handshake.
func (c *Client) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cmd != nil {
		return nil
	}

	c.cmd = exec.Command(c.Config.Command, c.Config.Args...)
	c.cmd.Env = append(c.cmd.Env, os.Environ()...)
	for k, v := range c.Config.Env {
		c.cmd.Env = append(c.cmd.Env, k+"="+v)
	}

	var err error
	c.stdin, err = c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := c.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	c.stdout = bufio.NewReader(stdout)

	if err := c.cmd.Start(); err != nil {
		c.stdin.Close()
		return fmt.Errorf("start server: %w", err)
	}

	if err := c.initialize(); err != nil {
		c.stdin.Close()
		_ = c.cmd.Process.Kill()
		c.cmd = nil
		return fmt.Errorf("initialize: %w", err)
	}

	return nil
}

// Close stops the MCP server.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cmd == nil || c.cmd.Process == nil {
		return nil
	}

	if c.stdin != nil {
		_ = c.stdin.Close()
	}

	waitCh := make(chan error, 1)
	go func() { waitCh <- c.cmd.Wait() }()

	select {
	case <-waitCh:
	case <-time.After(2 * time.Second):
		_ = c.cmd.Process.Kill()
		<-waitCh
	}

	c.cmd = nil
	c.stdin = nil
	c.stdout = nil
	return nil
}
