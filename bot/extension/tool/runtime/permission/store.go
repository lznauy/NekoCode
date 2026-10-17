package permission

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"nekocode/bot/extension/tool/runtime/core"
)

const permissionStoreVersion = 1

// storeMu serializes read-modify-write cycles on the shared permissions file.
// Every Executor owns a separate Store instance bound to the same project
// file, so a package-level lock is required to keep concurrent remembers
// (e.g. parallel sub-agents approving rules) from overwriting each other.
var storeMu sync.Mutex

// Store persists capability grants and remembered rules for one project. The
// project's permissions live at <root>/.nekocode/permissions.json.
type Store struct {
	root string
}

// NewStore creates a store bound to a project root.
func NewStore(root string) *Store {
	return &Store{root: filepath.Clean(root)}
}

func (s *Store) projectPath() string {
	return filepath.Join(s.root, ".nekocode", "permissions.json")
}

type permissionFile struct {
	Version  int                          `json:"version"`
	Projects map[string]permissionProject `json:"projects"`
}

type permissionProject struct {
	Grants     []Grant         `json:"grants"`
	Rules      []Rule          `json:"rules,omitempty"`
	Workspaces []WorkspaceRoot `json:"workspaces,omitempty"`
}

// Grant records a user-authorized capability opening for a tool. The JSON is
// intentionally minimal: effect + tool + capabilities + the workspace it was
// granted for (for traceability) + the human-readable reason.
type Grant struct {
	Effect       string   `json:"effect"`
	Tool         string   `json:"tool"`
	Capabilities []string `json:"capabilities"`
	WritePaths   []string `json:"writePaths,omitempty"`
	Workspace    string   `json:"workspace,omitempty"`
	Reason       string   `json:"reason,omitempty"`
}

type WorkspaceRoot struct {
	Path   string `json:"path"`
	Access string `json:"access"`
}

func (s *Store) Match(tool string, req core.PermissionRequest) (Grant, bool) {
	tool = normalizeToolName(tool)
	if g, denied := s.Denied(tool, req); denied {
		return g, false
	}
	return s.find(tool, req, "allow", grantAllowsRequest)
}

func (s *Store) Denied(tool string, req core.PermissionRequest) (Grant, bool) {
	return s.find(normalizeToolName(tool), req, "deny", grantDeniesRequest)
}

func (s *Store) find(tool string, req core.PermissionRequest, effect string, match func(Grant, core.PermissionRequest) bool) (Grant, bool) {
	f, err := s.load()
	if err != nil {
		return Grant{}, false
	}
	project, ok := f.Projects[s.root]
	if !ok {
		return Grant{}, false
	}
	tool = normalizeToolName(tool)
	for _, g := range project.Grants {
		if g.Effect != effect || normalizeToolName(g.Tool) != tool {
			continue
		}
		if g.Workspace != "" && g.Workspace != s.root {
			continue
		}
		if match(g, req) {
			return g, true
		}
	}
	return Grant{}, false
}

func (s *Store) Allow(tool string, req core.PermissionRequest) error {
	tool = normalizeToolName(tool)
	return s.mutate(func(f *permissionFile) bool {
		p := f.Projects[s.root]
		g := Grant{
			Effect:       "allow",
			Tool:         tool,
			Capabilities: append([]string(nil), req.Capabilities...),
			WritePaths:   WritePathsFromRequest(req),
			Workspace:    s.root,
			Reason:       req.Reason,
		}
		for _, existing := range p.Grants {
			if existing.Effect == g.Effect && existing.Tool == g.Tool &&
				existing.Workspace == g.Workspace &&
				slices.Equal(existing.Capabilities, g.Capabilities) &&
				slices.Equal(normalizeWritePaths(existing.WritePaths), g.WritePaths) {
				return false
			}
		}
		p.Grants = append(p.Grants, g)
		f.Projects[s.root] = p
		return true
	})
}

func (s *Store) RememberWorkspaceRoot(root WorkspaceRoot) error {
	return s.mutate(func(f *permissionFile) bool {
		p := f.Projects[s.root]
		for i, existing := range p.Workspaces {
			if existing.Path != root.Path {
				continue
			}
			if root.Access == "read-write" || existing.Access != "read-write" {
				p.Workspaces[i] = root
				f.Projects[s.root] = p
				return true
			}
			return false
		}
		p.Workspaces = append(p.Workspaces, root)
		f.Projects[s.root] = p
		return true
	})
}

func (s *Store) WorkspaceRoots() []WorkspaceRoot {
	f, err := s.load()
	if err != nil {
		return nil
	}
	p, ok := f.Projects[s.root]
	if !ok {
		return nil
	}
	return append([]WorkspaceRoot(nil), p.Workspaces...)
}

// mutate runs a load-modify-save cycle atomically across all Store instances
// in this process. The update callback reports whether it changed anything;
// no-change cycles skip the save entirely (common for repeated approvals that
// hit the dedupe paths).
func (s *Store) mutate(update func(*permissionFile) (changed bool)) error {
	storeMu.Lock()
	defer storeMu.Unlock()

	f, err := s.load()
	if err != nil {
		return err
	}
	if f.Version == 0 {
		f.Version = permissionStoreVersion
	}
	if f.Projects == nil {
		f.Projects = map[string]permissionProject{}
	}
	if !update(&f) {
		return nil
	}
	return s.save(f)
}

func (s *Store) load() (permissionFile, error) {
	path := s.projectPath()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return permissionFile{Version: permissionStoreVersion, Projects: map[string]permissionProject{}}, nil
	}
	if err != nil {
		return permissionFile{}, err
	}
	var f permissionFile
	if err := json.Unmarshal(data, &f); err != nil {
		return permissionFile{}, err
	}
	if f.Projects == nil {
		f.Projects = map[string]permissionProject{}
	}
	return f, nil
}

func (s *Store) save(f permissionFile) error {
	path := s.projectPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	// Write to a temp file then rename so a concurrent reader (or a crash)
	// never observes a truncated/partial permissions file.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".permissions-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
