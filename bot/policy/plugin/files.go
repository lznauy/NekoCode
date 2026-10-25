package plugin

import (
	"fmt"
	"io"
	"os"
)

const maxPluginSourceBytes = 1024 * 1024

func readLimitedFile(path string, limit int) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("plugin path must be a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, fmt.Errorf("plugin file exceeds %d bytes", limit)
	}
	return data, nil
}
