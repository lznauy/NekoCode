//go:build linux

package impl

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// Called only inside a newly created network namespace, before capabilities
// are dropped. Bringing up lo adds no host interfaces or outbound routes.
func enableLoopback() error {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open loopback control: %w", err)
	}
	defer unix.Close(fd)
	req, err := unix.NewIfreq("lo")
	if err == nil {
		err = unix.IoctlIfreq(fd, unix.SIOCGIFFLAGS, req)
	}
	if err == nil {
		req.SetUint16(req.Uint16() | unix.IFF_UP)
		err = unix.IoctlIfreq(fd, unix.SIOCSIFFLAGS, req)
	}
	if err != nil {
		return fmt.Errorf("enable isolated loopback: %w", err)
	}
	return nil
}
