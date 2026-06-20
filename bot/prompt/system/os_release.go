package system

import (
	"os"
	"runtime"
	"strings"
)

func OSRelease() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return runtime.GOOS
	}
	return ParseOSReleaseID(string(data), runtime.GOOS)
}

func ParseOSReleaseID(content, fallback string) string {
	for line := range strings.SplitSeq(content, "\n") {
		if id, ok := strings.CutPrefix(line, "ID="); ok {
			return strings.Trim(id, `"`)
		}
	}
	return fallback
}
