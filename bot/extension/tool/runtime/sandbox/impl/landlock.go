//go:build linux

package impl

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	tbsb "github.com/tirdyhouse/sandbox"
)

// Landlock fallback backend.
//
// We import github.com/tirdyhouse/sandbox for its self-exec Landlock helper:
// when a child is spawned with __SANDBOX_HELPER=1 (and a JSON config in
// __SANDBOX_CONFIG), tbsb's init() — registered simply by importing the
// package — sets up Landlock restrictions and exec's the real command.
//
// We do NOT call tbsb.Command() because its Cmd does not expose the process
// handle, so we could not enforce timeout/kill. Instead we replicate its
// applySandbox (env + argv layout) on top of exec.CommandContext, keeping
// full timeout/process-group control, while still letting tbsb's init() do
// the Landlock enforcement in the child.
//
// Known tbsb bug worked around here: setupLandlock omits prctl(PR_SET_NO_NEW_PRIVS),
// so landlock_restrict_self returns EPERM unless no_new_privs is already set
// on the child process. no_new_privs is a per-thread attribute inherited only
// via clone(2), and os/exec forks the child from whatever OS thread the
// calling goroutine happens to be scheduled on. Setting it once on the parent
// process is therefore NOT sufficient: other runtime threads (GC, sysmon,
// timers) never get the flag, and a child forked from one of those threads
// lacks no_new_privs and hits EPERM. The fix is to set PR_SET_NO_NEW_PRIVS on
// the exact OS thread that performs the fork, immediately before spawning —
// see withNoNewPrivs.

// withNoNewPrivs runs fn on a goroutine locked to the current OS thread,
// after setting PR_SET_NO_NEW_PRIVS on that thread. The lock pins the
// goroutine (and therefore the fork inside fn) to a thread known to carry the
// no_new_privs flag, which the child then inherits across clone(2). The flag
// is idempotent and harmless to set repeatedly; pinning per-spawn keeps
// concurrency (different callers land on different threads) while guaranteeing
// correctness regardless of which thread the goroutine started on.
//
// Calls may also be nested (e.g. the probe re-enters via runLandlockBash); the
// lock is recursive-safe because Go counts nested LockOSThread calls.
func withNoNewPrivs(fn func() error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	const prSetNoNewPrivs = 38 // PR_SET_NO_NEW_PRIVS
	_, _, _ = syscall.Syscall(syscall.SYS_PRCTL, prSetNoNewPrivs, 1, 0)
	return fn()
}

// landlockAvailable reports whether the tbsb Landlock backend is usable.
//
// It performs a one-time self-test so that kernels which report Landlock
// support but block enforcement at runtime (common in containers / CI where
// landlock_restrict_self returns EPERM) are treated as unavailable. Without
// this, callers would attempt the backend, fail to enforce, and surface a
// confusing "landlock restrict failed" error instead of falling back.
func landlockAvailable() bool {
	if !tbsb.Available() {
		return false
	}
	landlockProbeOnce.Do(func() {
		landlockProbeOK = landlockSelfTest()
	})
	return landlockProbeOK
}

var (
	landlockProbeOnce sync.Once
	landlockProbeOK   bool
)

// landlockSelfTest runs a trivial command through the Landlock backend to
// verify that enforcement actually works at runtime. The kernel may report
// Landlock as available (ABI version > 0) while the container/runtime blocks
// landlock_restrict_self (EPERM); in that case the backend is unusable even
// though tbsb.Available() returns true.
func landlockSelfTest() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ws, err := os.MkdirTemp("", "landlock-probe-")
	if err != nil {
		return false
	}
	defer func() { _ = os.RemoveAll(ws) }()
	_, err = runLandlockBash(ctx, "echo ok", Profile{Workspace: ws}, 10*time.Second)
	return err == nil
}

// The helper and shell inherit the pinned directory at fd 3. Accessing a
// parent process's /proc/PID/fd may be denied by the kernel's ptrace policy.
func landlockScratch(profile Profile) (Profile, []*os.File) {
	if profile.ScratchFile == nil {
		return profile, nil
	}
	files := []*os.File{profile.ScratchFile}
	profile.ScratchFile = nil
	profile.ScratchDir = "/proc/self/fd/3"
	return profile, files
}

// runLandlockBash runs command under the tbsb Landlock backend.
//
// Writable paths are the workspace plus allowed cache paths; everything else
// is read-only. Network is NOT isolated by this backend (Landlock only
// restricts file paths on ABI <4; TCP restrictions need kernel 6.7+ and are
// not applied here). Callers that need network isolation should prefer the
// native namespace backend (runNativeBash); this is the fallback when user
// namespaces are unavailable.
func runLandlockBash(ctx context.Context, command string, profile Profile, timeout time.Duration) (string, error) {
	if profile.Workspace == "" {
		return "", fmt.Errorf("sandbox workspace is required")
	}
	ws, err := filepath.Abs(profile.Workspace)
	if err != nil {
		return "", fmt.Errorf("resolve workspace: %w", err)
	}

	var writable []string
	if profile.Mode != ModeReadOnly {
		writable = append(writable, ws)
	}
	writePaths, err := resolveWritePaths(profile.WritePaths)
	if err != nil {
		return "", err
	}
	writable = append(writable, writePaths...)
	profile, files := landlockScratch(profile)
	if len(files) != 0 {
		writable = append(writable, profile.ScratchDir)
	}

	// helperConfig mirrors tbsb's unexported helperConfig{WritableDirs `json:"w"`}.
	cfg := struct {
		WritableDirs []string `json:"w"`
	}{WritableDirs: writable}
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("marshal sandbox config: %w", err)
	}

	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("get executable path: %w", err)
	}

	// Replicate tbsb applySandbox argv: <self> __sandbox__ -- bash -c <command>
	args := []string{"__sandbox__", "--", "bash", "-c", command}
	env := append(commandEnvironment(os.Environ(), profile),
		"__SANDBOX_HELPER=1",
		"__SANDBOX_CONFIG="+string(cfgJSON),
	)
	sysproc := &syscall.SysProcAttr{Setpgid: true}

	// tbsb's init() prints "sandbox: landlock setup failed: ..." (with
	// restrict_self EPERM) when Landlock cannot be enforced. Match that
	// precisely so an EPERM from the command itself is not misclassified.
	unavail := func(stderr string, _ error) bool {
		return strings.Contains(stderr, "landlock setup failed")
	}

	// Spawn on a thread that carries PR_SET_NO_NEW_PRIVS so the child
	// inherits it (see withNoNewPrivs). Without this the tbsb helper child
	// hits landlock_restrict_self EPERM when forked from a Go runtime thread
	// lacking the flag.
	var out string
	spawnErr := withNoNewPrivs(func() error {
		var err error
		out, err = runChild(ctx, self, args, env, ws, timeout, sysproc, unavail, "landlock restrict failed", files)
		return err
	})
	return out, spawnErr
}

func startLandlockBash(ctx context.Context, command string, profile Profile) (*Process, error) {
	if profile.Workspace == "" {
		return nil, fmt.Errorf("sandbox workspace is required")
	}
	ws, err := filepath.Abs(profile.Workspace)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace: %w", err)
	}
	var writable []string
	if profile.Mode != ModeReadOnly {
		writable = append(writable, ws)
	}
	writePaths, err := resolveWritePaths(profile.WritePaths)
	if err != nil {
		return nil, err
	}
	writable = append(writable, writePaths...)
	profile, files := landlockScratch(profile)
	if len(files) != 0 {
		writable = append(writable, profile.ScratchDir)
	}

	cfg := struct {
		WritableDirs []string `json:"w"`
	}{WritableDirs: writable}
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal sandbox config: %w", err)
	}
	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("get executable path: %w", err)
	}
	args := []string{"__sandbox__", "--", "bash", "-c", command}
	env := append(commandEnvironment(os.Environ(), profile),
		"__SANDBOX_HELPER=1",
		"__SANDBOX_CONFIG="+string(cfgJSON),
	)
	// Spawn on a PR_SET_NO_NEW_PRIVS thread so the child inherits it (see
	// withNoNewPrivs); otherwise landlock_restrict_self EPERMs in the child
	// when forked from a Go runtime thread lacking the flag.
	var p *Process
	spawnErr := withNoNewPrivs(func() error {
		var err error
		p, err = startCommand(ctx, self, args, env, ws, &syscall.SysProcAttr{Setpgid: true}, nil, files)
		return err
	})
	return p, spawnErr
}
