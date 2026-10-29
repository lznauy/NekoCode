package acp

import (
	"context"
	"strings"

	controlruntime "nekocode/runtime"
)

func (s *server) handlePermission(ctx context.Context, sessionID string, view controlruntime.ApprovalView) {
	options := []map[string]any{{"optionId": "allow_once", "name": "Allow once", "kind": "allow_once"}}
	allowAlways := view.Approval != nil && view.Approval.Scope == controlruntime.ApprovalScopeProject
	if allowAlways {
		options = append(options, map[string]any{"optionId": "allow_always", "name": "Always allow for this project", "kind": "allow_always"})
	}
	options = append(options, map[string]any{"optionId": "reject_once", "name": "Reject", "kind": "reject_once"})
	toolCallID := view.CallID
	if toolCallID == "" {
		toolCallID = view.ToolCallHash
	}
	toolCall := map[string]any{"toolCallId": toolCallID, "title": view.ToolName, "status": "pending", "rawInput": view.Args}
	if summary := approvalSummary(view); summary != "" {
		toolCall["content"] = []any{map[string]any{
			"type": "content", "content": map[string]any{"type": "text", "text": summary},
		}}
	}
	var response permissionResponse
	err := s.conn.request(ctx, "session/request_permission", map[string]any{
		"sessionId": sessionID, "options": options, "toolCall": toolCall,
	}, &response)
	_ = s.backend.DecideApproval(context.Background(), view.ID, permissionDecision(response, err == nil, allowAlways))
}

func permissionDecision(response permissionResponse, valid, allowAlways bool) controlruntime.ApprovalDecision {
	if !valid {
		// An RPC failure carries no user decision, even if a response was partially
		// decoded before the error. Keep it separate from explicit rejection.
		return controlruntime.ApprovalDecision{Cancelled: true}
	}
	if response.Outcome.Outcome != "selected" {
		return controlruntime.ApprovalDecision{}
	}
	if response.Outcome.OptionID == "allow_once" {
		return controlruntime.ApprovalDecision{Allowed: true}
	}
	if response.Outcome.OptionID == "allow_always" && allowAlways {
		return controlruntime.ApprovalDecision{Allowed: true, Remember: true}
	}
	return controlruntime.ApprovalDecision{}
}

func approvalSummary(view controlruntime.ApprovalView) string {
	if view.Approval == nil {
		return ""
	}
	details := make([]string, 0, 10)
	appendValue := func(label, value string) {
		if value != "" {
			details = append(details, label+": "+value)
		}
	}
	appendValue("Risk", view.Approval.Risk)
	appendValue("Reason", view.Approval.Reason)
	appendValue("Capabilities", strings.Join(view.Approval.Capabilities, ", "))
	appendValue("Operations", strings.Join(view.Approval.Structures, ", "))
	appendValue("Write paths", strings.Join(view.Approval.WritePaths, ", "))
	appendValue("Workspace", view.Approval.Workspace)
	appendValue("Sandbox", view.Approval.Sandbox)
	if view.Approval.OutboundDomains != nil {
		domains := strings.Join(view.Approval.OutboundDomains, ", ")
		if domains == "" {
			domains = "none (all outbound network access denied)"
		}
		appendValue("Outbound domains", domains)
	}
	appendValue("Persistence scope", string(view.Approval.Scope))
	if view.Approval.Combined {
		details = append(details, "This approval combines the operation and its required capabilities.")
	}
	return strings.Join(details, "\n")
}
