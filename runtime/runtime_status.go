package runtime

import (
	"context"
	"nekocode/runtime/internal/recording"
	"nekocode/runtime/internal/runstore"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

const metricsPublishInterval = 100 * time.Millisecond
const contextSampleInterval = time.Second

type contextView struct {
	mu           sync.Mutex
	snapshot     ContextSnapshot
	sampledAt    time.Time
	generation   uint64
	published    ContextSnapshot
	publishedRun RunID
}

func cloneContext(snapshot ContextSnapshot) ContextSnapshot {
	snapshot.Segments = slices.Clone(snapshot.Segments)
	return snapshot
}

func (r *Runtime) invalidateContext() {
	r.contextView.mu.Lock()
	r.contextView.sampledAt = time.Time{}
	r.contextView.generation++
	r.contextView.mu.Unlock()
}

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
	hasCommands := r.services.Commands.ExecuteCommand != nil || len(r.runtimeCommands) > 0
	r.mu.Unlock()
	return CapabilityManifest{
		Checkpoints:   r.services.Tools.Checkpoints != nil,
		ModelCatalog:  r.services.Models.ModelOptions != nil,
		SessionCreate: r.services.Sessions.NewSession != nil,
		SessionResume: r.services.Sessions.ResumeSession != nil,
		SessionDelete: r.services.Sessions.DeleteSession != nil,
		ToolCatalog:   r.services.Tools.ToolNames != nil, Rewind: r.services.Tools.Rewind != nil, ModelSelection: r.services.Models.SwitchSessionModel != nil, PermissionControl: r.services.Permissions.SetFullAccess != nil,
		Protocol: ProtocolVersion, Steering: r.services.Commands.Steer != nil,
		Commands: hasCommands,
		Metrics:  r.services.Lifecycle.Metrics != nil, Models: r.services.Models.CurrentModel != nil,
		Context: r.services.Context.ContextSnapshot != nil, Extensions: r.services.Skills.SkillManagementView != nil,
		Configuration: r.services.Config.ConfigView != nil, Sessions: r.services.Sessions.ListSessions != nil,
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
	service, closed := r.services.Models.CurrentModel, r.closed
	r.mu.Unlock()
	if closed || service == nil {
		return ModelSelection{}
	}
	return service()
}

// ToolNames reports the configured tool catalog without configuration secrets.
func (r *Runtime) ToolNames() []string {
	r.mu.Lock()
	service, closed := r.services.Tools.ToolNames, r.closed
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
	service, closed := r.services.Permissions.PermissionMode, r.closed
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
	service, closed := r.services.Commands.ExecuteLocalCommand, r.closed
	r.mu.Unlock()
	if closed || service == nil {
		return "", LocalCommandNotCommand
	}
	return service(ctx, input)
}

func (r *Runtime) ContextSnapshot() ContextSnapshot {
	r.mu.Lock()
	service, closed := r.services.Context.ContextSnapshot, r.closed
	r.mu.Unlock()
	if closed || service == nil {
		return ContextSnapshot{}
	}
	r.contextView.mu.Lock()
	if !r.contextView.sampledAt.IsZero() && time.Since(r.contextView.sampledAt) < contextSampleInterval {
		snapshot := cloneContext(r.contextView.snapshot)
		r.contextView.mu.Unlock()
		return snapshot
	}
	generation := r.contextView.generation
	r.contextView.mu.Unlock()

	// Service callbacks may call back into Runtime. Never hold a Runtime-owned
	// lock across them; an invalidation during sampling prevents caching the
	// potentially stale result.
	fresh := cloneContext(service())
	r.contextView.mu.Lock()
	if r.contextView.generation == generation {
		if r.contextView.sampledAt.IsZero() || time.Since(r.contextView.sampledAt) >= contextSampleInterval {
			r.contextView.snapshot = fresh
			r.contextView.sampledAt = time.Now()
		}
		fresh = cloneContext(r.contextView.snapshot)
	}
	r.contextView.mu.Unlock()
	return fresh
}

func (r *Runtime) WorkspaceChanges() WorkspaceChanges {
	r.mu.Lock()
	service, closed := r.services.Context.WorkspaceChanges, r.closed
	r.mu.Unlock()
	if closed || service == nil {
		return WorkspaceChanges{}
	}
	return service()
}

func (r *Runtime) MemoryView(scope MemoryScope) MemoryView {
	r.mu.Lock()
	service, closed := r.services.Context.MemoryView, r.closed
	r.mu.Unlock()
	if closed || service == nil {
		return MemoryView{}
	}
	return service(scope)
}

func (r *Runtime) SkillManagementView() SkillManagementView {
	r.mu.Lock()
	service, closed := r.services.Skills.SkillManagementView, r.closed
	r.mu.Unlock()
	if closed || service == nil {
		return SkillManagementView{}
	}
	return service()
}

func (r *Runtime) ConfigView() ConfigView {
	r.mu.Lock()
	service, closed := r.services.Config.ConfigView, r.closed
	r.mu.Unlock()
	if closed || service == nil {
		return ConfigView{}
	}
	return service()
}

func (r *Runtime) ResolveModelProfile(model ModelSpec) ModelProfile {
	r.mu.Lock()
	service, closed := r.services.Config.ResolveModelProfile, r.closed
	r.mu.Unlock()
	if closed || service == nil {
		return ModelProfile{}
	}
	return service(model)
}

func (r *Runtime) CurrentSessionID() string {
	r.mu.Lock()
	service, closed := r.services.Sessions.CurrentSessionID, r.closed
	r.mu.Unlock()
	if closed || service == nil {
		return ""
	}
	return service()
}

func (r *Runtime) ListSessions() []SessionMeta {
	r.mu.Lock()
	service, closed := r.services.Sessions.ListSessions, r.closed
	r.mu.Unlock()
	if closed || service == nil {
		return nil
	}
	return service()
}

func (r *Runtime) SessionMessages() []DisplayMessage {
	r.mu.Lock()
	service, closed := r.services.Sessions.SessionMessages, r.closed
	r.mu.Unlock()
	if closed || service == nil {
		return nil
	}
	return service()
}

func (r *Runtime) publishMetrics(runID RunID) {
	if r.services.Lifecycle.Metrics == nil {
		return
	}
	metrics := r.services.Lifecycle.Metrics()
	r.events.Publish(Event{
		RunID: runID, Type: EventMetricsUpdated, Source: SourceRef{Kind: "bot"},
		Payload: metrics,
	})
}

func (r *Runtime) startMetricsUpdates(runID RunID, lease *runLease) func() {
	if r.services.Lifecycle.Metrics == nil && r.services.Context.ContextSnapshot == nil {
		return func() {}
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	lease.guard(func() { r.publishContext(runID) })
	go func() {
		defer close(done)
		ticker := time.NewTicker(metricsPublishInterval)
		defer ticker.Stop()
		contextTicker := time.NewTicker(contextSampleInterval)
		defer contextTicker.Stop()
		for {
			select {
			case <-ticker.C:
				if !lease.guard(func() {
					r.publishMetrics(runID)
				}) {
					return
				}
			case <-contextTicker.C:
				if !lease.guard(func() { r.publishContext(runID) }) {
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

// publishContext reports the context window composition for live observation.
// It is separate from Metrics because it is the one place segment breakdown
// (system/tools/memory/... ) is available.
func (r *Runtime) publishContext(runID RunID) {
	if r.services.Context.ContextSnapshot == nil {
		return
	}
	snapshot := r.ContextSnapshot()
	r.contextView.mu.Lock()
	if r.contextView.publishedRun == runID && reflect.DeepEqual(r.contextView.published, snapshot) {
		r.contextView.mu.Unlock()
		return
	}
	r.contextView.publishedRun, r.contextView.published = runID, cloneContext(snapshot)
	r.contextView.mu.Unlock()
	r.events.Publish(Event{
		RunID: runID, Type: EventContextUpdated, Source: SourceRef{Kind: "bot"},
		Payload: snapshot,
	})
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
	if snapshot, ok := r.runs.Lookup(runID); ok {
		return snapshot, true
	}
	r.mu.Lock()
	run, ok := r.archivedRuns[runID]
	r.mu.Unlock()
	if !ok {
		return RunSnapshot{}, false
	}
	temporary := runstore.NewRunStore(1)
	if err := recording.WalkRecordedEventFile(run.Path, temporary.Record); err != nil {
		return RunSnapshot{}, false
	}
	snapshot, ok := temporary.Lookup(runID)
	if ok {
		r.runs.CacheRestored(snapshot)
	}
	return snapshot, ok
}

func (r *Runtime) Runs(limit int) []RunSnapshot {
	// Archive cache insertion order is not run chronology. Apply the caller's
	// limit only after merging and sorting, so an old lookup cannot hide a
	// newer live run that has no archive file yet.
	current := r.runs.List(0)
	if limit <= 0 {
		limit = recordedRunRestoreLimit
	}
	r.mu.Lock()
	archived := make(map[RunID]recording.RecordedRun, len(r.archivedRuns))
	for id, path := range r.archivedRuns {
		archived[id] = path
	}
	r.mu.Unlock()
	seen := make(map[RunID]bool, len(current))
	for _, run := range current {
		seen[run.ID] = true
	}
	ids := make([]RunID, 0, len(archived))
	for id := range archived {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return archived[ids[i]].Sequence > archived[ids[j]].Sequence })
	if len(ids) > limit {
		ids = ids[:limit]
	}
	for _, id := range ids {
		if !seen[id] {
			if run, ok := r.LookupRun(id); ok {
				current = append(current, run)
			}
		}
	}
	sort.SliceStable(current, func(i, j int) bool { return current[i].UpdatedAt.After(current[j].UpdatedAt) })
	if len(current) > limit {
		current = current[:limit]
	}
	return current
}

// Metrics returns the live context usage snapshot when the host supplies a
// metrics service, falling back to the last observed event snapshot. The
// cached value alone can go stale across session switches (it would report
// the previous session's usage after session/load).
func (r *Runtime) Metrics() MetricsSnapshot {
	r.mu.Lock()
	service, cached, closed := r.services.Lifecycle.Metrics, r.latestMetrics, r.closed
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
	service, closed := r.services.Commands.CommandMenu, r.closed
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
		runtimeMenu, _ := r.runtimeCommandMenu(ctx, "/")
		return mergeCommandMenus(menu, runtimeMenu), true
	}
	if service != nil {
		if menu, ok := service(ctx, trimmed); ok {
			return menu, true
		}
	}
	return r.runtimeCommandMenu(ctx, trimmed)
}

func (r *Runtime) runtimeCommandMenu(ctx context.Context, input string) (CommandMenu, bool) {
	parts := strings.Fields(strings.TrimSpace(input))
	if len(parts) == 0 {
		return CommandMenu{}, false
	}
	command := strings.ToLower(parts[0])
	if command == "/connect" && len(parts) >= 2 && len(parts) <= 3 {
		return r.connectors.CommandMenu(ctx, parts[1], parts[2:])
	}
	if len(parts) != 1 {
		return CommandMenu{}, false
	}
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
	service, closed := r.services.Tools.Checkpoints, r.closed
	r.mu.Unlock()
	if closed {
		return nil, protocolError(ErrorClosed, "checkpoints", "closed")
	}
	if service == nil {
		return nil, protocolError(ErrorUnsupported, "checkpoints", "capability unavailable")
	}
	return service()
}
