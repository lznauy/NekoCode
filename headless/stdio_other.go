//go:build !unix

package headless

import "os"

func stdioStreams() (*os.File, *os.File, func(), error) { return os.Stdin, os.Stdout, func() {}, nil }
