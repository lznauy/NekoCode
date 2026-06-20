package read

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func suggestSimilar(path string) []string {
	dir := filepath.Dir(path)
	base := strings.ToLower(filepath.Base(path))
	entries, _ := os.ReadDir(dir)

	type match struct {
		path  string
		score int
	}
	var matches []match
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.ToLower(e.Name())
		if name == base {
			continue
		}
		score := 0
		if strings.Contains(name, base) || strings.Contains(base, name) {
			score = 10
		} else if d := levenshtein(name, base); d <= 3 {
			score = max(0, 8-d)
		}
		if score > 0 {
			matches = append(matches, match{filepath.Join(dir, e.Name()), score})
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].score > matches[j].score })
	if len(matches) > 3 {
		matches = matches[:3]
	}
	out := make([]string, len(matches))
	for i, m := range matches {
		out[i] = m.path
	}
	return out
}

func levenshtein(a, b string) int {
	m, n := len(a), len(b)
	if m == 0 {
		return n
	}
	if n == 0 {
		return m
	}
	prev, cur := make([]int, n+1), make([]int, n+1)
	for j := range cur {
		prev[j] = j
	}
	for i := 1; i <= m; i++ {
		cur[0] = i
		for j := 1; j <= n; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, min(cur[j-1]+1, prev[j-1]+cost))
		}
		prev, cur = cur, prev
	}
	return prev[n]
}
