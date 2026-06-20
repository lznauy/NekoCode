package indexer

import (
	"path/filepath"
	"regexp"
	"strings"
)

const (
	indexMaxFiles = 5000
	indexMaxDepth = 10
)

var (
	ignoreDirs = map[string]bool{
		"node_modules": true, "vendor": true, "target": true, ".git": true,
		"dist": true, "build": true, "__pycache__": true, ".cache": true,
		".next": true, ".turbo": true, "coverage": true, "testdata": true,
		".nekocode": true, "venv": true, ".venv": true, "env": true,
	}

	goGeneratedRE = regexp.MustCompile(`\.(pb|mock|generated)\.go$`)

	supportedExts = map[string]bool{
		".go": true, ".js": true, ".jsx": true, ".ts": true, ".tsx": true,
		".py": true, ".rs": true,
	}

	extToLang = map[string]string{
		".go":   "go",
		".js":   "javascript",
		".jsx":  "javascript",
		".ts":   "typescript",
		".tsx":  "typescript",
		".py":   "python",
		".rs":   "rust",
		".java": "java",
	}
)

// ShouldSkipDir returns true if the directory should be skipped during walks.
func ShouldSkipDir(name string) bool {
	return ignoreDirs[name] || (strings.HasPrefix(name, ".") && name != ".")
}

// SupportsFile reports whether the path is indexable.
func SupportsFile(path string) bool {
	ext := filepath.Ext(path)
	if !supportedExts[ext] {
		return false
	}
	return ext != ".go" || !goGeneratedRE.MatchString(filepath.Base(path))
}

func detectLanguageForFile(ext string) string {
	if lang, ok := extToLang[ext]; ok {
		return lang
	}
	return "unknown"
}
