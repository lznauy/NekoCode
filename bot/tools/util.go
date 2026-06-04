// util.go — 工具函数：字符串处理、路径安全、HTTP 客户端。
package tools

import (
	"fmt"
	"hash/fnv"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const base62Chars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// HashLine returns a 4-character hash of the line content for hashline editing.
// Empty lines return "____".
func HashLine(s string) string {
	if s == "" {
		return "____"
	}
	h := fnv.New64a()
	h.Write([]byte(s))
	u := h.Sum64()
	return string([]byte{
		base62Chars[u%62],
		base62Chars[(u/62)%62],
		base62Chars[(u/(62*62))%62],
		base62Chars[(u/(62*62*62))%62],
	})
}

// AnnotateLines prefixes each line with "lineNo:[hash]" for hashline editing.
func AnnotateLines(content string) string {
	lines := strings.Split(content, "\n")
	var b strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&b, "%d:[%s]%s", i+1, HashLine(line), line)
		if i < len(lines)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

var ansiRegex = regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")

// StripAnsi removes ANSI escape sequences from a string.
func StripAnsi(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

// ValidatePath resolves path against the current working directory.
// It resolves symlinks and returns the absolute path, but no longer rejects
// paths outside cwd — the confirmation system handles user consent.
func ValidatePath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("path resolution failed: %v", err)
	}
	// Resolve symlinks to prevent escape via symlink indirection.
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		parent := filepath.Dir(abs)
		realParent, pErr := filepath.EvalSymlinks(parent)
		if pErr != nil {
			real = abs
		} else {
			real = filepath.Join(realParent, filepath.Base(abs))
		}
	}
	return real, nil
}

func NewToolHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:    10,
			IdleConnTimeout: 60 * time.Second,
		},
		Timeout: timeout,
	}
}
