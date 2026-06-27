package common

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// NekocodeHome returns the user-level ~/.nekocode directory path.
func NekocodeHome() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".nekocode")
}

// NekocodeLogDir returns the user-level ~/.nekocode/logs directory path.
// All runtime log output (debug logs, panic logs) should live here, not /tmp.
func NekocodeLogDir() string {
	return filepath.Join(NekocodeHome(), "logs")
}

// NekocodeDataDir returns a user-level ~/.nekocode/<subdir> data directory path.
// Used for runtime artifacts that are not logs (edit undo snapshots, exports, ...).
func NekocodeDataDir(subdir string) string {
	return filepath.Join(NekocodeHome(), subdir)
}

// NekocodeDirs returns the project-level and user-level .nekocode/<subdir> directories.
func NekocodeDirs(subdir string) []string {
	var dirs []string
	if cwd, err := os.Getwd(); err == nil {
		dirs = append(dirs, filepath.Join(cwd, ".nekocode", subdir))
	}
	dirs = append(dirs, filepath.Join(NekocodeHome(), subdir))
	return dirs
}

// LooksLikeGit returns true if s looks like a "user/repo" git reference.
func LooksLikeGit(s string) bool {
	parts := strings.Split(s, "/")
	return len(parts) == 2 && !strings.Contains(parts[0], ".") && !strings.Contains(parts[0], ":")
}

// SplitPairs splits on commas that are not inside double-quoted segments.
func SplitPairs(s string) []string {
	var pairs []string
	start := 0
	inQuote := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			inQuote = !inQuote
		case '\\':
			if inQuote && i+1 < len(s) {
				i++ // skip escaped char
			}
		case ',':
			if !inQuote {
				pairs = append(pairs, s[start:i])
				start = i + 1
			}
		}
	}
	pairs = append(pairs, s[start:])
	return pairs
}

// TruncateByRune truncates s to max runes.
func TruncateByRune(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}

// FormatTokens formats a token count for display (e.g. 1200 → "1.2k", 1500000 → "1.5m").
func FormatTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fm", float64(n)/1_000_000)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// ReadJSONFile reads a JSON file and unmarshals it into T.
func ReadJSONFile[T any](path string) (T, error) {
	var zero T
	data, err := os.ReadFile(path)
	if err != nil {
		return zero, err
	}
	var result T
	if err := json.Unmarshal(data, &result); err != nil {
		return zero, err
	}
	return result, nil
}

// WriteFileWithDir creates parent directories and writes data to path.
func WriteFileWithDir(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, perm)
}

// HTTPError is a typed error carrying the HTTP status code and response body.
// Use errors.As to extract the status code in retry logic instead of parsing
// formatted error strings.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("API error (HTTP %d): %s", e.StatusCode, e.Body)
}

// NewHTTPError creates an HTTPError.
func NewHTTPError(statusCode int, body string) *HTTPError {
	return &HTTPError{StatusCode: statusCode, Body: body}
}

// SSELineData extracts the data payload from an SSE "data: " line.
// Returns the data string and true if the line is a data line, or ("", false) otherwise.
func SSELineData(line string) (string, bool) {
	if !strings.HasPrefix(line, "data: ") {
		return "", false
	}
	return strings.TrimPrefix(line, "data: "), true
}

// ParseYAMLFrontmatter extracts YAML frontmatter between --- delimiters.
// Returns the YAML bytes and the body text after the closing ---.
func ParseYAMLFrontmatter(content string) (yamlBytes []byte, body string, err error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "---") {
		return nil, "", fmt.Errorf("missing frontmatter (---)")
	}
	rest := content[3:]
	yamlText, body, found := strings.Cut(rest, "\n---")
	if !found {
		return nil, "", fmt.Errorf("unclosed frontmatter")
	}
	return []byte(yamlText), strings.TrimSpace(body), nil
}
