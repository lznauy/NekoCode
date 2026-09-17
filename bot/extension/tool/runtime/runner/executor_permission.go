package runner

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tools "nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/protocol"
)

// escalationFailReason explains why tryPermissionEscalation could not satisfy
// the request. It drives the message shown back to the model so the model
// gets actionable, non-leaky feedback — never "go ask the user in chat".
type escalationFailReason int

const (
	failNotPermissionError escalationFailReason = iota // execErr was not a PermissionError
	failNotPrivileged                                  // tool has no privileged retry registered
	failDeniedByRule                                   // an explicit deny rule blocks this capability
	failNoConfirmFn                                    // no UI callback wired (subagent / headless)
	failUserDenied                                     // user dismissed the confirm dialog
	failRetryError                                     // ExecuteWithPermission ran but returned an error
)

// tryPermissionEscalation tries to satisfy a RequiredPermissionError raised
// by a privileged tool. Returns:
//   - output: the tool's output on success
//   - ok: true if the call was satisfied
//   - reason: which failure branch was taken (only meaningful when ok=false)
//   - causeErr: the error whose .Error() should be shown to the model
//     (the raw PermissionError for non-retry failures; the actual
//     ExecuteWithPermission error for failRetryError so the model isn't told
//     "permission required" after it already got approval)
func (e *Executor) tryPermissionEscalation(ctx context.Context, privileged tools.PrivilegedFunc, tc core.ToolCallItem, execErr error, confirmFn protocol.ConfirmFunc, preApproved escalationApproval, hasPreApproval bool) (output string, ok bool, reason escalationFailReason, causeErr error) {
	var permErr core.PermissionError
	if !errors.As(execErr, &permErr) {
		return "", false, failNotPermissionError, execErr
	}
	if privileged == nil {
		return "", false, failNotPrivileged, execErr
	}

	req := permErr.PermissionRequest()
	if e.permissionDenied(tc.Name, req) {
		return "", false, failDeniedByRule, execErr
	}
	// Full-takeover mode: escalate without any dialog, including capabilities
	// (like process.host) that manual mode never pre-approves. Deny rules
	// above still block.
	if e.FullAccess() {
		out, err := privileged(e.toolContext(ctx), tc.Args, req)
		if err == nil {
			return out, true, 0, nil
		}
		return "", false, failRetryError, err
	}
	if e.permissionAllowed(tc.Name, req) {
		out, err := privileged(e.toolContext(ctx), tc.Args, req)
		if err == nil {
			return out, true, 0, nil
		}
		return "", false, failRetryError, err
	}
	if confirmFn == nil {
		return "", false, failNoConfirmFn, execErr
	}
	if hasPreApproval && preApproved.requestCoveredBy(req) {
		// The unified dialog already showed this call's predicted capabilities.
		// Its call-ID-bound token authorizes the privileged retry directly;
		// only an explicit remembered decision is persisted for later calls.
		e.rememberPermission(tc.Name, req, preApproved.remember)
		out, err := privileged(e.toolContext(ctx), tc.Args, req)
		if err == nil {
			return out, true, 0, nil
		}
		return "", false, failRetryError, err
	}
	confirmReq := protocol.NewApprovalRequest(
		canonicalPermissionTool(tc.Name),
		protocol.ApprovalArgs(tc.Args),
		protocol.ConfirmKindPermission,
		approvalContextFromPermission(req),
	)
	confirmReq.CallID = tc.ID
	reply := confirmFn(confirmReq)
	if !reply.Allowed {
		return "", false, failUserDenied, execErr
	}
	if reply.Remember {
		e.rememberPermission(tc.Name, req, true)
	}
	out, err := privileged(e.toolContext(ctx), tc.Args, req)
	if err == nil {
		return out, true, 0, nil
	}
	return "", false, failRetryError, err
}

// permissionFailureMessage renders the error string shown to the model when
// a privileged bash/shell call could not be satisfied.
//
// Design rule: the confirm dialog is transparent to the model. The model
// never sees "wait for the prompt", "ask the user in chat", or any hint that
// a UI dialog exists — those instructions used to leak the dialog's existence
// and made the model reply in chat asking the user to click confirm, even
// though the dialog was already being handled by the UI. The model only ever
// sees one of:
//
//   - "permission denied by user"        — the user dismissed the dialog.
//   - "no approval available in this runtime" — headless / subagent runs
//     that have no UI; the model can stop retrying capabilities here.
//   - "denied by permission rule: ..."   — an explicit deny rule blocked it.
//   - "sandbox/privileged execution failed: <real error>" — the privileged
//     retry actually ran (sandbox opened up / host shell invoked) and that
//     *runtime* error is what the model should react to. Never the original
//     PermissionError "permission required: ..." text, which used to make
//     the model misread "permission required" as a prompt to ask the user
//     to authorize in chat — even though escalation had already been
//     attempted.
func permissionFailureMessage(tc core.ToolCallItem, execErr error, reason escalationFailReason, causeErr error) string {
	var permErr core.PermissionError
	if !errors.As(execErr, &permErr) {
		return execErr.Error()
	}
	req := permErr.PermissionRequest()
	caps := strings.Join(req.Capabilities, `", "`)

	// The escalation path ran and could not satisfy the request. Give the
	// model situation-appropriate, non-leaky feedback — never "retry with
	// capabilities", "wait for the prompt", or "ask the user in chat".
	switch reason {
	case failUserDenied:
		if caps != "" {
			return fmt.Sprintf("permission denied by user (capabilities: %s); do not retry the same capability request without a change in approach.", caps)
		}
		return "permission denied by user"
	case failNoConfirmFn:
		if caps != "" {
			return fmt.Sprintf("no approval available in this runtime for capabilities [%s]; retry without those capabilities or skip the action.", caps)
		}
		return "no approval available in this runtime"
	case failDeniedByRule:
		return fmt.Sprintf("denied by permission rule for capabilities [%s]", caps)
	case failRetryError:
		// ExecuteWithPermission actually ran (sandbox was opened up, host
		// was hit, ...) and returned a real runtime error. Surface THAT
		// error to the model — never the original "permission required: ..."
		// text. The original PermissionError reason contains the words
		// "permission required", which the model misreads as "ask the user
		// to authorize" and replies in chat asking the user to click the
		// confirm button — even though escalation already happened (and
		// the dialog, if any, was already handled). Wrap the real error
		// with a model-readable lead-in so the model reacts to the runtime
		// failure itself rather than to a phantom permission prompt.
		realErr := causeErr
		if realErr == nil {
			realErr = execErr
		}
		return fmt.Sprintf("sandbox/privileged execution failed: %s. Pick a different approach (e.g. drop the capability, run a read-only alternative, or skip).", realErr.Error())
	}

	// Default (failNotPermissionError / failNotPrivileged / zero value):
	// surface the raw error.
	return execErr.Error()
}

func (e *Executor) permissionDenied(toolName string, req core.PermissionRequest) bool {
	e.fnMu.RLock()
	store := e.permStore
	e.fnMu.RUnlock()
	if store == nil {
		return false
	}
	for _, tool := range permissionToolAliases(toolName) {
		if _, denied := store.Denied(tool, req); denied {
			return true
		}
	}
	return false
}

func (e *Executor) permissionAllowed(toolName string, req core.PermissionRequest) bool {
	e.fnMu.RLock()
	store := e.permStore
	e.fnMu.RUnlock()
	if store == nil {
		return false
	}
	for _, tool := range permissionToolAliases(toolName) {
		if _, ok := store.Match(tool, req); ok {
			return true
		}
	}
	return false
}

// rememberPermission persists a capability grant only for an explicit
// remembered approval. One-time approvals remain bound to the current call.
func (e *Executor) rememberPermission(toolName string, req core.PermissionRequest, persistStore bool) {
	// CapProcessHost must never be persisted — every unsandboxed host
	// execution requires an explicit user prompt, regardless of prior
	// grants. The store layer also enforces this, but we short-circuit
	// here to make the architectural intent explicit.
	for _, cap := range req.Capabilities {
		if cap == core.CapProcessHost {
			return
		}
	}
	e.fnMu.RLock()
	store := e.permStore
	e.fnMu.RUnlock()
	if store == nil || !persistStore {
		return
	}
	_ = store.Allow(canonicalPermissionTool(toolName), req)
}

func canonicalPermissionTool(toolName string) string {
	switch toolName {
	case "bash", "bg", "shell":
		return "shell"
	}
	return toolName
}

func permissionToolAliases(toolName string) []string {
	canonical := canonicalPermissionTool(toolName)
	out := []string{canonical}
	if toolName != canonical {
		out = append(out, toolName)
	}
	if canonical == "shell" {
		for _, legacy := range []string{"bash", "bg"} {
			seen := false
			for _, existing := range out {
				if existing == legacy {
					seen = true
					break
				}
			}
			if !seen {
				out = append(out, legacy)
			}
		}
	}
	return out
}

func approvalContextFromPermission(req core.PermissionRequest) *protocol.ApprovalContext {
	context := &protocol.ApprovalContext{
		Reason:       req.Reason,
		Capabilities: append([]string(nil), req.Capabilities...),
		Scope:        protocol.ApprovalScope(req.Scope),
		WritePaths:   permission.WritePathsFromRequest(req),
	}
	if workspace, ok := req.Details["workspace"].(string); ok {
		context.Workspace = workspace
	}
	if sandbox, ok := req.Details["sandbox"].(string); ok {
		context.Sandbox = sandbox
	}
	return context
}
