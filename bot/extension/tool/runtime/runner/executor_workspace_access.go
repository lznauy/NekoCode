package runner

import (
	"fmt"
	"nekocode/protocol"
	"path/filepath"
	"strings"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/bot/extension/tool/runtime/toolutil"
	"nekocode/bot/extension/tool/runtime/workspace"
)

func (e *Executor) ensureWorkspaceAccess(tc core.ToolCallItem, confirmFn protocol.ConfirmFunc) (core.ToolCallItem, string, bool) {
	if tc.Name == "image_understand" {
		paths, err := toolutil.ImageUnderstandingPaths(tc.Args)
		if err != nil {
			return tc, err.Error(), false
		}
		for i, path := range paths {
			if err := validateLocalImagePath(path); err != nil {
				return tc, err.Error(), false
			}
			safe, _, allowed, err := e.workspace.CheckReadLexical(path)
			if err != nil {
				return tc, err.Error(), false
			}
			if !allowed {
				return tc, fmt.Sprintf("image path is outside configured workspaces: %s", safe), false
			}
			paths[i] = safe
		}
		next := make(map[string]any, len(tc.Args))
		for k, v := range tc.Args {
			next[k] = v
		}
		if _, batch := tc.Args["paths"]; batch {
			next["paths"] = paths
		} else {
			next["path"] = paths[0]
		}
		tc.Args = next
		return tc, "", true
	}
	// Each target gets its own workspace check; diff's display header is
	// never a filesystem authority or an execution argument to rewrite.
	access, ok := fileToolAccess(tc.Name)
	if !ok {
		return tc, "", true
	}
	for _, target := range permission.FileToolTargets(tc.Name, tc.Args) {
		if target.Path == "" {
			return tc, "file path must not be empty", false
		}
		safe, message, allowed := e.ensureFilePathAccess(tc.Name, tc.ID, target.Path, access, confirmFn)
		if !allowed {
			return tc, message, false
		}
		tc.Args = permission.SetFileTarget(tc.Args, target, safe)
	}
	return tc, "", true
}

func (e *Executor) ensureFilePathAccess(toolName, callID, path string, access workspace.Access, confirmFn protocol.ConfirmFunc) (string, string, bool) {
	policy := e.permissionSnapshot()
	var safePath string
	var allowed bool
	var err error
	if access == workspace.AccessReadWrite {
		safePath, _, allowed, err = e.workspace.CheckWrite(path)
	} else {
		safePath, _, allowed, err = e.workspace.CheckRead(path)
	}
	if err != nil {
		return "", err.Error(), false
	}
	if allowed {
		return safePath, "", true
	}
	// Full-takeover mode: grant session-scoped workspace access without the
	// prompt, consistent with bypassing every other approval.
	if e.FullAccess() {
		rootPath, err := candidateWorkspaceRoot(toolName, safePath)
		if err != nil {
			return "", err.Error(), false
		}
		if _, err := e.workspace.AddSessionRoot(rootPath, access); err != nil {
			return "", err.Error(), false
		}
		return safePath, "", true
	}
	if confirmFn == nil {
		return "", fmt.Sprintf("workspace access required for %s: %s", access, safePath), false
	}

	rootPath, err := candidateWorkspaceRoot(toolName, safePath)
	if err != nil {
		return "", err.Error(), false
	}
	rootApproval, err := e.workspace.PrepareRootApproval(rootPath, access)
	if err != nil {
		return "", err.Error(), false
	}
	rootPath = rootApproval.Root().Path
	approval := &protocol.ApprovalContext{
		Source:            "workspace",
		Reason:            fmt.Sprintf("add %s workspace for %s", access, toolName),
		Scope:             protocol.ApprovalScopeProject,
		Workspace:         rootPath,
		RememberWorkspace: policy.workspace,
	}
	if policy.store == nil || policy.workspace == "" {
		approval.Scope = protocol.ApprovalScopeOnce
	}
	if access == workspace.AccessReadWrite {
		approval.WritePaths = []string{rootPath}
	}
	req := protocol.NewApprovalRequest("workspace", map[string]any{
		"path":           rootPath,
		"access":         string(access),
		"requested_path": safePath,
	}, protocol.ConfirmKindPermission, approval)
	req.CallID = callID
	canRemember := approval.CanRemember()
	reply := confirmFn(req)
	if !canRemember {
		reply.Remember = false
	}
	if !reply.Allowed {
		return "", "workspace access denied by user; do not retry the same request — ask the user how to proceed", false
	}
	if err := e.commitWorkspaceApproval(policy, rootApproval, reply.Remember); err != nil {
		return "", err.Error(), false
	}
	return safePath, "", true
}

func (e *Executor) commitWorkspaceApproval(policy permissionSnapshot, approval workspace.RootApproval, remember bool) error {
	e.fnMu.Lock()
	defer e.fnMu.Unlock()
	if !policy.currentLocked(e) {
		return errApprovalContextChanged
	}
	var persist func(workspace.Root) error
	if remember && policy.store == nil {
		return fmt.Errorf("cannot save workspace approval: project permission store is unavailable")
	}
	if remember && policy.store != nil {
		persist = func(root workspace.Root) error {
			if err := policy.store.RememberWorkspaceRoot(permission.WorkspaceRoot{Path: root.Path, Access: string(root.Access)}); err != nil {
				return fmt.Errorf("cannot save workspace approval; access was not granted: %w", err)
			}
			return nil
		}
	}
	_, err := e.workspace.CommitRootApproval(approval, persist)
	return err
}

func fileToolAccess(toolName string) (workspace.Access, bool) {
	switch toolName {
	case "read", "list", "tree", "grep", "glob", "diff", "image_understand":
		return workspace.AccessReadOnly, true
	case "write", "edit", "image_gen":
		return workspace.AccessReadWrite, true
	default:
		return "", false
	}
}

// fileToolPath identifies all target arguments for detecting canonical changes.
// It is not used to resolve a batch as though it were a single path.
func fileToolPath(tc core.ToolCallItem) string {
	targets := permission.FileToolTargets(tc.Name, tc.Args)
	paths := make([]string, len(targets))
	for i, target := range targets {
		paths[i] = target.Path
	}
	return strings.Join(paths, "\x00")
}

func candidateWorkspaceRoot(toolName, safePath string) (string, error) {
	if toolName == "image_gen" {
		return filepath.Abs(safePath)
	}
	return workspace.CandidateRoot(safePath)
}

func (e *Executor) normalizeFilePolicyPath(tc core.ToolCallItem) core.ToolCallItem {
	policy := e.permissionSnapshot()
	for _, target := range permission.FileToolTargets(tc.Name, tc.Args) {
		tc.Args = permission.SetFileTarget(tc.Args, target, permission.LexicalPath(target.Path, e.workspace.Primary(), policy.home))
	}
	return tc
}
