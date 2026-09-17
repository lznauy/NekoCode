package runtime

import (
	"context"
	"fmt"
)

// Services explicitly describes the optional application functions attached
// to a Runner. The composition root supplies this value once; Runtime and
// transports do not discover capabilities through type assertions.
type Services struct {
	Checkpoints            func() ([]CheckpointInfo, error)
	ToolNames              func() []string
	Rewind                 func(string) (string, error)
	ExecuteCommand         func(ctx context.Context, input string, host RunHost) (CommandResult, error)
	ExecuteLocalCommand    func(context.Context, string) (string, LocalCommandResult)
	CommandMenu            func(context.Context, string) (CommandMenu, bool)
	Steer                  func(ctx context.Context, message string) error
	Metrics                func() MetricsSnapshot
	CurrentModel           func() ModelSelection
	PermissionMode         func() string
	SwitchModel            func(string) (ModelSelection, error)
	SwitchSessionModel     func(string) (ModelSelection, error)
	ModelOptions           func() ([]ModelOption, string)
	SetReasoningEffort     func(string) error
	SetSessionReasoning    func(string) error
	SetFullAccess          func(bool)
	ContextSnapshot        func() ContextSnapshot
	WorkspaceChanges       func() WorkspaceChanges
	MemoryView             func(MemoryScope) MemoryView
	SkillManagementView    func() SkillManagementView
	SelectSkill            func(string) error
	ClearSelectedSkill     func()
	RefreshSkillManagement func() SkillManagementView
	SetPluginEnabled       func(string, bool) (SkillManagementView, error)
	ConfigView             func() ConfigView
	ResolveModelProfile    func(ModelSpec) ModelProfile
	ApplyConfig            func(ConfigView) (ConfigView, error)
	CurrentSessionID       func() string
	ListSessions           func() []SessionMeta
	SessionMessages        func() []DisplayMessage
	ResumeSession          func(string) error
	NewSession             func() (SessionMeta, error)
	DeleteSession          func(string) error
	ReplaceMCPServers      func(context.Context, string, []MCPServerSpec) error
	Close                  func() error
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
		r.mu.Lock()
		r.mutating = false
		r.mu.Unlock()
	}()
	return fn()
}

func (r *Runtime) SwitchModel(name string) (selection ModelSelection, err error) {
	err = r.mutation("switch_model", r.services.SwitchModel != nil, func() error {
		selection, err = r.services.SwitchModel(name)
		return err
	})
	return selection, err
}

// SwitchSessionModel changes the active model without persisting a global
// preference. Session-aware transports own restoring it at session switches.
func (r *Runtime) SwitchSessionModel(name string) (selection ModelSelection, err error) {
	err = r.mutation("switch_session_model", r.services.SwitchSessionModel != nil, func() error {
		selection, err = r.services.SwitchSessionModel(name)
		return err
	})
	return selection, err
}

// ModelOptions reports the selectable model configurations and the name of
// the active one. It is a read-only projection for session config surfaces.
func (r *Runtime) ModelOptions() ([]ModelOption, string) {
	r.mu.Lock()
	service, closed := r.services.ModelOptions, r.closed
	r.mu.Unlock()
	if closed || service == nil {
		return nil, ""
	}
	return service()
}

// SetReasoningEffort changes the active model's reasoning effort. It is
// rejected while a run is active.
func (r *Runtime) SetReasoningEffort(effort string) error {
	return r.mutation("set_reasoning_effort", r.services.SetReasoningEffort != nil, func() error {
		return r.services.SetReasoningEffort(effort)
	})
}

// SetSessionReasoning changes reasoning depth without persisting it globally.
func (r *Runtime) SetSessionReasoning(effort string) error {
	return r.mutation("set_session_reasoning", r.services.SetSessionReasoning != nil, func() error {
		return r.services.SetSessionReasoning(effort)
	})
}

// SetFullAccess toggles the full-takeover permission mode. It is rejected
// while a run is active.
func (r *Runtime) SetFullAccess(on bool) error {
	return r.mutation("set_full_access", r.services.SetFullAccess != nil, func() error {
		r.services.SetFullAccess(on)
		return nil
	})
}

func (r *Runtime) SelectSkill(name string) error {
	return r.mutation("select_skill", r.services.SelectSkill != nil, func() error {
		return r.services.SelectSkill(name)
	})
}

func (r *Runtime) ClearSelectedSkill() error {
	return r.mutation("clear_selected_skill", r.services.ClearSelectedSkill != nil, func() error {
		r.services.ClearSelectedSkill()
		return nil
	})
}

func (r *Runtime) RefreshSkillManagement() (view SkillManagementView, err error) {
	err = r.mutation("refresh_extensions", r.services.RefreshSkillManagement != nil, func() error {
		view = r.services.RefreshSkillManagement()
		return nil
	})
	return view, err
}

func (r *Runtime) SetPluginEnabled(name string, enabled bool) (view SkillManagementView, err error) {
	err = r.mutation("set_plugin_enabled", r.services.SetPluginEnabled != nil, func() error {
		view, err = r.services.SetPluginEnabled(name, enabled)
		return err
	})
	return view, err
}

func (r *Runtime) ApplyConfig(config ConfigView) (view ConfigView, err error) {
	err = r.mutation("apply_config", r.services.ApplyConfig != nil, func() error {
		view, err = r.services.ApplyConfig(config)
		return err
	})
	return view, err
}

func (r *Runtime) ResumeSession(id string) error {
	err := r.mutation("resume_session", r.services.ResumeSession != nil, func() error {
		return r.services.ResumeSession(id)
	})
	if err == nil {
		r.publishSessionChanged()
	}
	return err
}

func (r *Runtime) NewSession() (session SessionMeta, err error) {
	err = r.mutation("new_session", r.services.NewSession != nil, func() error {
		session, err = r.services.NewSession()
		return err
	})
	if err == nil {
		r.publishSessionChanged()
	}
	return session, err
}

func (r *Runtime) DeleteSession(id string) error {
	err := r.mutation("delete_session", r.services.DeleteSession != nil, func() error {
		return r.services.DeleteSession(id)
	})
	if err == nil {
		r.publishSessionChanged()
	}
	return err
}

// ReplaceMCPServers atomically replaces transport-supplied MCP servers owned
// by source. The request context cancels process startup and discovery.
func (r *Runtime) ReplaceMCPServers(ctx context.Context, source string, servers []MCPServerSpec) error {
	return r.mutation("replace_mcp_servers", r.services.ReplaceMCPServers != nil, func() error {
		return r.services.ReplaceMCPServers(ctx, source, servers)
	})
}

func (r *Runtime) publishSessionChanged() {
	sessionID := ""
	if r.services.CurrentSessionID != nil {
		sessionID = r.services.CurrentSessionID()
	}
	r.events.Publish(Event{
		Type: EventSessionChanged, Source: SourceRef{Kind: "runtime"},
		Payload: SessionPayload{ID: sessionID},
	})
}

// Rewind restores checkpointed workspace files while the runtime is idle.
func (r *Runtime) Rewind(id string) (message string, err error) {
	err = r.mutation("rewind", r.services.Rewind != nil, func() error { message, err = r.services.Rewind(id); return err })
	return
}
