//go:build !unix

package stdio

import "os"

// Streams supplies the process standard streams on platforms without Unix pollers.
func Streams() (*os.File, *os.File, func(), error) { return os.Stdin, os.Stdout, func() {}, nil }
