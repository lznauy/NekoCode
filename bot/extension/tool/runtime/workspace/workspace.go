package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

type Access string

const (
	AccessReadOnly  Access = "read-only"
	AccessReadWrite Access = "read-write"
)

type Root struct {
	Path   string `json:"path"`
	Access Access `json:"access"`
}

// Manager owns workspace authority for one tool registry. Temporary roots are
// isolated by session and never shared across Bot instances.
type Manager struct {
	mu         sync.RWMutex
	configured []Root
	sessions   map[string][]Root
	sessionID  string
}

func New(primary string, extra []Root) *Manager {
	m := &Manager{sessions: make(map[string][]Root), sessionID: "default"}
	m.Configure(primary, extra)
	return m
}

func (m *Manager) Configure(primary string, extra []Root) {
	if strings.TrimSpace(primary) == "" {
		roots := append(fallbackRoots(), extra...)
		m.mu.Lock()
		m.configured = normalizeRoots(roots)
		m.mu.Unlock()
		return
	}
	roots := []Root{{Path: primary, Access: AccessReadWrite}}
	roots = append(roots, extra...)
	m.mu.Lock()
	m.configured = normalizeRoots(roots)
	m.mu.Unlock()
}

func (m *Manager) SetSession(id string) {
	m.mu.Lock()
	m.sessionID = id
	m.mu.Unlock()
}

func (m *Manager) DropSession(id string) {
	if id == "" {
		return
	}
	m.mu.Lock()
	delete(m.sessions, id)
	if m.sessionID == id {
		m.sessionID = ""
	}
	m.mu.Unlock()
}

func (m *Manager) AddSessionRoot(path string, access Access) (Root, error) {
	root, err := normalizeRoot(Root{Path: path, Access: access})
	if err != nil {
		return Root{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessionID == "" {
		return Root{}, fmt.Errorf("workspace session is not active")
	}
	m.sessions[m.sessionID] = upsertRoot(m.sessions[m.sessionID], root)
	return root, nil
}

func (m *Manager) Snapshot() []Root {
	m.mu.RLock()
	roots := append([]Root(nil), m.configured...)
	for _, r := range m.sessions[m.sessionID] {
		roots = upsertRoot(roots, r)
	}
	m.mu.RUnlock()
	if len(roots) > 0 {
		return roots
	}
	return fallbackRoots()
}

func (m *Manager) CheckRead(path string) (string, Root, bool, error) {
	return m.check(path, AccessReadOnly)
}

// CheckReadLexical checks an already configured root without resolving links
// or touching the filesystem. Callers must subsequently use a no-follow open.
func (m *Manager) CheckReadLexical(path string) (string, Root, bool, error) {
	abs, err := filepath.Abs(expandHome(path))
	if err != nil {
		return "", Root{}, false, fmt.Errorf("path resolution failed: %w", err)
	}
	clean := filepath.Clean(abs)
	for _, root := range m.Snapshot() {
		if rootAllows(root, AccessReadOnly) && insideRoot(clean, root.Path) {
			return clean, root, true, nil
		}
	}
	return clean, Root{}, false, nil
}

func (m *Manager) CheckWrite(path string) (string, Root, bool, error) {
	return m.check(path, AccessReadWrite)
}

func (m *Manager) check(path string, need Access) (string, Root, bool, error) {
	resolved, err := Resolve(path)
	if err != nil {
		return "", Root{}, false, err
	}
	for _, root := range m.Snapshot() {
		if !rootAllows(root, need) {
			continue
		}
		if insideRoot(resolved, root.Path) {
			return resolved, root, true, nil
		}
	}
	return resolved, Root{}, false, nil
}

type managerContextKey struct{}

func WithManager(ctx context.Context, manager *Manager) context.Context {
	if manager == nil {
		return ctx
	}
	return context.WithValue(ctx, managerContextKey{}, manager)
}

func FromContext(ctx context.Context) (*Manager, bool) {
	manager, ok := ctx.Value(managerContextKey{}).(*Manager)
	return manager, ok && manager != nil
}

func Resolve(path string) (string, error) {
	abs, err := filepath.Abs(expandHome(path))
	if err != nil {
		return "", fmt.Errorf("path resolution failed: %w", err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		parent := filepath.Dir(abs)
		realParent, pErr := filepath.EvalSymlinks(parent)
		if pErr != nil {
			return filepath.Clean(abs), nil
		}
		return filepath.Join(realParent, filepath.Base(abs)), nil
	}
	return real, nil
}

func CandidateRoot(path string) (string, error) {
	resolved, err := Resolve(path)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(resolved); err == nil && info.IsDir() {
		return resolved, nil
	}
	return filepath.Dir(resolved), nil
}

func normalizeRoots(roots []Root) []Root {
	out := make([]Root, 0, len(roots))
	for _, r := range roots {
		root, err := normalizeRoot(r)
		if err != nil {
			continue
		}
		out = upsertRoot(out, root)
	}
	slices.SortFunc(out, func(a, b Root) int { return strings.Compare(a.Path, b.Path) })
	return out
}

func normalizeRoot(r Root) (Root, error) {
	p := strings.TrimSpace(r.Path)
	if p == "" {
		return Root{}, fmt.Errorf("workspace path is required")
	}
	p = expandHome(p)
	abs, err := filepath.Abs(p)
	if err != nil {
		return Root{}, err
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	r.Path = filepath.Clean(abs)
	if r.Access != AccessReadOnly && r.Access != AccessReadWrite {
		r.Access = AccessReadOnly
	}
	return r, nil
}

// expandHome resolves a leading ~/ to the user's home directory so paths
// handed to Resolve and root normalization behave consistently.
func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, _ := os.UserHomeDir(); home != "" {
			p = filepath.Join(home, strings.TrimPrefix(p, "~/"))
		}
	}
	return p
}

func upsertRoot(roots []Root, root Root) []Root {
	for i, existing := range roots {
		if existing.Path != root.Path {
			continue
		}
		if rootAllows(root, AccessReadWrite) || !rootAllows(existing, AccessReadWrite) {
			roots[i] = root
		}
		return roots
	}
	return append(roots, root)
}

func rootAllows(root Root, need Access) bool {
	if need == AccessReadOnly {
		return root.Access == AccessReadOnly || root.Access == AccessReadWrite
	}
	return root.Access == AccessReadWrite
}

func insideRoot(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}

func fallbackRoots() []Root {
	cwd := os.Getenv("NEKOCODE_WORKSPACE")
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	roots := []Root{{Path: cwd, Access: AccessReadWrite}}
	if extra := os.Getenv("NEKOCODE_EXTRA_DIRS"); extra != "" {
		for _, p := range filepath.SplitList(extra) {
			roots = append(roots, Root{Path: p, Access: AccessReadWrite})
		}
	}
	return normalizeRoots(roots)
}
