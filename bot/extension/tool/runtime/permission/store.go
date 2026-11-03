package permission

import (
	"encoding/json"
	"errors"
	"nekocode/bot/extension/tool/runtime/authority"
	"nekocode/bot/extension/tool/runtime/netpolicy"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"nekocode/bot/extension/tool/runtime/core"
)

const permissionStoreVersion = 1

// Older binaries must not interpret a scoped grant as broad net.outbound.
const scopedNetworkCapability = "net.outbound.domains"

// storeMu serializes read-modify-write cycles on the shared permissions file.
// Every Executor owns a separate Store instance bound to the same project
// file, so a package-level lock is required to keep concurrent remembers
// (e.g. parallel sub-agents approving rules) from overwriting each other.
var storeMu sync.Mutex

// Store persists capability grants and remembered rules for one project. The
// project's permissions live in the trusted user authority directory.
type Store struct {
	root string
}

// NewStore creates a store bound to a project root.
func NewStore(root string) *Store {
	return &Store{root: filepath.Clean(root)}
}

// Path returns the trusted store path, or an empty string if it cannot be resolved.
func (s *Store) Path() string { return s.projectPath() }

func (s *Store) projectPath() string {
	path, _ := authority.StorePath(s.root)
	return path
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
	Effect          string   `json:"effect"`
	Tool            string   `json:"tool"`
	Capabilities    []string `json:"capabilities"`
	WritePaths      []string `json:"writePaths,omitempty"`
	OutboundDomains []string `json:"outbound_domains"`
	Workspace       string   `json:"workspace,omitempty"`
	Reason          string   `json:"reason,omitempty"`
}

type WorkspaceRoot struct {
	Path   string `json:"path"`
	Access string `json:"access"`
}

// CapabilityDecision distinguishes unavailable policy from an unmatched request.
// Deny always wins across every alias in one immutable file snapshot.
type CapabilityDecision struct {
	Allow bool
	Deny  bool
	Grant Grant
	Err   error
}

func (s *Store) Decide(tools []string, req core.PermissionRequest) CapabilityDecision {
	file, err := s.load()
	if err != nil {
		return CapabilityDecision{Err: err}
	}
	for _, tool := range tools {
		if g, denied := s.findInFile(file, tool, req, "deny", grantDeniesRequest); denied {
			return CapabilityDecision{Deny: true, Grant: g}
		}
	}
	for _, tool := range tools {
		if g, allowed := s.findInFile(file, tool, req, "allow", grantAllowsRequest); allowed {
			return CapabilityDecision{Allow: true, Grant: g}
		}
	}
	return CapabilityDecision{}
}

// Match is the legacy boolean interface; authorization callers should use Decide.
func (s *Store) Match(tool string, req core.PermissionRequest) (Grant, bool) {
	decision := s.Decide([]string{tool}, req)
	return decision.Grant, decision.Allow
}

// Denied is the legacy boolean interface; authorization callers should use Decide.
func (s *Store) Denied(tool string, req core.PermissionRequest) (Grant, bool) {
	return s.find(normalizeToolName(tool), req, "deny", grantDeniesRequest)
}

func (s *Store) find(tool string, req core.PermissionRequest, effect string, match func(Grant, core.PermissionRequest) bool) (Grant, bool) {
	f, err := s.load()
	if err != nil {
		return Grant{}, false
	}
	return s.findInFile(f, tool, req, effect, match)
}

// findInFile keeps deny and allow decisions on the same immutable disk snapshot.
func (s *Store) findInFile(f permissionFile, tool string, req core.PermissionRequest, effect string, match func(Grant, core.PermissionRequest) bool) (Grant, bool) {
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
	if req.Scope == "once" {
		return errors.New("one-time capability approval cannot be persisted")
	}
	domains, err := netpolicy.FromDetails(req.Details)
	if err != nil {
		return err
	}
	tool = normalizeToolName(tool)
	return s.mutate(func(f *permissionFile) bool {
		p := f.Projects[s.root]
		g := Grant{
			Effect:          "allow",
			Tool:            tool,
			Capabilities:    append([]string(nil), req.Capabilities...),
			WritePaths:      WritePathsFromRequest(req),
			OutboundDomains: domains,
			Workspace:       s.root,
			Reason:          req.Reason,
		}
		if domains != nil {
			for i, capability := range g.Capabilities {
				if capability == core.CapNetOutbound {
					g.Capabilities[i] = scopedNetworkCapability
				}
			}
		}
		for _, existing := range p.Grants {
			if existing.Effect == g.Effect && existing.Tool == g.Tool &&
				existing.Workspace == g.Workspace &&
				slices.Equal(existing.Capabilities, g.Capabilities) &&
				slices.Equal(normalizeWritePaths(existing.WritePaths), g.WritePaths) &&
				(existing.OutboundDomains == nil) == (g.OutboundDomains == nil) && slices.Equal(existing.OutboundDomains, g.OutboundDomains) {
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
	var roots []WorkspaceRoot
	for _, root := range p.Workspaces {
		// Approval stores a canonical directory, not a link whose target can
		// change on restart. Never let Configure rebind a remembered grant.
		if !filepath.IsAbs(root.Path) || (root.Access != "read-only" && root.Access != "read-write") {
			continue
		}
		real, err := filepath.EvalSymlinks(root.Path)
		if err != nil || filepath.Clean(real) != filepath.Clean(root.Path) {
			continue
		}
		roots = append(roots, root)
	}
	return roots
}

// mutate runs a load-modify-save cycle atomically across all Store instances
// and processes. A fixed sidecar lock remains stable across atomic file renames.
// The update callback reports whether it changed anything;
// no-change cycles skip the save entirely (common for repeated approvals that
// hit the dedupe paths).
func (s *Store) mutate(update func(*permissionFile) (changed bool)) error {
	storeMu.Lock()
	defer storeMu.Unlock()

	path, err := authority.StorePath(s.root)
	if err != nil {
		return err
	}
	unlock, err := lockStore(path)
	if err != nil {
		return err
	}
	defer unlock()
	f, err := loadPermissionFile(path)
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
	return savePermissionFile(path, f)
}

func (s *Store) load() (permissionFile, error) {
	path, err := authority.StorePath(s.root)
	if err != nil {
		return permissionFile{}, err
	}
	return loadPermissionFile(path)
}

func loadPermissionFile(path string) (permissionFile, error) {
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

func savePermissionFile(path string, f permissionFile) error {
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
