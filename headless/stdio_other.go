//go:build !unix

package headless

import (
	"fmt"
	"os"
)

func stdioStreams() (*os.File, *os.File, func(), error) { return os.Stdin, os.Stdout, func() {}, nil }

func openSchemaFile(path string) (*os.File, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("output schema must be a regular file")
	}
	return os.Open(path)
}
