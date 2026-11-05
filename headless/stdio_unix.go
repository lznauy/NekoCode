//go:build unix

package headless

import (
	"golang.org/x/sys/unix"
	"os"
)

// Nonblocking open prevents a supplied FIFO from trapping signal cancellation.
// The caller checks the opened descriptor is a regular file before reading.
func openSchemaFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|unix.O_NONBLOCK, 0)
}
