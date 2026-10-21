package runtime

import (
	"context"
	"errors"
	"fmt"
)

// ImageAttachmentsEnabled reports whether the active runtime can consume
// pasted image attachments: natively (vision model) or through a configured
// image-understanding tool.
func (r *Runtime) ImageAttachmentsEnabled() bool {
	r.mu.Lock()
	service, closed := r.services.Models.ImageAttachmentsEnabled, r.closed
	r.mu.Unlock()
	return !closed && service != nil && service()
}

// VisionEnabled reports whether the active model accepts native image input,
// in which case pasted images are encoded as image content parts instead of
// only being reachable through the image_understand tool.
func (r *Runtime) VisionEnabled() bool {
	r.mu.Lock()
	service, closed := r.services.Models.VisionEnabled, r.closed
	r.mu.Unlock()
	return !closed && service != nil && service()
}

func (r *Runtime) mutation(op string, supported bool, fn func() error) error {
	r.mutationMu.Lock()
	defer r.mutationMu.Unlock()

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return protocolError(ErrorClosed, op, "closed")
	}
	if !supported {
		r.mu.Unlock()
		return protocolError(ErrorUnsupported, op, "capability unavailable")
	}
	if r.status != RunIdle {
		r.mu.Unlock()
		return protocolError(ErrorBusy, op, fmt.Sprintf("run %s is %s", r.currentRun, r.status))
	}
	r.mutating = true
	r.mu.Unlock()
	defer func() {
		r.invalidateContext()
		r.mu.Lock()
		r.mutating = false
		r.mu.Unlock()
	}()
	return fn()
}

func (r *Runtime) SwitchModel(name string) (selection ModelSelection, err error) {
	err = r.mutation("switch_model", r.services.Models.SwitchModel != nil, func() error {
		selection, err = r.services.Models.SwitchModel(name)
		return err
	})
	return selection, err
}

// SwitchSessionModel changes the active model without persisting a global
// preference. Session-aware transports own restoring it at session switches.
func (r *Runtime) SwitchSessionModel(name string) (selection ModelSelection, err error) {
	err = r.mutation("switch_session_model", r.services.Models.SwitchSessionModel != nil, func() error {
		selection, err = r.services.Models.SwitchSessionModel(name)
		return err
	})
	return selection, err
}

// ModelOptions reports the selectable model configurations and the name of
// the active one. It is a read-only projection for session config surfaces.
func (r *Runtime) ModelOptions() ([]ModelOption, string) {
	r.mu.Lock()
	service, closed := r.services.Models.ModelOptions, r.closed
	r.mu.Unlock()
	if closed || service == nil {
		return nil, ""
	}
	return service()
}

// SetReasoningEffort changes the active model's reasoning effort. It is
// rejected while a run is active.
func (r *Runtime) SetReasoningEffort(effort string) error {
	return r.mutation("set_reasoning_effort", r.services.Models.SetReasoningEffort != nil, func() error {
		return r.services.Models.SetReasoningEffort(effort)
	})
}

// SetSessionReasoning changes reasoning depth without persisting it globally.
func (r *Runtime) SetSessionReasoning(effort string) error {
	return r.mutation("set_session_reasoning", r.services.Models.SetSessionReasoning != nil, func() error {
		return r.services.Models.SetSessionReasoning(effort)
	})
}

// SetFullAccess toggles the full-takeover permission mode. It is rejected
// while a run is active.
func (r *Runtime) SetFullAccess(on bool) error {
	return r.mutation("set_full_access", r.services.Permissions.SetFullAccess != nil, func() error {
		r.services.Permissions.SetFullAccess(on)
		return nil
	})
}

func (r *Runtime) SelectSkill(name string) error {
	return r.mutation("select_skill", r.services.Skills.SelectSkill != nil, func() error {
		return r.services.Skills.SelectSkill(name)
	})
}

func (r *Runtime) ClearSelectedSkill() error {
	return r.mutation("clear_selected_skill", r.services.Skills.ClearSelectedSkill != nil, func() error {
		r.services.Skills.ClearSelectedSkill()
		return nil
	})
}

func (r *Runtime) RefreshSkillManagement() (view SkillManagementView, err error) {
	err = r.mutation("refresh_extensions", r.services.Skills.RefreshSkillManagement != nil, func() error {
		view = r.services.Skills.RefreshSkillManagement()
		return nil
	})
	return view, err
}

func (r *Runtime) SetPluginEnabled(name string, enabled bool) (view SkillManagementView, err error) {
	err = r.mutation("set_plugin_enabled", r.services.Skills.SetPluginEnabled != nil, func() error {
		view, err = r.services.Skills.SetPluginEnabled(name, enabled)
		return err
	})
	return view, err
}

func (r *Runtime) ApplyConfig(config ConfigView) (view ConfigView, err error) {
	err = r.mutation("apply_config", r.services.Config.ApplyConfig != nil, func() error {
		view, err = r.services.Config.ApplyConfig(config)
		return err
	})
	return view, err
}

func (r *Runtime) ResumeSession(id string) error {
	err := r.mutation("resume_session", r.services.Sessions.ResumeSession != nil, func() error {
		return r.services.Sessions.ResumeSession(id)
	})
	if err == nil || mutationCommitted(err) {
		r.publishSessionChanged("resume", id, true)
	}
	return err
}

func (r *Runtime) NewSession() (session SessionMeta, err error) {
	err = r.mutation("new_session", r.services.Sessions.NewSession != nil, func() error {
		session, err = r.services.Sessions.NewSession()
		return err
	})
	if err == nil || mutationCommitted(err) {
		r.publishSessionChanged("new", session.ID, true)
	}
	return session, err
}

func (r *Runtime) DeleteSession(id string) error {
	previousID := ""
	err := r.mutation("delete_session", r.services.Sessions.DeleteSession != nil, func() error {
		if r.services.Sessions.CurrentSessionID != nil {
			previousID = r.services.Sessions.CurrentSessionID()
		}
		return r.services.Sessions.DeleteSession(id)
	})
	if err == nil || mutationCommitted(err) {
		r.publishSessionChanged("delete", id, previousID == id)
	}
	return err
}

// ReplaceMCPServers atomically replaces transport-supplied MCP servers owned
// by source. The request context cancels process startup and discovery.
func (r *Runtime) ReplaceMCPServers(ctx context.Context, source string, servers []MCPServerSpec) error {
	return r.mutation("replace_mcp_servers", r.services.MCP.ReplaceMCPServers != nil, func() error {
		return r.services.MCP.ReplaceMCPServers(ctx, source, servers)
	})
}

func (r *Runtime) publishSessionChanged(reason, changedID string, currentChanged bool) {
	r.events.Publish(Event{
		Type: EventSessionChanged, Source: SourceRef{Kind: "runtime"},
		Payload: r.sessionChangedPayload(reason, changedID, currentChanged),
	})
}

func (r *Runtime) sessionChangedPayload(reason, changedID string, currentChanged bool) SessionPayload {
	sessionID := ""
	if r.services.Sessions.CurrentSessionID != nil {
		sessionID = r.services.Sessions.CurrentSessionID()
	}
	return SessionPayload{ID: sessionID, Reason: reason, ChangedID: changedID, CurrentChanged: currentChanged}
}

func mutationCommitted(err error) bool {
	type committed interface{ MutationCommitted() bool }
	var target committed
	return errors.As(err, &target) && target.MutationCommitted()
}

// Rewind restores checkpointed workspace files while the runtime is idle.
func (r *Runtime) Rewind(id string) (message string, err error) {
	err = r.mutation("rewind", r.services.Tools.Rewind != nil, func() error { message, err = r.services.Tools.Rewind(id); return err })
	return
}

func (r *Runtime) MCPAuthorizationAction(name, action string) error {
	// Deliberately not wrapped in mutation: authorization starts or cancels a
	// background browser flow that touches no run state, and the 5-minute
	// authorization window regularly overlaps a running task.
	r.mu.Lock()
	closed, supported := r.closed, r.services.MCP.MCPAuthorizationAction != nil
	r.mu.Unlock()
	if closed {
		return protocolError(ErrorClosed, "mcp_authorization", "closed")
	}
	if !supported {
		return protocolError(ErrorUnsupported, "mcp_authorization", "capability unavailable")
	}
	return r.services.MCP.MCPAuthorizationAction(name, action)
}
