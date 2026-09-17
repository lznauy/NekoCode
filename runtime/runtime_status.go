package runtime

import (
	"context"
	"sort"
	"strings"
	"time"
)

const metricsPublishInterval = 100 * time.Millisecond

// CapabilityManifest lets an interaction surface discover optional features
// without probing methods or depending on a concrete bot.
type CapabilityManifest struct {
	Checkpoints       bool   `json:"checkpoints"`
	ModelCatalog      bool   `json:"model_catalog"`
	SessionCreate     bool   `json:"session_create"`
	SessionResume     bool   `json:"session_resume"`
	SessionDelete     bool   `json:"session_delete"`
	Protocol          string `json:"protocol"`
	ToolCatalog       bool   `json:"tool_catalog"`
	Rewind            bool   `json:"rewind"`
	ModelSelection    bool   `json:"model_selection"`
	PermissionControl bool   `json:"permission_control"`
	Steering          bool   `json:"steering"`
	Commands          bool   `json:"commands"`
	Metrics           bool   `json:"metrics"`
	Models            bool   `json:"models"`
	Context           bool   `json:"context"`
	Extensions        bool   `json:"extensions"`
	Configuration     bool   `json:"configuration"`
	Sessions          bool   `json:"sessions"`
	Connectors        bool   `json:"connectors"`
}

type RuntimeState string

const (
	RuntimeReady  RuntimeState = "ready"
	RuntimeBusy   RuntimeState = "busy"
	RuntimeClosed RuntimeState = "closed"
)

// RuntimeStatus is the small, stable health and lifecycle snapshot.
type RuntimeStatus struct {
	State        RuntimeState       `json:"state"`
	ActiveRun    RunID              `json:"active_run,omitempty"`
	RunStatus    RunStatus          `json:"run_status"`
	Capabilities CapabilityManifest `json:"capabilities"`
}

func (r *Runtime) Capabilities() CapabilityManifest {
	r.mu.Lock()
	hasCommands := r.services.ExecuteCommand != nil || len(r.runtimeCommands) > 0
	r.mu.Unlock()
	return CapabilityManifest{
		Checkpoints:   r.services.Checkpoints != nil,
		ModelCatalog:  r.services.ModelOptions != nil,
		SessionCreate: r.services.NewSession != nil,
		SessionResume: r.services.ResumeSession != nil,
		SessionDelete: r.services.DeleteSession != nil,
		ToolCatalog:   r.services.ToolNames != nil, Rewind: r.services.Rewind != nil, ModelSelection: r.services.SwitchSessionModel != nil, PermissionControl: r.services.SetFullAccess != nil,
		Protocol: ProtocolVersion, Steering: r.services.Steer != nil,
		Commands: hasCommands,
		Metrics:  r.services.Metrics != nil, Models: r.services.CurrentModel != nil,
		Context: r.services.ContextSnapshot != nil, Extensions: r.services.SkillManagementView != nil,
		Configuration: r.services.ConfigView != nil, Sessions: r.services.ListSessions != nil,
		Connectors: len(r.connectors.View().Connectors) > 0,
	}
}

func (r *Runtime) Status() RuntimeStatus {
	r.mu.Lock()
	state := RuntimeReady
	if r.closed {
		state = RuntimeClosed
	} else if r.status != RunIdle || r.mutating {
		state = RuntimeBusy
	}
	activeRun := r.currentRun
	if r.status == RunIdle {
		activeRun = ""
	}
	runStatus := r.status
	r.mu.Unlock()
	return RuntimeStatus{
		State: state, ActiveRun: activeRun, RunStatus: runStatus,
		Capabilities: r.Capabilities(),
	}
}

// Optional read models are queried directly through Runtime. Call
// Capabilities to discover availability; unavailable or closed capabilities
// return their zero value.
func (r *Runtime) CurrentModel() ModelSelection {
	r.mu.Lock()
	service, closed := r.services.CurrentModel, r.closed
	r.mu.Unlock()
	if closed || service == nil {
		return ModelSelection{}
	}
	return service()
}

// ToolNames reports the configured tool catalog without configuration secrets.
func (r *Runtime) ToolNames() []string {
	r.mu.Lock()
	service, closed := r.services.ToolNames, r.closed
	r.mu.Unlock()
	if closed || service == nil {
		return nil
	}
	return service()
}

// PermissionMode reports the permission mode ("manual"/"full"); "" when the
// runtime provides no permission service or is closed.
func (r *Runtime) PermissionMode() string {
	r.mu.Lock()
	service, closed := r.services.PermissionMode, r.closed
	r.mu.Unlock()
	if closed || service == nil {
		return ""
	}
	return service()
}

// ExecuteLocalCommand runs a during-task-safe command without a run
// lifecycle. Unlike StartRun it never checks the busy state: local commands
// do not touch run state by definition.
func (r *Runtime) ExecuteLocalCommand(ctx context.Context, input string) (string, LocalCommandResult) {
	r.mu.Lock()
	service, closed := r.services.ExecuteLocalCommand, r.closed
	r.mu.Unlock()
	if closed || service == nil {
		return "", LocalCommandNotCommand
	}
	return service(ctx, input)
}

func (r *Runtime) ContextSnapshot() ContextSnapshot {
	r.mu.Lock()
	service, closed := r.services.ContextSnapshot, r.closed
	r.mu.Unlock()
	if closed || service == nil {
		return ContextSnapshot{}
	}
	return service()
}

func (r *Runtime) WorkspaceChanges() WorkspaceChanges {
	r.mu.Lock()
	service, closed := r.services.WorkspaceChanges, r.closed
	r.mu.Unlock()
	if closed || service == nil {
		return WorkspaceChanges{}
	}
	return service()
}

func (r *Runtime) MemoryView(scope MemoryScope) MemoryView {
	r.mu.Lock()
	service, closed := r.services.MemoryView, r.closed
	r.mu.Unlock()
	if closed || service == nil {
		return MemoryView{}
	}
	return service(scope)
}

func (r *Runtime) SkillManagementView() SkillManagementView {
	r.mu.Lock()
	service, closed := r.services.SkillManagementView, r.closed
	r.mu.Unlock()
	if closed || service == nil {
		return SkillManagementView{}
	}
	return service()
}

func (r *Runtime) ConfigView() ConfigView {
	r.mu.Lock()
	service, closed := r.services.ConfigView, r.closed
	r.mu.Unlock()
	if closed || service == nil {
		return ConfigView{}
	}
	return service()
}

func (r *Runtime) ResolveModelProfile(model ModelSpec) ModelProfile {
	r.mu.Lock()
	service, closed := r.services.ResolveModelProfile, r.closed
	r.mu.Unlock()
	if closed || service == nil {
		return ModelProfile{}
	}
	return service(model)
}

func (r *Runtime) CurrentSessionID() string {
	r.mu.Lock()
	service, closed := r.services.CurrentSessionID, r.closed
	r.mu.Unlock()
	if closed || service == nil {
		return ""
	}
	return service()
}

func (r *Runtime) ListSessions() []SessionMeta {
	r.mu.Lock()
	service, closed := r.services.ListSessions, r.closed
	r.mu.Unlock()
	if closed || service == nil {
		return nil
	}
	return service()
}

func (r *Runtime) SessionMessages() []DisplayMessage {
	r.mu.Lock()
	service, closed := r.services.SessionMessages, r.closed
	r.mu.Unlock()
	if closed || service == nil {
		return nil
	}
	return service()
}

func (r *Runtime) publishMetrics(runID RunID) {
	if r.services.Metrics == nil {
		return
	}
	metrics := r.services.Metrics()
	r.events.Publish(Event{
		RunID: runID, Type: EventMetricsUpdated, Source: SourceRef{Kind: "bot"},
		Payload: metrics,
	})
}

func (r *Runtime) startMetricsUpdates(runID RunID, lease *runLease) func() {
	if r.services.Metrics == nil {
		return func() {}
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(metricsPublishInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if !lease.guard(func() { r.publishMetrics(runID) }) {
					return
				}
			case <-stop:
				return
			}
		}
	}()
	return func() {
		close(stop)
		<-done
	}
}

func (r *Runtime) recordMetrics(event Event) {
	if event.Type != EventMetricsUpdated {
		return
	}
	metrics, ok := event.Payload.(MetricsSnapshot)
	if !ok {
		return
	}
	r.mu.Lock()
	r.latestMetrics = metrics
	r.mu.Unlock()
}

func (r *Runtime) CurrentRun() (RunSnapshot, bool) {
	return r.runs.Current()
}

func (r *Runtime) LookupRun(runID RunID) (RunSnapshot, bool) {
	return r.runs.Lookup(runID)
}

func (r *Runtime) Runs(limit int) []RunSnapshot {
	return r.runs.List(limit)
}

// Metrics returns the live context usage snapshot when the host supplies a
// metrics service, falling back to the last observed event snapshot. The
// cached value alone can go stale across session switches (it would report
// the previous session's usage after session/load).
func (r *Runtime) Metrics() MetricsSnapshot {
	r.mu.Lock()
	service, cached, closed := r.services.Metrics, r.latestMetrics, r.closed
	r.mu.Unlock()
	if closed || service == nil {
		return cached
	}
	return service()
}

func (r *Runtime) CommandCatalog() []string {
	seen := make(map[string]bool)
	var names []string
	for _, prefix := range []string{"/", "$"} {
		menu, ok := r.CommandMenu(context.Background(), prefix)
		if !ok {
			continue
		}
		for _, item := range menu.Items {
			if item.Value == "" || seen[item.Value] {
				continue
			}
			seen[item.Value] = true
			names = append(names, item.Value)
		}
	}
	sort.Strings(names)
	return names
}

func (r *Runtime) CommandMenu(ctx context.Context, input string) (CommandMenu, bool) {
	if err := ctx.Err(); err != nil {
		return CommandMenu{}, false
	}
	r.mu.Lock()
	service, closed := r.services.CommandMenu, r.closed
	r.mu.Unlock()
	if closed {
		return CommandMenu{}, false
	}
	trimmed := strings.TrimSpace(input)
	if trimmed == "/" {
		var menu CommandMenu
		if service != nil {
			menu, _ = service(ctx, "/")
		}
		runtimeMenu, _ := r.runtimeCommandMenu("/")
		return mergeCommandMenus(menu, runtimeMenu), true
	}
	if service != nil {
		if menu, ok := service(ctx, trimmed); ok {
			return menu, true
		}
	}
	return r.runtimeCommandMenu(trimmed)
}

func (r *Runtime) runtimeCommandMenu(input string) (CommandMenu, bool) {
	parts := strings.Fields(strings.TrimSpace(input))
	if len(parts) != 1 {
		return CommandMenu{}, false
	}
	command := strings.ToLower(parts[0])
	if command == "/" {
		r.mu.Lock()
		items := make([]CommandMenuItem, 0, len(r.runtimeCommands))
		for name, entry := range r.runtimeCommands {
			items = append(items, CommandMenuItem{
				Value: "/" + name, Label: "/" + name, Description: entry.description,
			})
		}
		r.mu.Unlock()
		sort.Slice(items, func(i, j int) bool { return items[i].Value < items[j].Value })
		return CommandMenu{Title: "Commands", Empty: "No commands available", Items: items}, true
	}
	if command != "/connect" && command != "/disconnect" {
		return CommandMenu{}, false
	}
	view := r.connectors.View()
	items := make([]CommandMenuItem, 0, len(view.Connectors))
	for _, connector := range view.Connectors {
		if command == "/disconnect" && !connector.Initialized {
			continue
		}
		description := connector.Status
		if description == "" {
			description = "registered"
		}
		items = append(items, CommandMenuItem{
			Value: command + " " + connector.Name, Label: connector.Name,
			Description: description, Submit: command == "/disconnect",
		})
	}
	if command == "/connect" {
		return CommandMenu{Title: "Choose connector", Empty: "No connectors registered", Items: items}, true
	}
	return CommandMenu{Title: "Disconnect connector", Empty: "No active connectors", Items: items}, true
}

func mergeCommandMenus(menus ...CommandMenu) CommandMenu {
	merged := CommandMenu{Title: "Commands", Empty: "No commands available"}
	seen := make(map[string]bool)
	for _, menu := range menus {
		for _, item := range menu.Items {
			if item.Value == "" || seen[item.Value] {
				continue
			}
			seen[item.Value] = true
			merged.Items = append(merged.Items, item)
		}
	}
	sort.Slice(merged.Items, func(i, j int) bool { return merged.Items[i].Value < merged.Items[j].Value })
	return merged
}

// Checkpoints returns structured rewind points and preserves lookup failures.
func (r *Runtime) Checkpoints() ([]CheckpointInfo, error) {
	r.mu.Lock()
	service, closed := r.services.Checkpoints, r.closed
	r.mu.Unlock()
	if closed {
		return nil, protocolError(ErrorClosed, "checkpoints", "closed")
	}
	if service == nil {
		return nil, protocolError(ErrorUnsupported, "checkpoints", "capability unavailable")
	}
	return service()
}
