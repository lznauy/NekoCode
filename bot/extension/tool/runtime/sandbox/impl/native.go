//go:build linux
// +build linux

package impl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"nekocode/bot/extension/tool/runtime/netpolicy"
	"nekocode/util/fs"
)

// childFlag is the sentinel argument that tells a re-exec'd process to
// behave as the sandbox child (set up namespaces and exec the command).
const childFlag = "--nekocode-sandbox-child"

const (
	stagingRootPrefix = "nekocode-sb-"
	stagingOwnerFile  = ".owner-pid"

	defaultRootTmpfsSize = "2g"
	defaultTmpTmpfsSize  = "2g"
)

// sandboxStagingBase returns the directory under which per-run staging
// roots are created. It prefers ~/.nekocode/sandbox so sandbox artifacts
// live alongside the rest of the user's NekoCode data instead of cluttering
// the system /tmp. It falls back to the OS temp dir when the home dir is
// unavailable (e.g. read-only home in some containers).
var sandboxStagingBase = sync.OnceValue(func() string {
	if base := fs.NekocodeDataDir("sandbox"); os.MkdirAll(base, 0o700) == nil {
		return base
	}
	return os.TempDir()
})

// defaultStagingRoot is the shared fallback used when the parent could not
// create a unique per-run staging dir. It is the base dir itself, so the
// parent must NOT clean it up (see runNativeBash) — doing so would break
// concurrent runs that share it.
func defaultStagingRoot() string { return sandboxStagingBase() }

// isNativeAvailable reports whether the current system can run the native
// Linux-namespaced sandbox. It requires Linux and working user namespaces.
// The probe forks a helper process, so the result is cached: it cannot
// change during the lifetime of this process.
var isNativeAvailable = sync.OnceValue(func() bool {
	if os.Getenv("NEKOCODE_DISABLE_NATIVE_SANDBOX") != "" {
		return false
	}
	cmd := exec.Command("unshare", "--user", "--map-root-user", "true")
	return cmd.Run() == nil
})

// nativeLaunch holds everything the shared preparation phase produces for
// launching the sandboxed child. cleanup releases the per-run staging dir;
// it is a no-op when the staging root is the shared fallback or was supplied
// by the caller.
type nativeLaunch struct {
	self    string
	args    []string
	workdir string
	sysproc *syscall.SysProcAttr
	cleanup func()
}

// prepareNativeLaunch performs the preparation shared by runNativeBash and
// startNativeBash: workspace validation, staging-root creation, profile
// marshaling, and namespace (SysProcAttr) construction. On error any staging
// dir it created is removed before returning; on success the caller owns
// launch.cleanup.
func prepareNativeLaunch(command string, profile Profile) (*nativeLaunch, error) {
	if profile.Workspace == "" {
		return nil, fmt.Errorf("sandbox workspace is required")
	}

	ws, err := filepath.Abs(profile.Workspace)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace: %w", err)
	}
	profile.Workspace = ws
	profile.OutboundDomains, err = netpolicy.Normalize(profile.OutboundDomains)
	if err != nil {
		return nil, err
	}
	profile.EgressSocket = ""

	writePaths, err := resolveWritePaths(profile.WritePaths)
	if err != nil {
		return nil, err
	}
	profile.WritePaths = writePaths

	readPaths, err := resolveWritePaths(profile.ReadPaths)
	if err != nil {
		return nil, err
	}
	profile.ReadPaths = readPaths

	// The parent creates the staging mountpoint and the caller cleans it up
	// after the child exits. Using a unique dir avoids collisions between
	// concurrent sandbox runs. When MkdirTemp fails we fall back to a shared
	// default path (the base dir itself), which must NOT be removed (it would
	// break concurrent runs).
	createdStaging := false
	if profile.StagingRoot == "" {
		base := sandboxStagingBase()
		cleanupStaleStagingRoots(base)
		if staging, err := os.MkdirTemp(base, stagingRootPrefix); err == nil {
			profile.StagingRoot = staging
			createdStaging = true
			writeStagingOwner(staging)
		} else {
			profile.StagingRoot = defaultStagingRoot()
		}
	}
	if err := os.MkdirAll(profile.StagingRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create staging root: %w", err)
	}
	cleanup := func() {}
	if createdStaging {
		staging := profile.StagingRoot
		cleanup = func() { cleanupStagingRoot(staging) }
	}

	if len(profile.OutboundDomains) > 0 {
		proxy, err := createEgressProxy(profile.OutboundDomains)
		if err != nil {
			cleanup()
			return nil, err
		}
		previousCleanup := cleanup
		cleanup = func() { proxy.Close(); previousCleanup() }
		profile.EgressSocket = proxy.socket()
	}
	profileJSON, err := json.Marshal(profile)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("marshal profile: %w", err)
	}

	self, err := os.Executable()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("get executable path: %w", err)
	}

	// Default to an isolated network namespace. Only when the caller has been
	// granted public network access do we keep the host network namespace.
	var cloneFlags uintptr = syscall.CLONE_NEWUSER |
		syscall.CLONE_NEWNS |
		syscall.CLONE_NEWIPC |
		syscall.CLONE_NEWUTS |
		syscall.CLONE_NEWPID |
		syscall.CLONE_NEWNET
	if profile.Network && profile.OutboundDomains == nil {
		// Network was granted: share the host network namespace instead of
		// creating an isolated (loopback-only) one.
		cloneFlags &^= syscall.CLONE_NEWNET
	}
	sysproc := &syscall.SysProcAttr{
		Cloneflags: cloneFlags,
		UidMappings: []syscall.SysProcIDMap{
			{ContainerID: 0, HostID: os.Getuid(), Size: 1},
		},
		GidMappings: []syscall.SysProcIDMap{
			{ContainerID: 0, HostID: os.Getgid(), Size: 1},
		},
		GidMappingsEnableSetgroups: false,
		Setpgid:                    true,
	}

	return &nativeLaunch{
		self:    self,
		args:    []string{childFlag, string(profileJSON), command},
		workdir: profile.Workspace,
		sysproc: sysproc,
		cleanup: cleanup,
	}, nil
}

// runNativeBash executes command inside a fresh set of Linux namespaces
// (user, mount, net, pid, ipc, uts) created without any external binary.
// It re-execs the current binary with childFlag; the child process builds
// an isolated filesystem view (via pivot_root) and execs /bin/bash -c command.
func runNativeBash(ctx context.Context, command string, profile Profile, timeout time.Duration) (string, error) {
	launch, err := prepareNativeLaunch(command, profile)
	if err != nil {
		return "", err
	}
	defer launch.cleanup()

	// Namespace creation or the child's mount setup can fail on kernels /
	// container runtimes / CI that restrict user namespaces or mount calls.
	// Classify those as UnavailableError so the router falls back to Landlock.
	// The child setup failures are prefixed "sandbox child:"; we match that
	// plus EPERM/EACCES to avoid misclassifying an EACCES from the command
	// itself (e.g. writing /etc) which would not carry the child prefix.
	unavail := func(stderr string, runErr error) bool {
		if strings.Contains(stderr, "sandbox child:") &&
			(strings.Contains(stderr, "operation not permitted") ||
				strings.Contains(stderr, "permission denied")) {
			return true
		}
		return strings.Contains(stderr, "operation not permitted") ||
			strings.Contains(runErr.Error(), "operation not permitted")
	}

	if profile.OutboundDomains != nil {
		return runChild(ctx, launch.self, launch.args, nil, launch.workdir, timeout, launch.sysproc, nil, "")
	}
	return runChild(ctx, launch.self, launch.args, nil, launch.workdir, timeout, launch.sysproc, unavail, "namespace creation failed")
}

func startNativeBash(ctx context.Context, command string, profile Profile) (*Process, error) {
	launch, err := prepareNativeLaunch(command, profile)
	if err != nil {
		return nil, err
	}
	// startCommand invokes cleanup on start failure and after the process
	// exits, so ownership of launch.cleanup transfers to it here.
	p, err := startCommand(ctx, launch.self, launch.args, nil, launch.workdir, launch.sysproc, launch.cleanup)
	if err != nil {
		return nil, UnavailableError{Reason: fmt.Sprintf("namespace creation failed: %v", err)}
	}
	return p, nil
}

func writeStagingOwner(staging string) {
	if staging == "" || staging == defaultStagingRoot() {
		return
	}
	_ = os.WriteFile(filepath.Join(staging, stagingOwnerFile), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600)
}

func cleanupStagingRoot(staging string) {
	if staging == "" || staging == defaultStagingRoot() {
		return
	}
	_ = unix.Unmount(staging, unix.MNT_DETACH)
	_ = os.RemoveAll(staging)
}

func cleanupStaleStagingRoots(parent string) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), stagingRootPrefix) {
			continue
		}
		staging := filepath.Join(parent, entry.Name())
		data, err := os.ReadFile(filepath.Join(staging, stagingOwnerFile))
		if err != nil {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil || processAlive(pid) {
			continue
		}
		cleanupStagingRoot(staging)
	}
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func sandboxTmpfsSize(envName, fallback string) string {
	size := strings.TrimSpace(os.Getenv(envName))
	if validTmpfsSize(size) {
		return size
	}
	return fallback
}

func validTmpfsSize(size string) bool {
	if size == "" {
		return false
	}
	digits := 0
	for i, r := range size {
		if r >= '0' && r <= '9' {
			digits++
			continue
		}
		if digits > 0 && i == len(size)-1 {
			switch r {
			case 'k', 'K', 'm', 'M', 'g', 'G', 't', 'T':
				return true
			}
		}
		return false
	}
	return digits == len(size)
}

// handleSandboxChild is invoked from init() when the process has been
// re-exec'd by runNativeBash. It sets up the filesystem view and execs
// the target command, never returning.
func handleSandboxChild() {
	if len(os.Args) < 4 || os.Args[1] != childFlag {
		return
	}

	var profile Profile
	if err := json.Unmarshal([]byte(os.Args[2]), &profile); err != nil {
		fmt.Fprintf(os.Stderr, "sandbox child: invalid profile: %v\n", err)
		os.Exit(1)
	}
	command := os.Args[3]

	if err := sandboxChildSetupAndExec(profile, command); err != nil {
		var exit *exec.ExitError
		if profile.OutboundDomains != nil && errors.As(err, &exit) {
			code := exit.ExitCode()
			if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				code = 128 + int(status.Signal())
			}
			if code < 1 {
				code = 1
			}
			os.Exit(code)
		}
		fmt.Fprintf(os.Stderr, "sandbox child: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func init() {
	handleRestrictedCommand()
	handleSandboxChild()
}

// sandboxChildSetupAndExec builds an isolated filesystem view and execs the
// command. It uses pivot_root so the workspace keeps its host-absolute path
// (absolute paths in commands still work) while /tmp, /home and the system
// directories are isolated or read-only.
func sandboxChildSetupAndExec(profile Profile, command string) error {
	root := profile.StagingRoot
	if root == "" {
		root = defaultStagingRoot()
	}

	// 1. Detach all mounts from the host namespace so nothing leaks back.
	if err := unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""); err != nil {
		return fmt.Errorf("make root private: %w", err)
	}

	// 2. The new root is a fresh tmpfs mounted on the staging mountpoint.
	//    Mounting on a subdirectory (not on /tmp or /home itself) means the
	//    host /tmp, /home, and any workspace that lives under them stay
	//    reachable as bind sources until we pivot away from the staging dir.
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("create staging root: %w", err)
	}
	rootSize := sandboxTmpfsSize("NEKOCODE_SANDBOX_ROOT_SIZE", defaultRootTmpfsSize)
	if err := unix.Mount("tmpfs", root, "tmpfs", 0, "size="+rootSize); err != nil {
		return fmt.Errorf("mount staging root: %w", err)
	}

	// put_old must exist under the new root before pivot_root.
	if err := os.MkdirAll(filepath.Join(root, ".old"), 0o755); err != nil {
		return fmt.Errorf("create pivot old: %w", err)
	}

	// 3. Bind host paths into the new root. All sources are still reachable
	//    here: the only thing we shadowed is the staging dir itself.
	//
	//    By default the sandbox is "restrict, don't hide": every top-level
	//    host directory is bound read-only, so commands see the real
	//    filesystem (ls/stat/cat return truthful results) and only writes
	//    outside authorized roots fail (EPERM). The overlays below (/dev,
	//    /proc, /run, /tmp) and the writable binds (workspace, WritePaths)
	//    then stack on top of this read-only tree. Binds are best-effort:
	//    mountpoint roots of other filesystems (e.g. /boot/efi) refuse to
	//    bind under a user namespace, and hiding those is fine.
	//
	//    NEKOCODE_SANDBOX_HIDE_FS=1 restores the legacy behavior (only a
	//    fixed set of system dirs is bound; everything else stays hidden).
	if os.Getenv("NEKOCODE_SANDBOX_HIDE_FS") != "" {
		for _, src := range []string{"/usr", "/bin", "/lib", "/lib64", "/lib32", "/etc", "/nix/store"} {
			if _, err := os.Stat(src); err != nil {
				continue
			}
			if err := bindHostPath(root, src, true); err != nil {
				return err
			}
		}
	} else {
		entries, err := os.ReadDir("/")
		if err != nil {
			return fmt.Errorf("list host root: %w", err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			switch entry.Name() {
			// Handled by dedicated overlays below.
			case "proc", "sys", "dev", "run", "tmp":
				continue
			}
			// Non-recursive: nested mounts (docker overlays, bind farms)
			// stay hidden, which is fine for read visibility.
			_ = bindHostPath(root, "/"+entry.Name(), true)
		}
		// Top-level binds are non-recursive, so nested mountpoints (e.g.
		// /nix/store on NixOS) stay hidden behind them. Re-bind the
		// critical ones explicitly: the shell (/bin/sh is a symlink into
		// /nix/store) and TLS certs depend on them.
		for _, src := range []string{"/usr", "/bin", "/lib", "/lib64", "/lib32", "/etc", "/nix/store"} {
			if _, err := os.Stat(src); err != nil {
				continue
			}
			if err := bindHostPath(root, src, true); err != nil {
				return err
			}
		}
	}

	// /dev: a fresh tmpfs with the essential device nodes bind-mounted from
	// the host (binding the device files works even though binding the /dev
	// mountpoint itself does not).
	if err := setupDev(root); err != nil {
		return err
	}

	// /proc: fresh mount for the new pid namespace (best-effort).
	if err := os.MkdirAll(filepath.Join(root, "proc"), 0o755); err == nil {
		_ = unix.Mount("proc", filepath.Join(root, "proc"), "proc", 0, "")
	}

	// /run: isolated tmpfs. On NixOS /run/current-system/sw holds the system
	// binaries; bind it in if present (best-effort — it may be a mountpoint
	// and fail under some kernels).
	if err := os.MkdirAll(filepath.Join(root, "run"), 0o755); err != nil {
		return fmt.Errorf("mkdir /run: %w", err)
	}
	_ = unix.Mount("tmpfs", filepath.Join(root, "run"), "tmpfs", 0, "size=128m")
	if _, err := os.Stat("/run/current-system/sw"); err == nil {
		_ = bindHostPath(root, "/run/current-system/sw", true)
	}

	// /tmp: isolated tmpfs so writes never reach the host.
	if err := os.MkdirAll(filepath.Join(root, "tmp"), 0o1777); err != nil {
		return fmt.Errorf("mkdir /tmp: %w", err)
	}
	tmpSize := sandboxTmpfsSize("NEKOCODE_SANDBOX_TMP_SIZE", defaultTmpTmpfsSize)
	if err := unix.Mount("tmpfs", filepath.Join(root, "tmp"), "tmpfs", 0, "size="+tmpSize); err != nil {
		return fmt.Errorf("mount /tmp: %w", err)
	}

	// Workspace: bind at its host-absolute path so absolute paths in commands
	// resolve to the real project directory. read-only mode keeps the
	// workspace visible but prevents writes unless the caller separately
	// authorized a specific WritePath.
	if err := bindHostPathStrict(root, profile.Workspace, profile.Mode == ModeReadOnly); err != nil {
		return err
	}

	// WritePaths: bind at their host-absolute paths. They are extra
	// writable directories (package caches, authorized external dirs)
	// beyond the workspace. The caller is responsible for authorization;
	// the sandbox only handles mount mechanics.
	for _, wp := range profile.WritePaths {
		if err := bindHostPath(root, wp, false); err != nil {
			return err
		}
	}

	// ReadPaths: bind read-only at their host-absolute paths. These are
	// extra authorized workspace roots that must be visible (but not
	// writable) inside the sandbox.
	for _, rp := range profile.ReadPaths {
		if err := bindHostPath(root, rp, true); err != nil {
			return err
		}
	}

	if profile.EgressSocket != "" {
		socket := filepath.Join(root, "tmp", ".nekocode-egress.sock")
		f, err := os.OpenFile(socket, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return err
		}
		_ = f.Close()
		if err := unix.Mount(profile.EgressSocket, socket, "", unix.MS_BIND, ""); err != nil {
			return fmt.Errorf("mount egress socket: %w", err)
		}
		if err := unix.Mount("", socket, "", unix.MS_REMOUNT|unix.MS_BIND|unix.MS_RDONLY, ""); err != nil {
			return fmt.Errorf("protect egress socket: %w", err)
		}
		profile.EgressSocket = "/tmp/.nekocode-egress.sock"
	}
	if profile.OutboundDomains != nil {
		self, err := os.Executable()
		if err != nil {
			return err
		}
		target := filepath.Join(root, "tmp", ".nekocode-exec")
		f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, 0500)
		if err != nil {
			return err
		}
		_ = f.Close()
		if err := unix.Mount(self, target, "", unix.MS_BIND, ""); err != nil {
			return fmt.Errorf("mount restricted helper: %w", err)
		}
		if err := unix.Mount("", target, "", unix.MS_REMOUNT|unix.MS_BIND|unix.MS_RDONLY, ""); err != nil {
			return fmt.Errorf("protect restricted helper: %w", err)
		}
	}
	// 4. Swap the staging tree in as the root.
	if err := unix.PivotRoot(root, filepath.Join(root, ".old")); err != nil {
		return fmt.Errorf("pivot_root: %w", err)
	}
	// The process cwd still points at the old-root workspace inode (now under
	// /.old). Move to the new-root workspace before detaching the old root.
	if err := os.Chdir(profile.Workspace); err != nil {
		return fmt.Errorf("chdir workspace: %w", err)
	}
	if err := unix.Unmount("/.old", unix.MNT_DETACH); err != nil {
		return fmt.Errorf("detach old root: %w", err)
	}

	// 5. Environment + exec the command.
	env := buildSandboxEnv()
	if profile.OutboundDomains != nil {
		return superviseRestrictedCommand(profile, command, env)
	}

	shells := []string{"/bin/bash", "/bin/sh", "/usr/bin/bash", "/usr/bin/sh"}
	var lastErr error
	for _, shell := range shells {
		if _, err := os.Stat(shell); err != nil {
			continue
		}
		name := "bash"
		if strings.HasSuffix(shell, "/sh") {
			name = "sh"
		}
		lastErr = unix.Exec(shell, []string{name, "-c", command}, env)
		if lastErr == nil {
			return nil
		}
	}
	if lastErr != nil {
		return fmt.Errorf("exec shell: %w", lastErr)
	}
	return fmt.Errorf("no usable shell found (tried %v)", shells)
}

// setupDev creates a minimal /dev tree on a fresh tmpfs inside the staging
// root and bind-mounts the essential host device nodes into it. Binding the
// individual device files works in a user namespace; binding the /dev
// mountpoint itself does not.
func setupDev(root string) error {
	devDir := filepath.Join(root, "dev")
	if err := os.MkdirAll(devDir, 0o755); err != nil {
		return fmt.Errorf("mkdir /dev: %w", err)
	}
	if err := unix.Mount("tmpfs", devDir, "tmpfs", 0, "size=64m,mode=755"); err != nil {
		return fmt.Errorf("mount /dev: %w", err)
	}
	for _, dev := range []string{"/dev/null", "/dev/zero", "/dev/urandom", "/dev/random", "/dev/tty", "/dev/console"} {
		if _, err := os.Stat(dev); err != nil {
			continue
		}
		dst := filepath.Join(root, dev)
		if f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY, 0o666); err == nil {
			if err := f.Close(); err != nil {
				return fmt.Errorf("close device placeholder %s: %w", dst, err)
			}
		}
		if err := unix.Mount(dev, dst, "", unix.MS_BIND, ""); err != nil {
			return fmt.Errorf("bind %s: %w", dev, err)
		}
	}
	// Convenience symlinks that shells and tools commonly expect.
	for _, link := range []struct{ target, name string }{
		{"/proc/self/fd", filepath.Join(root, "dev", "fd")},
		{"/proc/self/fd/0", filepath.Join(root, "dev", "stdin")},
		{"/proc/self/fd/1", filepath.Join(root, "dev", "stdout")},
		{"/proc/self/fd/2", filepath.Join(root, "dev", "stderr")},
	} {
		_ = os.Symlink(link.target, link.name)
	}
	return nil
}

// bindHostPath bind-mounts the host path src into the staging root at the
// same absolute path. When readOnly is true the bind is remounted read-only.
// The remount is best-effort: some source mounts (notably /nix/store on
// NixOS) are owned by the host's user namespace and refuse flag changes with
// EPERM, but they are already read-only on the host so the bind inherits ro.
func bindHostPath(root, src string, readOnly bool) error {
	return bindHostPathWithRemountPolicy(root, src, readOnly, true)
}

func bindHostPathStrict(root, src string, readOnly bool) error {
	return bindHostPathWithRemountPolicy(root, src, readOnly, false)
}

func bindHostPathWithRemountPolicy(root, src string, readOnly bool, ignoreReadOnlyEPERM bool) error {
	if src == "" {
		return nil
	}
	dst := filepath.Join(root, src)
	if err := os.MkdirAll(dst, 0o755); err != nil {
		// The target may sit under a read-only top-level bind (e.g. a
		// WritePath under /home after the "restrict, don't hide" change),
		// so MkdirAll fails with EROFS/EPERM before the writable bind can
		// be established. WritePaths are authorized writable roots, so
		// materializing their directory on the host is acceptable and
		// matches the caller's intent; retry after creating the source.
		if !readOnly && (errors.Is(err, syscall.EROFS) || errors.Is(err, syscall.EPERM)) {
			if mkErr := os.MkdirAll(src, 0o755); mkErr != nil {
				return fmt.Errorf("mkdir %s: %w", src, mkErr)
			}
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return fmt.Errorf("mkdir %s: %w", src, err)
			}
		} else {
			return fmt.Errorf("mkdir %s: %w", src, err)
		}
	}
	if err := unix.Mount(src, dst, "", unix.MS_BIND, ""); err != nil {
		return fmt.Errorf("bind %s: %w", src, err)
	}
	if readOnly {
		if err := unix.Mount("", dst, "", unix.MS_REMOUNT|unix.MS_BIND|unix.MS_RDONLY, ""); err != nil {
			if err != unix.EPERM || !ignoreReadOnlyEPERM {
				return fmt.Errorf("remount read-only %s: %w", src, err)
			}
		}
	}
	return nil
}

// hostHome returns the user's home directory as seen on the host. The child
// inherits the parent environment, so $HOME is available before we replace
// the environment for the exec'd command.
func hostHome() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return "/root"
}

// buildSandboxEnv builds the environment for the sandboxed command.
//
// It inherits the host environment so NixOS-specific variables that the
// profile / home-manager / nix-shell set are preserved:
//
//   - NIX_SSL_CERT_FILE / SSL_CERT_FILE: CA bundle path inside /nix/store;
//     without it curl/go/git TLS verification fails.
//   - GOROOT / GOPATH / GOMODCACHE: Go toolchain resolution.
//   - NIX_PATH / NIX_PROFILES: nix commands.
//   - locale, TERM_PROGRAM, COLORTERM, etc.
//
// Sandbox-control variables (__SANDBOX_*, NEKOCODE_*) are scrubbed so the
// command cannot accidentally re-enter the helper. HOME/PATH/LC_ALL/TERM are
// overridden: HOME points at the real home (cache bind mounts live there),
// PATH is inherited (host PATH already covers /nix/store, home-manager, and
// user-installed bins — all of which are either bind-mounted ro or resolved
// through the inherited PATH).
func buildSandboxEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "__SANDBOX_") || strings.HasPrefix(kv, "NEKOCODE_") {
			continue
		}
		env = append(env, kv)
	}
	path := os.Getenv("PATH")
	if path == "" {
		path = sandboxPath()
	}
	for k, v := range map[string]string{
		"HOME":   hostHome(),
		"PATH":   path,
		"LC_ALL": "C.UTF-8",
		"TERM":   "xterm-256color",
		"TMPDIR": "/tmp",
		"TMP":    "/tmp",
		"TEMP":   "/tmp",
	} {
		env = setEnv(env, k, v)
	}
	return env
}

// setEnv replaces (or appends) the value of env key k in the KEY=VAL list.
func setEnv(env []string, k, v string) []string {
	prefix := k + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = prefix + v
			return env
		}
	}
	return append(env, prefix+v)
}

// sandboxPath builds PATH for the sandboxed shell. On NixOS the real
// binaries live under /run/current-system/sw/bin; elsewhere the standard
// locations suffice.
func sandboxPath() string {
	if _, err := os.Stat("/run/current-system/sw/bin"); err == nil {
		return "/run/current-system/sw/bin:/usr/local/bin:/usr/bin:/bin"
	}
	return "/usr/local/bin:/usr/bin:/bin"
}
