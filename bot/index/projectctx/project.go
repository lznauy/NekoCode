// project.go — project context preloading (NEKOCODE.md discovery and loading).
// Discovers and loads NEKOCODE.md files at session start.
// This eliminates repeated glob/grep/read exploration at the
// start of every conversation.

package projectctx

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const maxContextChars = 40000
const maxIncludeDepth = 3

// LoadProjectContext discovers NEKOCODE.md files from cwd up to root and
// returns formatted context ready for injection into the system prompt.
// Results are sorted root-first so files closer to cwd take precedence.
func LoadProjectContext(cwd string) string {
	var files []string

	// 1. User-global: ~/.nekocode/NEKOCODE.md
	if home, err := os.UserHomeDir(); err == nil {
		globalPath := filepath.Join(home, ".nekocode", "NEKOCODE.md")
		if _, err := os.Stat(globalPath); err == nil {
			files = append(files, globalPath)
		}
	}

	// 2. Walk from cwd up to root, finding NEKOCODE.md and .nekocode/NEKOCODE.md
	cwd = filepath.Clean(cwd)
	var projectDirs []string
	for dir := cwd; dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		projectDirs = append(projectDirs, dir)
	}
	// Reverse so root comes first.
	for i := 0; i < len(projectDirs)/2; i++ {
		j := len(projectDirs) - 1 - i
		projectDirs[i], projectDirs[j] = projectDirs[j], projectDirs[i]
	}

	for _, dir := range projectDirs {
		// NEKOCODE.md at project root
		p := filepath.Join(dir, "NEKOCODE.md")
		if _, err := os.Stat(p); err == nil {
			files = append(files, p)
		}
		// .nekocode/NEKOCODE.md
		p = filepath.Join(dir, ".nekocode", "NEKOCODE.md")
		if _, err := os.Stat(p); err == nil {
			files = append(files, p)
		}
	}

	// 3. .nekocode/rules/*.md (sorted by name, closest to cwd)
	for _, dir := range projectDirs {
		rulesDir := filepath.Join(dir, ".nekocode", "rules")
		entries, err := os.ReadDir(rulesDir)
		if err != nil {
			continue
		}
		var ruleFiles []string
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				ruleFiles = append(ruleFiles, filepath.Join(rulesDir, e.Name()))
			}
		}
		sort.Strings(ruleFiles)
		files = append(files, ruleFiles...)
	}

	if len(files) == 0 {
		return ""
	}

	// Deduplicate by real path while preserving order.
	seen := make(map[string]bool)
	var unique []string
	for _, f := range files {
		rp, err := filepath.EvalSymlinks(f)
		if err != nil {
			rp = f
		}
		if !seen[rp] {
			seen[rp] = true
			unique = append(unique, f)
		}
	}

	return buildContext(unique)
}

func buildContext(files []string) string {
	var b strings.Builder
	processed := make(map[string]bool)
	remaining := maxContextChars

	for _, f := range files {
		if remaining <= 0 {
			break
		}
		content := loadWithIncludes(f, processed, 0, &remaining)
		if content != "" {
			b.WriteString(content)
		}
	}

	result := strings.TrimSpace(b.String())
	if result == "" {
		return ""
	}
	return "<project-context>\n" + result + "\n</project-context>"
}

func loadWithIncludes(path string, processed map[string]bool, depth int, remaining *int) string {
	rp, err := filepath.EvalSymlinks(path)
	if err != nil {
		rp = path
	}
	if processed[rp] || depth > maxIncludeDepth || *remaining <= 0 {
		return ""
	}
	processed[rp] = true

	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	content := string(data)
	// Process @include directives.
	content = resolveIncludes(path, content, processed, depth, remaining)

	if len(content) > *remaining {
		content = content[:*remaining] + "\n..."
	}
	*remaining -= len(content)

	var b strings.Builder
	fmt.Fprintf(&b, "\n<!-- %s -->\n", filepath.Base(path))
	b.WriteString(content)
	b.WriteString("\n")
	return b.String()
}

// resolveIncludes replaces @path directives with included file contents.
// Supported forms: @./path (relative), @~/path (home-relative)
func resolveIncludes(baseFile, content string, processed map[string]bool, depth int, remaining *int) string {
	lines := strings.Split(content, "\n")
	var out []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "@") && !strings.HasPrefix(trimmed, "@@") {
			ref := strings.TrimPrefix(trimmed, "@")
			ref = strings.TrimSpace(ref)
			if ref == "" || !looksLikePath(ref) {
				out = append(out, line)
				continue
			}

			var target string
			switch {
			case strings.HasPrefix(ref, "~/"):
				home, err := os.UserHomeDir()
				if err != nil {
					out = append(out, line)
					continue
				}
				target = filepath.Join(home, ref[2:])
			case strings.HasPrefix(ref, "./") || strings.HasPrefix(ref, "../"):
				target = filepath.Join(filepath.Dir(baseFile), ref)
			case strings.HasPrefix(ref, "/"):
				target = ref
			default:
				// Bare filename: treat as relative.
				target = filepath.Join(filepath.Dir(baseFile), ref)
			}

			if isTextFile(target) {
				included := loadWithIncludes(target, processed, depth+1, remaining)
				if included != "" {
					out = append(out, included)
					continue
				}
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// Text file extensions that are safe to include.
var textExts = map[string]bool{
	".md": true, ".txt": true, ".go": true, ".rs": true, ".py": true,
	".js": true, ".ts": true, ".tsx": true, ".jsx": true, ".yaml": true,
	".yml": true, ".json": true, ".toml": true, ".cfg": true, ".ini": true,
	".sh": true, ".bash": true, ".zsh": true, ".fish": true,
	".c": true, ".h": true, ".cpp": true, ".hpp": true, ".java": true,
	".rb": true, ".php": true, ".swift": true, ".kt": true, ".scala": true,
	".sql": true, ".css": true, ".html": true, ".xml": true, ".svg": true,
	".Makefile": true, ".Dockerfile": true, ".env": true, ".gitignore": true,
	".editorconfig": true, ".codebuddy": true, ".rules": true,
}

// looksLikePath checks if a string looks like a file path (contains / or a
// known text extension) rather than a code annotation like @param or @return.
func looksLikePath(ref string) bool {
	if strings.Contains(ref, "/") {
		return true
	}
	ext := strings.ToLower(filepath.Ext(ref))
	return ext != "" && textExts[ext]
}

func isTextFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if textExts[ext] {
		return true
	}
	// Check common dotfiles without extensions.
	base := filepath.Base(path)
	noExtBases := map[string]bool{
		"Makefile": true, "Dockerfile": true, ".gitignore": true,
		".editorconfig": true, ".env": true,
	}
	return noExtBases[base]
}
