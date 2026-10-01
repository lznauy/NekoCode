package runner

import (
	"fmt"
	"nekocode/protocol"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/bot/extension/tool/runtime/workspace"
)

func (e *Executor) ensureWorkspaceAccess(tc core.ToolCallItem, confirmFn protocol.ConfirmFunc) (core.ToolCallItem, string, bool) {
	access, ok := fileToolAccess(tc.Name)
	if !ok {
		return tc, "", true
	}
	path := fileToolPath(tc)
	if path == "" {
		return tc, "", true
	}
	if tc.Name == "image_understand" {
		if err := validateLocalImagePath(path); err != nil {
			return tc, err.Error(), false
		}
	}

	var safePath string
	var allowed bool
	var err error
	if tc.Name == "image_understand" {
		safePath, _, allowed, err = e.workspace.CheckReadLexical(path)
		if err != nil {
			return tc, err.Error(), false
		}
		if !allowed {
			return tc, fmt.Sprintf("image path is outside configured workspaces: %s", safePath), false
		}
		tc.Args = setToolPath(tc.Args, safePath)
		return tc, "", true
	}
	if access == workspace.AccessReadWrite {
		safePath, _, allowed, err = e.workspace.CheckWrite(path)
	} else {
		safePath, _, allowed, err = e.workspace.CheckRead(path)
	}
	if err != nil {
		return tc, err.Error(), false
	}
	if allowed {
		tc.Args = setToolPath(tc.Args, safePath)
		return tc, "", true
	}
	// Full-takeover mode: grant session-scoped workspace access without the
	// prompt, consistent with bypassing every other approval.
	if e.FullAccess() {
		rootPath, err := workspace.CandidateRoot(path)
		if err != nil {
			return tc, err.Error(), false
		}
		if _, err := e.workspace.AddSessionRoot(rootPath, access); err != nil {
			return tc, err.Error(), false
		}
		tc.Args = setToolPath(tc.Args, safePath)
		return tc, "", true
	}
	if confirmFn == nil {
		return tc, fmt.Sprintf("workspace access required for %s: %s", access, safePath), false
	}

	rootPath, err := workspace.CandidateRoot(path)
	if err != nil {
		return tc, err.Error(), false
	}
	approval := &protocol.ApprovalContext{
		Reason:    fmt.Sprintf("add %s workspace for %s", access, tc.Name),
		Scope:     protocol.ApprovalScopeProject,
		Workspace: rootPath,
	}
	if access == workspace.AccessReadWrite {
		approval.WritePaths = []string{rootPath}
	}
	req := protocol.NewApprovalRequest("workspace", map[string]any{
		"path":           rootPath,
		"access":         string(access),
		"requested_path": safePath,
	}, protocol.ConfirmKindPermission, approval)
	req.CallID = tc.ID
	reply := confirmFn(req)
	if !reply.Allowed {
		return tc, "cancelled", false
	}
	root, err := e.workspace.AddSessionRoot(rootPath, access)
	if err != nil {
		return tc, err.Error(), false
	}
	if reply.Remember {
		e.fnMu.RLock()
		store := e.permStore
		e.fnMu.RUnlock()
		if store != nil {
			_ = store.RememberWorkspaceRoot(permission.WorkspaceRoot{
				Path:   root.Path,
				Access: string(root.Access),
			})
		}
	}
	tc.Args = setToolPath(tc.Args, safePath)
	return tc, "", true
}

func fileToolAccess(toolName string) (workspace.Access, bool) {
	switch toolName {
	case "read", "list", "tree", "grep", "glob", "diff", "image_understand":
		return workspace.AccessReadOnly, true
	case "write", "edit":
		return workspace.AccessReadWrite, true
	default:
		return "", false
	}
}

func fileToolPath(tc core.ToolCallItem) string {
	if p, ok := tc.Args["path"].(string); ok && p != "" {
		return p
	}
	switch tc.Name {
	case "grep", "glob":
		return "."
	}
	return ""
}

func setToolPath(args map[string]any, path string) map[string]any {
	next := make(map[string]any, len(args)+1)
	for k, v := range args {
		next[k] = v
	}
	next["path"] = path
	return next
}
