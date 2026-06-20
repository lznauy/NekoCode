package edit

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

func lintFile(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return lintGo(path)
	default:
		return ""
	}
}

func lintGo(path string) string {
	cmd := exec.Command("gofmt", "-e", path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg != "" {
			return fmt.Sprintf("gofmt: %s", msg)
		}
	}
	return ""
}
