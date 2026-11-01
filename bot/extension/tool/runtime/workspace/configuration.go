package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
)

// ConfigurationHash observes current effective roots and the execution root
// atomically. It returns no paths and does not resolve or scan the filesystem.
func (m *Manager) ConfigurationHash() string {
	if m == nil {
		return ""
	}
	m.mu.RLock()
	primary, roots := m.primary, m.snapshotLocked()
	m.mu.RUnlock()
	slices.SortFunc(roots, func(a, b Root) int {
		if order := strings.Compare(a.Path, b.Path); order != 0 {
			return order
		}
		return strings.Compare(string(a.Access), string(b.Access))
	})
	data, _ := json.Marshal(struct {
		Schema  int    `json:"schema"`
		Primary string `json:"primary"`
		Roots   []Root `json:"roots"`
	}{1, primary, roots})
	digest := sha256.Sum256(data)
	return "v1:" + hex.EncodeToString(digest[:])
}
