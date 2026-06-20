package editdsl

import (
	"path/filepath"
	"strings"
)

// ExtractPathsFromPatch pulls file paths from a hashline patch string.
// Accepts both [path#TAG] bracketed headers and bare path#TAG headers.
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
	return append(paths, extractBarePaths(s)...)
}

func extractBarePaths(s string) []string {
	var paths []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			continue
		}
		idx := strings.LastIndex(line, "#")
		if idx <= 0 || len(line)-idx-1 != 8 {
			continue
		}
		tag := line[idx+1:]
		if isHexTag(tag) {
			paths = append(paths, line[:idx])
		}
	}
	return paths
}

func isHexTag(tag string) bool {
	for _, c := range tag {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// ExtractFirstPathFromPatch extracts the basename of the first file path.
func ExtractFirstPathFromPatch(patch string) string {
	paths := ExtractPathsFromPatch(patch)
	if len(paths) > 0 {
		return filepath.Base(paths[0])
	}
	return ""
}
