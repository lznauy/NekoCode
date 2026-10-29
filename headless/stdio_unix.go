//go:build unix

package headless

import (
	"golang.org/x/sys/unix"
	"os"
)

// Inherited standard files may not be registered with Go's poller. Closing
// them cannot interrupt a blocked syscall. Register nonblocking duplicates;
// restore shared file-description flags only after all I/O has stopped.
func stdioStreams() (*os.File, *os.File, func(), error) {
	in, restoreIn, err := pollableFile(os.Stdin)
	if err != nil {
		return nil, nil, nil, err
	}
	out, restoreOut, err := pollableFile(os.Stdout)
	if err != nil {
		_ = in.Close()
		restoreIn()
		return nil, nil, nil, err
	}
	return in, out, func() { restoreOut(); restoreIn() }, nil
}

func pollableFile(file *os.File) (*os.File, func(), error) {
	raw, err := file.SyscallConn()
	if err != nil {
		return nil, nil, err
	}
	var flags, fd int
	var setupErr error
	err = raw.Control(func(original uintptr) {
		flags, setupErr = unix.FcntlInt(original, unix.F_GETFL, 0)
		if setupErr != nil {
			return
		}
		fd, setupErr = unix.FcntlInt(original, unix.F_DUPFD_CLOEXEC, 0)
		if setupErr != nil {
			return
		}
		setupErr = unix.SetNonblock(fd, true)
		if setupErr != nil {
			_ = unix.Close(fd)
		}
	})
	if err != nil {
		return nil, nil, err
	}
	if setupErr != nil {
		return nil, nil, setupErr
	}
	restore := func() {
		_ = raw.Control(func(original uintptr) { _ = unix.SetNonblock(int(original), flags&unix.O_NONBLOCK != 0) })
	}
	return os.NewFile(uintptr(fd), file.Name()), restore, nil
}

// Nonblocking open prevents a supplied FIFO from trapping signal cancellation.
// The caller checks the opened descriptor is a regular file before reading.
func openSchemaFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|unix.O_NONBLOCK, 0)
}
