package graph

import (
	"path/filepath"
	"strings"
)

var extToLang = map[string]string{
	".go":   "go",
	".js":   "javascript",
	".jsx":  "javascript",
	".ts":   "typescript",
	".tsx":  "typescript",
	".py":   "python",
	".rs":   "rust",
	".java": "java",
}

func detectLanguageForFile(ext string) string {
	if lang, ok := extToLang[ext]; ok {
		return lang
	}
	return "unknown"
}

func detectLanguage(g *Graph) string {
	langs := make(map[string]int)
	for _, n := range g.Nodes {
		ext := filepath.Ext(n.File)
		if lang := detectLanguageForFile(ext); lang != "unknown" {
			langs[lang]++
		}
	}
	maxCount := 0
	lang := "unknown"
	for l, c := range langs {
		if c > maxCount {
			maxCount = c
			lang = l
		}
	}
	return lang
}

func detectModule(g *Graph) string {
	for _, n := range g.Nodes {
		if n.PkgPath != "" && strings.Contains(n.PkgPath, ".") {
			parts := strings.Split(n.PkgPath, "/")
			if len(parts) >= 3 {
				return strings.Join(parts[:3], "/")
			}
			return n.PkgPath
		}
	}
	return ""
}
