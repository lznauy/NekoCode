package runner

import (
	"context"
	"encoding/json"
	"nekocode/logger"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/protocol"
)

// PermissionEvent is a detached, JSON-shaped observation. It carries no
// authorization token and the observer cannot affect the approval decision.
type PermissionEvent struct {
	Denied bool
	Tool   string
	Args   map[string]any
	Reason string
}

func (e *Executor) SetPermissionObserver(fn func(context.Context, PermissionEvent)) {
	e.fnMu.Lock()
	e.permissionObserver = fn
	e.fnMu.Unlock()
}

func (e *Executor) emitPermission(ctx context.Context, event PermissionEvent) {
	e.fnMu.RLock()
	observer := e.permissionObserver
	e.fnMu.RUnlock()
	if observer == nil {
		return
	}
	// Both input maps and approval scopes must be detached from live authority.
	data, err := json.Marshal(event.Args)
	if err != nil {
		return
	}
	var snapshot map[string]any
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return
	}
	event.Args = snapshot
	defer func() {
		if recover() != nil {
			logger.Log("permission observer panicked; approval policy unchanged")
		}
	}()
	observer(ctx, event)
}

func (e *Executor) observeConfirm(ctx context.Context, confirm protocol.ConfirmFunc) protocol.ConfirmFunc {
	if confirm == nil {
		return nil
	}
	return func(request protocol.ConfirmRequest) protocol.ConfirmReply {
		args := map[string]any{"call_id": request.CallID, "source": "approval", "kind": request.Kind, "tool_args": protocol.ApprovalArgs(request.Args), "approval": request.Approval.Clone()}
		e.emitPermission(ctx, PermissionEvent{Tool: canonicalPermissionTool(request.ToolName), Args: args})
		reply := confirm(request)
		if !reply.Allowed && !reply.Cancelled {
			// Construct a fresh snapshot: the request observer cannot mutate denial
			// facts or the original request sent to the UI.
			args["source"] = "user"
			e.emitPermission(ctx, PermissionEvent{Denied: true, Tool: canonicalPermissionTool(request.ToolName), Args: args, Reason: "approval denied by user"})
		}
		return reply
	}
}

func (e *Executor) observePermissionDenial(ctx context.Context, call core.ToolCallItem, reason string, predicted *core.PermissionRequest) {
	tool, args := call.Effective()
	facts := map[string]any{"call_id": call.ID, "source": "rule", "tool_args": protocol.ApprovalArgs(args)}
	if predicted != nil {
		facts["approval"] = approvalContextFromPermission(*predicted)
	}
	e.emitPermission(ctx, PermissionEvent{Denied: true, Tool: canonicalPermissionTool(tool), Reason: reason, Args: facts})
}
