//go:build linux

package impl

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

const restrictedCommandFlag = "--nekocode-restricted-command"

func handleRestrictedCommand() {
	if len(os.Args) != 3 || os.Args[1] != restrictedCommandFlag {
		return
	}
	runtime.LockOSThread()
	if err := restrictEgressCommand(); err != nil {
		fmt.Fprintln(os.Stderr, "network isolation setup failed:", err)
		os.Exit(125)
	}
	for _, shell := range []string{"/bin/bash", "/bin/sh", "/usr/bin/bash", "/usr/bin/sh"} {
		if _, err := os.Stat(shell); err == nil {
			if err := unix.Exec(shell, []string{shell, "-c", os.Args[2]}, os.Environ()); err == nil {
				return
			}
		}
	}
	fmt.Fprintln(os.Stderr, "network isolation: no usable shell")
	os.Exit(125)
}

func superviseRestrictedCommand(profile Profile, command string, env []string) error {
	// A command may not inspect its supervisor's descriptors/memory to steal
	// the Unix bridge endpoint. The command itself is additionally seccomped.
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		return err
	}
	var listener net.Listener
	var active sync.Map
	if len(profile.OutboundDomains) > 0 {
		fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			return err
		}
		req, err := unix.NewIfreq("lo")
		if err == nil {
			req.SetUint16(unix.IFF_UP | unix.IFF_LOOPBACK)
			err = unix.IoctlIfreq(fd, unix.SIOCSIFFLAGS, req)
		}
		_ = unix.Close(fd)
		if err != nil {
			return err
		}
		listener, err = net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			return err
		}
		defer listener.Close()
		proxyURL := "http://" + listener.Addr().String()
		filtered := make([]string, 0, len(env))
		for _, entry := range env {
			key, _, _ := strings.Cut(entry, "=")
			switch strings.ToLower(key) {
			case "http_proxy", "https_proxy", "all_proxy", "no_proxy":
				continue
			}
			filtered = append(filtered, entry)
		}
		env = append(filtered, "HTTP_PROXY="+proxyURL, "HTTPS_PROXY="+proxyURL, "http_proxy="+proxyURL, "https_proxy="+proxyURL, "ALL_PROXY="+proxyURL, "all_proxy="+proxyURL, "NO_PROXY=", "no_proxy=")
		defer active.Range(func(key, value any) bool { _ = key.(net.Conn).Close(); return true })
		go func() {
			for {
				local, err := listener.Accept()
				if err != nil {
					return
				}
				remote, err := net.Dial("unix", profile.EgressSocket)
				if err != nil {
					_ = local.Close()
					continue
				}
				active.Store(local, true)
				active.Store(remote, true)
				go func() { defer active.Delete(local); defer active.Delete(remote); relayEgress(local, remote) }()
			}
		}()
	}
	cmd := exec.Command("/tmp/.nekocode-exec", restrictedCommandFlag, command)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("restricted command: %w", err)
	}
	// Namespace PID 1 exits after this return; the kernel kills remaining
	// descendants even when they escaped the original process group.
	return nil
}

func restrictEgressCommand() error {
	// Drop namespace capabilities and their bounding set before executing user
	// code; no_new_privs prevents setuid/file capabilities restoring them.
	for capability := 0; capability <= unix.CAP_LAST_CAP; capability++ {
		if err := unix.Prctl(unix.PR_CAPBSET_DROP, uintptr(capability), 0, 0, 0); err != nil && err != unix.EINVAL {
			return err
		}
	}
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	data := [2]unix.CapUserData{}
	if err := unix.Capset(&header, &data[0]); err != nil {
		return err
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return err
	}
	var auditArch uint32
	switch runtime.GOARCH {
	case "amd64":
		auditArch = unix.AUDIT_ARCH_X86_64
	case "arm64":
		auditArch = unix.AUDIT_ARCH_AARCH64
	default:
		return fmt.Errorf("restricted networking unsupported on %s", runtime.GOARCH)
	}
	// seccomp_data: nr at offset 0, arch at 4, args[0] at 16.
	filters := []unix.SockFilter{
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 4},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jt: 1, K: auditArch},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_KILL_PROCESS},
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0},
	}
	if runtime.GOARCH == "amd64" {
		filters = append(filters, unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JGE | unix.BPF_K, Jf: 1, K: 0x40000000}, unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_KILL_PROCESS})
	}
	for _, nr := range []uint32{unix.SYS_SOCKET, unix.SYS_SOCKETPAIR} {
		filters = append(filters,
			unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jf: 3, K: nr},
			unix.SockFilter{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 16},
			unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jf: 1, K: unix.AF_UNIX},
			unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(syscall.EPERM)},
			unix.SockFilter{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0},
		)
	}
	for _, nr := range []uint32{unix.SYS_PTRACE, unix.SYS_PROCESS_VM_READV, unix.SYS_PROCESS_VM_WRITEV, unix.SYS_PIDFD_GETFD, unix.SYS_SETNS, unix.SYS_UNSHARE, unix.SYS_MOUNT, unix.SYS_UMOUNT2, unix.SYS_PIVOT_ROOT, unix.SYS_OPEN_BY_HANDLE_AT, unix.SYS_IO_URING_SETUP, unix.SYS_IO_URING_ENTER, unix.SYS_IO_URING_REGISTER, unix.SYS_BPF} {
		filters = append(filters, unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jf: 1, K: nr}, unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(syscall.EPERM)})
	}
	filters = append(filters, unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW})
	program := unix.SockFprog{Len: uint16(len(filters)), Filter: &filters[0]}
	return unix.Prctl(unix.PR_SET_SECCOMP, unix.SECCOMP_MODE_FILTER, uintptr(unsafe.Pointer(&program)), 0, 0)
}
