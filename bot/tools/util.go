// util.go — 工具函数：字符串处理、路径安全、HTTP 客户端。
package tools

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"nekocode/bot/tools/hashline"
	"nekocode/common"
)

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
		return "", fmt.Errorf("path resolution failed: %w", err)
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

// normalizePathKey normalizes a path for use as a cache key.
// It resolves to absolute path and resolves symlinks, matching ValidatePath behavior.
func normalizePathKey(path string) string {
	if resolved, err := ValidatePath(path); err == nil {
		return resolved
	}
	abs, _ := filepath.Abs(path)
	return abs
}

// NormalizeText strips ANSI escapes and normalizes line endings to LF.
func NormalizeText(text string) string {
	text = StripAnsi(text)
	return hashline.NormalizeToLF(text)
}

// ReadNormalizedFile reads a file, strips ANSI escapes, and normalizes line endings.
// Returns the normalized text content.
func ReadNormalizedFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return NormalizeText(string(data)), nil
}

// ReadSafeFile validates the path and reads the file, returning raw bytes.
func ReadSafeFile(path string) ([]byte, error) {
	safePath, err := ValidatePath(path)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(safePath)
}

// ExtractPathsFromPatch pulls file paths from a hashline patch string.
// Accepts both [path#TAG] (bracketed) and path#TAG (bare) headers.
// The input can be string or any (for compatibility with map[string]any args).
func ExtractPathsFromPatch(patch any) []string {
	s, ok := patch.(string)
	if !ok || s == "" {
		return nil
	}
	var paths []string
	rest := s
	for {
		start := strings.Index(rest, "[")
		if start < 0 {
			break
		}
		rest = rest[start+1:]
		end := strings.IndexByte(rest, ']')
		if end < 0 {
			break
		}
		inner := rest[:end]
		if hashIdx := strings.LastIndexByte(inner, '#'); hashIdx > 0 {
			paths = append(paths, inner[:hashIdx])
		}
		rest = rest[end+1:]
	}
	// Also extract paths from bare path#TAG headers (LLMs often forget brackets).
	paths = append(paths, extractBarePaths(s)...)
	return paths
}

// extractBarePaths finds bare path#XXXXXXXX headers (8-char hex tag after #,
// no brackets). Used as a fallback when the LLM omits [...] brackets.
func extractBarePaths(s string) []string {
	var paths []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		// Skip lines already handled by bracket extraction
		if strings.HasPrefix(line, "[") {
			continue
		}
		idx := strings.LastIndex(line, "#")
		if idx <= 0 || len(line)-idx-1 != 8 {
			continue
		}
		// Verify the tag is 8 hex chars
		hex := true
		for _, c := range line[idx+1:] {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				hex = false
				break
			}
		}
		if hex {
			paths = append(paths, line[:idx])
		}
	}
	return paths
}

// ExtractFirstPathFromPatch extracts the basename of the first file path
// from a hashline patch string. Useful for display purposes.
func ExtractFirstPathFromPatch(patch string) string {
	paths := ExtractPathsFromPatch(patch)
	if len(paths) > 0 {
		return filepath.Base(paths[0])
	}
	return ""
}

// NewToolHTTPClient creates an HTTP client with the given timeout, sharing a
// common transport for connection pooling.
func NewToolHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: common.SharedTransport,
		Timeout:   timeout,
	}
}

// RecordSnapshot records a file snapshot if GlobalSnapshotStore is set.
// Returns the snapshot tag, or empty string if store is nil.
// Record internally normalizes line endings before hashing; pre-normalization
// by the caller is redundant.
func RecordSnapshot(path, content string) string {
	if store := GetGlobalSnapshotStore(); store != nil {
		return store.Record(path, content)
	}
	return ""
}
