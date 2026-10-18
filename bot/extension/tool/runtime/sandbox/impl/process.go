package impl

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"time"
)

// Process is a started sandbox or host command.
type Process struct {
	cmd      *exec.Cmd
	cancel   context.CancelFunc
	stopKill func() bool

	stdout  io.ReadCloser
	stderr  io.ReadCloser
	stdoutW io.Closer
	stderrW io.Closer
	done    chan struct{}

	doneOnce    sync.Once
	cleanupOnce sync.Once
	cleanup     func()
}

func (p *Process) Stdout() io.ReadCloser {
	if p == nil {
		return nil
	}
	return p.stdout
}

func (p *Process) Stderr() io.ReadCloser {
	if p == nil {
		return nil
	}
	return p.stderr
}

func (p *Process) Wait() error {
	if p == nil || p.cmd == nil {
		return fmt.Errorf("process not started")
	}
	// cmd.Stdout/Stderr are io.Writers, so Wait blocks until the process
	// exits AND all output has been copied to the pipes — no final output
	// can be lost (unlike StdoutPipe, where Wait may close the pipe while
	// a reader still has unread data in flight).
	err := p.cmd.Wait()
	// Signal EOF to the drain readers only after the copy completed.
	_ = p.stdoutW.Close()
	_ = p.stderrW.Close()
	p.doneOnce.Do(func() { close(p.done) })
	p.finish()
	return err
}

func (p *Process) Terminate(grace time.Duration) error {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return fmt.Errorf("process not started")
	}
	pid := p.cmd.Process.Pid
	if runtime.GOOS == "windows" {
		if err := p.cmd.Process.Kill(); err != nil {
			return err
		}
		p.finish()
		return nil
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	deadline := time.NewTimer(grace)
	defer deadline.Stop()
	for {
		select {
		case <-p.done:
			p.finish()
			return nil
		case <-deadline.C:
			err := syscall.Kill(-pid, syscall.SIGKILL)
			p.finish()
			return err
		}
	}
}

func (p *Process) finish() {
	if p == nil {
		return
	}
	if p.stopKill != nil {
		p.stopKill()
	}
	if p.cancel != nil {
		p.cancel()
	}
	p.cleanupOnce.Do(func() {
		if p.cleanup != nil {
			p.cleanup()
		}
	})
}

func startCommand(ctx context.Context, name string, args []string, env []string, dir string, sysproc *syscall.SysProcAttr, cleanup func()) (*Process, error) {
	cmdCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(cmdCtx, name, args...)
	cmd.Env = env
	cmd.Dir = dir
	cmd.SysProcAttr = sysproc

	// Wire output through io.Pipe writers: exec copies process output into
	// them and Wait() blocks until the copy completes, so the final bytes
	// written just before exit are never lost (StdoutPipe would let Wait
	// close the pipe ahead of the drain readers — a documented misuse).
	stdoutR, stdoutW := io.Pipe()
	stderrR, stderrW := io.Pipe()
	cmd.Stdout = stdoutW
	cmd.Stderr = stderrW

	if err := cmd.Start(); err != nil {
		cancel()
		if cleanup != nil {
			cleanup()
		}
		return nil, err
	}
	p := &Process{
		cmd: cmd, cancel: cancel,
		stdout: stdoutR, stderr: stderrR, stdoutW: stdoutW, stderrW: stderrW,
		done: make(chan struct{}), cleanup: cleanup,
	}
	p.stopKill = context.AfterFunc(cmdCtx, func() {
		if cmd.Process != nil && runtime.GOOS != "windows" {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	})
	return p, nil
}

// StartHost launches command on the host with NO sandbox isolation.
func StartHost(ctx context.Context, command string) (*Process, error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return StartHostIn(ctx, command, dir)
}

// StartHostIn executes in an explicitly owned workspace without process chdir.
func StartHostIn(ctx context.Context, command, dir string) (*Process, error) {
	sysproc := &syscall.SysProcAttr{}
	if runtime.GOOS != "windows" {
		sysproc.Setpgid = true
	}
	return startCommand(ctx, "bash", []string{"-c", command}, nil, dir, sysproc, nil)
}
