package workspace

import (
	"fmt"
	"path/filepath"
)

// RootApproval binds an exact root to its manager configuration and session.
// Its fields are private so a UI reply cannot replace the approved authority.
type RootApproval struct {
	manager  *Manager
	root     Root
	session  string
	revision uint64
}

func (a RootApproval) Root() Root { return a.root }

// ApprovalRevision changes when configuration or session identity changes,
// including a switch away and back, but not for parallel additive grants.
func (m *Manager) ApprovalRevision() uint64 {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.approvalRevision
}

func (m *Manager) PrepareRootApproval(path string, access Access) (RootApproval, error) {
	m.mu.RLock()
	primary, session, revision := m.primary, m.sessionID, m.approvalRevision
	m.mu.RUnlock()
	if session == "" {
		return RootApproval{}, fmt.Errorf("workspace session is not active")
	}
	path = expandHome(path)
	if !filepath.IsAbs(path) {
		path = filepath.Join(primary, path)
	}
	resolved, err := Resolve(path)
	if err != nil {
		return RootApproval{}, err
	}
	root, err := normalizeRoot(Root{Path: resolved, Access: access})
	if err != nil {
		return RootApproval{}, err
	}
	if m.ApprovalRevision() != revision {
		return RootApproval{}, fmt.Errorf("workspace approval context changed; do not retry automatically")
	}
	return RootApproval{manager: m, root: root, session: session, revision: revision}, nil
}

// CommitRootApproval validates context and path before persisting or adding
// authority. The callback must not call Manager methods. Failed persistence
// leaves session authority unchanged; no rollback window exposes the root.
func (m *Manager) CommitRootApproval(approval RootApproval, persist func(Root) error) (Root, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if approval.manager != m || approval.session == "" || approval.session != m.sessionID || approval.revision != m.approvalRevision {
		return Root{}, fmt.Errorf("workspace approval context changed; do not retry automatically")
	}
	resolved, err := Resolve(approval.root.Path)
	if err != nil {
		return Root{}, err
	}
	if resolved != approval.root.Path {
		return Root{}, fmt.Errorf("workspace approval target changed; do not retry automatically")
	}
	if persist != nil {
		if err := persist(approval.root); err != nil {
			return Root{}, err
		}
		root := approval.root
		root.Canonical = true
		m.configured = upsertRoot(m.configured, root)
	} else {
		m.sessions[m.sessionID] = upsertRoot(m.sessions[m.sessionID], approval.root)
	}
	return approval.root, nil
}
