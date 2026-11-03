package runner

import (
	"errors"

	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/protocol"
)

var errApprovalContextChanged = errors.New("approval context changed; command was not executed; do not retry automatically")

// A grant is bound to the policy and project shown before the callback runs.
// Ordinary rule additions do not invalidate simultaneous approvals in that
// project, but project or declared-policy changes do.
type permissionSnapshot struct {
	store             *permission.Store
	engine            *permission.Engine
	workspace, home   string
	revision          uint64
	workspaceRevision uint64
	sessionRevision   uint64
	mode              permission.Mode
	modeEpoch         uint64
}

type commandApprovalReply struct {
	protocol.ConfirmReply
	policy permissionSnapshot
	rules  []permission.Rule
	err    error
}

func (e *Executor) permissionSnapshot() permissionSnapshot {
	e.fnMu.Lock()
	defer e.fnMu.Unlock()
	if e.permSessionRevision != e.workspace.SessionRevision() {
		e.rebuildEngineLocked()
	}
	mode := e.permissionModeStateLocked()
	return permissionSnapshot{store: e.permStore, engine: e.permEngine, workspace: e.permWorkspace, home: e.permHome, revision: e.permRevision, workspaceRevision: e.workspace.ApprovalRevision(), sessionRevision: e.permSessionRevision, mode: mode.mode, modeEpoch: mode.epoch}
}

func (s permissionSnapshot) currentLocked(e *Executor) bool {
	mode := e.permissionModeStateLocked()
	return s.mode == mode.mode && s.modeEpoch == mode.epoch && s.sessionRevision == e.workspace.SessionRevision() && s.revision == e.permRevision && s.store == e.permStore && s.workspace == e.permWorkspace && s.home == e.permHome && s.workspaceRevision == e.workspace.ApprovalRevision()
}

func (s permissionSnapshot) current(e *Executor) bool {
	e.fnMu.RLock()
	defer e.fnMu.RUnlock()
	return s.currentLocked(e)
}

// rememberApprovedRules applies one approval decision. Persistent grants go to
// the trusted permission store; session-scoped grants (exact statements of
// dynamic shell structures) stay in memory so one-off commands never
// accumulate in the file.
func (e *Executor) rememberApprovedRules(policy permissionSnapshot, rules []permission.Rule) error {
	e.fnMu.Lock()
	defer e.fnMu.Unlock()
	if !policy.currentLocked(e) {
		return errApprovalContextChanged
	}
	if len(rules) == 0 {
		return nil
	}
	var persistent, session []permission.Rule
	for _, rule := range rules {
		if rule.Persistent() {
			persistent = append(persistent, rule)
			continue
		}
		session = append(session, rule)
	}
	if len(persistent) > 0 && policy.store != nil && policy.workspace != "" {
		if err := policy.store.RememberRules(policy.workspace, persistent); err != nil {
			return err
		}
	}
	e.applySessionRulesLocked(session)
	if len(session) > 0 || len(persistent) > 0 {
		e.rebuildEngineLocked()
	}
	return nil
}
