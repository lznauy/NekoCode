// Package mcp implements MCP clients over stdio and Streamable HTTP with OAuth.
//
// Manager is the package entry point: it owns MCP server processes, health,
// and tool discovery. Tools reach the model exclusively through the
// constant-schema capability proxy (capability.go) so the provider-visible
// tool list never changes with MCP inventory.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Server health statuses reported by Manager.Health.
const (
	StatusStarting     = "starting"
	StatusReady        = "ready"
	StatusError        = "error"
	StatusAuthRequired = "auth_required"
	StatusAuthorizing  = "authorizing"
)

// Health reports the runtime state of one managed server.
type Health struct {
	AuthURL   string
	Owner     string
	Status    string
	Error     string
	ToolCount int
}

// server is a managed connection and the tools discovered on it.
type server struct {
	name        string
	client      *client
	tools       []toolDef
	cancel      context.CancelFunc
	overrideGen uint64 // auth override generation consumed by this start
}

// authOverride carries owner-scoped interactive login state across server
// rebuilds: AuthorizationAction stores it, every (re)registration merges it,
// and it is consumed by the start that used it so later reconnects stay
// non-interactive. gen guards against a stale startup consuming a newer
// override (e.g. a second login while the previous start is still winding down).
type authOverride struct {
	interactive bool
	scopes      []string
	gen         uint64
}

// Manager owns the lifecycle of every MCP server connection: spawning
// processes, performing the initialize handshake, and holding the tools for
// the capability proxy to route to. Servers have a stable owner ID separate
// from their user-facing name.
type Manager struct {
	mu            sync.Mutex
	servers       map[string]*server
	owners        map[string]string
	health        map[string]Health
	authOverrides map[string]authOverride
	authorizing   map[string]string
	authNotifier  func(message string)
	overrideSeq   uint64
	ctx           context.Context
	cancel        context.CancelFunc
	closed        bool
	wg            sync.WaitGroup
}

// New creates an empty Manager that owns MCP server lifecycles.
func New() *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		servers:       make(map[string]*server),
		owners:        make(map[string]string),
		health:        make(map[string]Health),
		authOverrides: make(map[string]authOverride),
		authorizing:   make(map[string]string),
		ctx:           ctx,
		cancel:        cancel,
	}
	go m.watchAuthTransitions()
	return m
}

// SetAuthNotifier registers a callback fired when an in-progress browser
// authorization finishes (successfully or not), so UIs can inform the user
// without them re-checking status. The callback must not call back into the
// Manager synchronously.
func (m *Manager) SetAuthNotifier(fn func(message string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.authNotifier = fn
}

// watchAuthTransitions publishes authorization links and final outcomes,
// including failures during discovery before a browser link exists.
func (m *Manager) watchAuthTransitions() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
		}
		var messages []string
		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			return
		}
		fn := m.authNotifier
		health := m.healthLocked()
		seen := make(map[string]bool, len(health))
		for name, state := range health {
			seen[name] = true
			lastURL, pending := m.authorizing[name]
			if state.Status == StatusAuthorizing {
				if lastURL != state.AuthURL && fn != nil && state.AuthURL != "" {
					messages = append(messages, name+" 授权链接（浏览器未打开时在本机打开）：\n"+state.AuthURL)
				}
				m.authorizing[name] = state.AuthURL
				continue
			}
			// A successful authorize is followed by a plain reconnect
			// (starting → ready); keep the flag through it.
			if state.Status == StatusStarting && pending {
				continue
			}
			if !pending {
				continue
			}
			delete(m.authorizing, name)
			if fn == nil {
				continue
			}
			switch state.Status {
			case StatusReady:
				messages = append(messages, fmt.Sprintf("%s 授权成功 · %d 个工具", name, state.ToolCount))
			case StatusError:
				messages = append(messages, fmt.Sprintf("%s 授权失败：%s", name, state.Error))
			case StatusAuthRequired:
				messages = append(messages, fmt.Sprintf("%s 授权未完成，可在 /mcp 菜单重试", name))
			}
		}
		// A server removed mid-flow (disabled, config change) ends its flow
		// silently: drop the pending flag without notifying.
		for name := range m.authorizing {
			if !seen[name] {
				delete(m.authorizing, name)
			}
		}
		m.mu.Unlock()
		for _, message := range messages {
			fn(message)
		}
	}
}

// applyAuthOverrideLocked merges the stored interactive login state for id
// into cfg. Caller holds m.mu. The override's generation is returned so the
// consuming start can later retire exactly that override.
func (m *Manager) applyAuthOverrideLocked(id string, cfg ServerConfig) (ServerConfig, uint64) {
	o, ok := m.authOverrides[id]
	if !ok {
		return cfg, 0
	}
	cfg.interactive = o.interactive
	if len(o.scopes) > 0 {
		cfg.authorizationScopes = append([]string(nil), o.scopes...)
	}
	return cfg, o.gen
}

// consumeAuthOverrideLocked retires the override with the given generation,
// leaving a newer override (another login in flight) untouched.
func (m *Manager) consumeAuthOverrideLocked(id string, gen uint64) {
	if gen == 0 {
		return
	}
	if o, ok := m.authOverrides[id]; ok && o.gen == gen {
		delete(m.authOverrides, id)
	}
}

// Add starts or replaces a server owned by id. Name remains the MCP tool
// prefix visible to the model.
func (m *Manager) Add(ctx context.Context, id, name string, cfg ServerConfig) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return fmt.Errorf("manager is closed")
	}
	if owner := m.owners[name]; owner != "" && owner != id {
		m.mu.Unlock()
		return fmt.Errorf("server name %q is already owned by %s", name, owner)
	}
	merged, _ := m.applyAuthOverrideLocked(id, cfg)
	interactive := merged.interactive
	m.mu.Unlock()
	if interactive {
		// An interactive start waits on a browser callback for minutes while
		// holding the remote client's own locks. It must never hold m.mu:
		// the watcher that publishes the auth link, health polls and the
		// cancel path all need it, so running inline would deadlock the
		// whole Manager. AddBackground runs the start outside the lock.
		return m.AddBackground(id, name, cfg)
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return fmt.Errorf("manager is closed")
	}
	if owner := m.owners[name]; owner != "" && owner != id {
		m.mu.Unlock()
		return fmt.Errorf("server name %q is already owned by %s", name, owner)
	}
	m.removeLocked(id)

	merged, gen := m.applyAuthOverrideLocked(id, cfg)
	if merged.interactive {
		// A login override may have landed between the pre-check above and
		// this re-lock (concurrent AuthorizationAction). Re-check under the
		// lock: an interactive start waits on a browser callback for minutes
		// and must never run while m.mu is held — the watcher that publishes
		// the auth link, health polls and the cancel path all need it.
		m.mu.Unlock()
		return m.AddBackground(id, name, cfg)
	}
	client := newClient(name, merged)
	m.servers[id] = &server{name: name, client: client, overrideGen: gen}
	m.owners[name] = id
	m.health[name] = Health{Status: StatusStarting}

	if err := client.Start(ctx); err != nil {
		m.removeLocked(id)
		// The interactive flag only governs the authorize pass inside Start.
		m.consumeAuthOverrideLocked(id, gen)
		m.mu.Unlock()
		return fmt.Errorf("start: %w", err)
	}

	defs, err := client.ListTools(ctx)
	if err != nil {
		_ = client.Close()
		m.removeLocked(id)
		m.consumeAuthOverrideLocked(id, gen)
		m.mu.Unlock()
		return fmt.Errorf("list tools: %w", err)
	}

	serverTools := append([]toolDef(nil), defs...)
	m.servers[id].tools = serverTools
	m.health[name] = Health{Status: StatusReady, ToolCount: len(serverTools)}
	m.consumeAuthOverrideLocked(id, gen)
	m.mu.Unlock()
	return nil
}

// Replace atomically replaces oldIDs with registrations. New processes are
// fully initialized before the live routing tables change; any failure leaves
// the previous set untouched.
func (m *Manager) Replace(ctx context.Context, oldIDs []string, registrations []Registration) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return fmt.Errorf("manager is closed")
	}

	old := make(map[string]struct{}, len(oldIDs))
	expectedOld := make(map[string]*server, len(oldIDs))
	for _, id := range oldIDs {
		old[id] = struct{}{}
		expectedOld[id] = m.servers[id]
	}
	if err := m.validateReplacementLocked(old, registrations); err != nil {
		m.mu.Unlock()
		return err
	}
	m.mu.Unlock()
	stageCtx, cancelStage := context.WithCancel(ctx)
	stopStage := context.AfterFunc(m.ctx, cancelStage)
	defer func() {
		stopStage()
		cancelStage()
	}()

	// Starting a remote transport may wait on network I/O for minutes. Build
	// the replacement set without holding the manager lock so health, cancel,
	// close, and authorization operations remain responsive.
	staged := make(map[string]*server, len(registrations))
	cleanupStaged := func() {
		for _, item := range staged {
			_ = item.client.Close()
		}
	}
	for _, registration := range registrations {
		if err := stageCtx.Err(); err != nil {
			cleanupStaged()
			return err
		}
		// A session replacement never opens a browser flow. Interactive login is
		// an explicit manager action and is not part of a transport refresh.
		stagedCfg := registration.Config
		stagedCfg.interactive = false
		stagedCfg.authorizationScopes = nil
		client := newClient(registration.Name, stagedCfg)
		item := &server{name: registration.Name, client: client}
		staged[registration.ID] = item
		// Some transports keep internal request contexts after Connect starts.
		// Closing the staged client propagates manager/caller cancellation through
		// the client's lifetime context as well as the Connect context.
		stopClient := context.AfterFunc(stageCtx, func() { _ = client.Close() })
		if err := client.Start(stageCtx); err != nil {
			stopClient()
			cleanupStaged()
			return fmt.Errorf("start %s: %w", registration.Name, err)
		}
		defs, err := client.ListTools(stageCtx)
		stopClient()
		if err != nil {
			cleanupStaged()
			return fmt.Errorf("list tools %s: %w", registration.Name, err)
		}
		item.tools = append([]toolDef(nil), defs...)
	}
	if err := stageCtx.Err(); err != nil {
		cleanupStaged()
		return err
	}

	// Revalidate after the lock-free startup. A concurrent owner change must
	// abort instead of letting this stale transaction remove a newer server.
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		cleanupStaged()
		return fmt.Errorf("manager is closed")
	}
	for id, expected := range expectedOld {
		if m.servers[id] != expected {
			m.mu.Unlock()
			cleanupStaged()
			return fmt.Errorf("server %q changed during replacement", id)
		}
	}
	if err := m.validateReplacementLocked(old, registrations); err != nil {
		m.mu.Unlock()
		cleanupStaged()
		return err
	}

	retired := make([]*server, 0, len(old))
	for id := range old {
		delete(m.authOverrides, id)
		if item, exists := m.servers[id]; exists {
			retired = append(retired, item)
			delete(m.servers, id)
			if m.owners[item.name] == id {
				delete(m.owners, item.name)
				delete(m.health, item.name)
			}
		}
	}
	for _, registration := range registrations {
		item := staged[registration.ID]
		m.servers[registration.ID] = item
		m.owners[registration.Name] = registration.ID
		m.health[registration.Name] = Health{Status: StatusReady, ToolCount: len(item.tools)}
		delete(m.authOverrides, registration.ID)
	}
	m.mu.Unlock()
	for _, item := range retired {
		if item.cancel != nil {
			item.cancel()
		}
		_ = item.client.Close()
	}
	return nil
}

func (m *Manager) validateReplacementLocked(old map[string]struct{}, registrations []Registration) error {
	seenIDs := make(map[string]struct{}, len(registrations))
	seenNames := make(map[string]struct{}, len(registrations))
	for _, registration := range registrations {
		if registration.ID == "" || registration.Name == "" {
			return fmt.Errorf("server registration requires id and name")
		}
		if _, exists := seenIDs[registration.ID]; exists {
			return fmt.Errorf("duplicate server id %q", registration.ID)
		}
		if _, exists := seenNames[registration.Name]; exists {
			return fmt.Errorf("duplicate server name %q", registration.Name)
		}
		seenIDs[registration.ID] = struct{}{}
		seenNames[registration.Name] = struct{}{}
		if _, exists := m.servers[registration.ID]; exists {
			if _, replacing := old[registration.ID]; !replacing {
				return fmt.Errorf("server id %q is already registered", registration.ID)
			}
		}
		if owner := m.owners[registration.Name]; owner != "" && owner != registration.ID {
			if _, replacing := old[owner]; !replacing {
				return fmt.Errorf("server name %q is already owned by %s", registration.Name, owner)
			}
		}
	}
	return nil
}

// AddBackground registers a server immediately and performs its process
// startup and tool discovery in the background. This keeps application startup
// independent from package runners and external MCP server latency.
func (m *Manager) AddBackground(id, name string, cfg ServerConfig) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return fmt.Errorf("manager is closed")
	}
	if owner := m.owners[name]; owner != "" && owner != id {
		m.mu.Unlock()
		return fmt.Errorf("server name %q is already owned by %s", name, owner)
	}

	var previous *server
	if current, ok := m.servers[id]; ok {
		previous = current
		delete(m.servers, id)
		if m.owners[current.name] == id {
			delete(m.owners, current.name)
			delete(m.health, current.name)
		}
	}

	ctx, cancel := context.WithCancel(m.ctx)
	merged, gen := m.applyAuthOverrideLocked(id, cfg)
	s := &server{name: name, client: newClient(name, merged), cancel: cancel, overrideGen: gen}
	m.servers[id] = s
	m.owners[name] = id
	m.health[name] = Health{Status: StatusStarting}
	m.wg.Add(1)
	go m.startBackground(ctx, id, s)
	if previous != nil {
		if previous.cancel != nil {
			previous.cancel()
		}
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			_ = previous.client.Close()
		}()
	}
	m.mu.Unlock()
	return nil
}

func (m *Manager) startBackground(ctx context.Context, id string, s *server) {
	defer m.wg.Done()

	err := s.client.Start(ctx)
	var defs []toolDef
	if err == nil {
		defs, err = s.client.ListTools(ctx)
		if err != nil {
			_ = s.client.Close()
		}
	}

	m.mu.Lock()
	if m.servers[id] != s {
		m.mu.Unlock()
		_ = s.client.Close()
		return
	}
	// The interactive flag only governs the authorize pass inside Start;
	// retire exactly the generation this start consumed so later rebuilds
	// stay non-interactive and a newer login override survives.
	m.consumeAuthOverrideLocked(id, s.overrideGen)
	if err != nil {
		m.health[s.name] = Health{Status: StatusError, Error: err.Error()}
		m.mu.Unlock()
		return
	}
	s.tools = append([]toolDef(nil), defs...)
	m.health[s.name] = Health{Status: StatusReady, ToolCount: len(defs)}
	m.mu.Unlock()
}

// Remove stops the server owned by id. Its tools disappear from the
// capability proxy's routing (the provider-visible schema is unaffected).
func (m *Manager) Remove(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removeLocked(id)
}

// Close stops every managed server.
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	m.cancel()
	servers := make([]*server, 0, len(m.servers))
	for _, s := range m.servers {
		servers = append(servers, s)
		if s.cancel != nil {
			s.cancel()
		}
	}
	clear(m.servers)
	clear(m.owners)
	clear(m.health)
	m.mu.Unlock()

	for _, s := range servers {
		_ = s.client.Close()
	}
	m.wg.Wait()
}

// Health returns a snapshot of per-server health keyed by server name.
func (m *Manager) Health() map[string]Health {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.healthLocked()
}

func (m *Manager) healthLocked() map[string]Health {
	health := maps.Clone(m.health)
	for name, state := range health {
		state.Owner = m.owners[name]
		if s := m.servers[state.Owner]; s != nil && s.client.remote != nil {
			if status, uri := s.client.remote.auth.state(); status != "" {
				state.Status, state.AuthURL = status, uri
			}
		}
		health[name] = state
	}
	return health
}

// AuthorizationAction starts a fresh connection without blocking the caller
// on browser interaction. Logout cancels any pending flow before erasing tokens.
func (m *Manager) AuthorizationAction(name, action string) error {
	m.mu.Lock()
	id := m.owners[name]
	s := m.servers[id]
	if s == nil || s.client.remote == nil {
		m.mu.Unlock()
		return fmt.Errorf("remote MCP server %q not found", name)
	}
	// Snapshot everything needed while the lock is held: the client may be
	// closed or replaced by the time the flow restarts below.
	cfg := s.client.config
	var scopes []string
	switch action {
	case "login":
		scopes = s.client.remote.auth.authorizationScopes()
	case "logout", "cancel":
	default:
		m.mu.Unlock()
		return fmt.Errorf("unknown MCP authorization action %q", action)
	}
	// Record the login state under the owner id so config reloads and error
	// rebuilds re-register the server with interactive (and the accumulated
	// scopes) instead of silently dropping a pending browser flow. The
	// override is merged by applyAuthOverrideLocked at registration time.
	if action == "login" {
		m.authorizing[name] = ""
		cfg.authorizationScopes = scopes
		m.overrideSeq++
		m.authOverrides[id] = authOverride{interactive: true, scopes: append([]string(nil), scopes...), gen: m.overrideSeq}
	} else {
		delete(m.authorizing, name)
		delete(m.authOverrides, id)
	}
	m.mu.Unlock()

	// Drain the old authorization attempt before removing its credentials;
	// its final token callback must not recreate a login after logout.
	_ = s.client.Close()
	if action == "logout" {
		// Local logout must succeed even if the remote DELETE fails.
		if err := forgetCredential(name, cfg); err != nil {
			return err
		}
	}
	return m.AddBackground(id, name, cfg)
}

// HasStoredCredential reports whether name is a remote server with a locally
// saved OAuth credential — i.e. whether logout has anything to remove. stdio
// servers and never-authorized remotes have none, so UIs can omit logout.
func (m *Manager) HasStoredCredential(name string) bool {
	m.mu.Lock()
	s := m.servers[m.owners[name]]
	if s == nil {
		m.mu.Unlock()
		return false
	}
	cfg := s.client.config
	m.mu.Unlock()
	if cfg.URL == "" {
		// stdio servers never hold OAuth credentials.
		return false
	}
	path, err := credentialPath(name, cfg)
	if err != nil {
		return false
	}
	if _, err := os.Stat(path); err != nil {
		return false
	}
	return true
}

// Owner reports the owner id that registered the server with this name, or
// an empty string when the name is unused.
func (m *Manager) Owner(name string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.owners[name]
}

func (m *Manager) removeLocked(id string) {
	s, ok := m.servers[id]
	if !ok {
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	_ = s.client.Close()
	delete(m.servers, id)
	if m.owners[s.name] == id {
		delete(m.owners, s.name)
		delete(m.health, s.name)
	}
}

// ListCapabilities renders the available servers and their tools in a
// compact, stable form for the model.
func (m *Manager) ListCapabilities() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.health) == 0 {
		return "No MCP servers configured."
	}
	names := make([]string, 0, len(m.health))
	for name := range m.health {
		names = append(names, name)
	}
	sort.Strings(names)
	byName := make(map[string]*server, len(m.servers))
	for _, s := range m.servers {
		byName[s.name] = s
	}

	health := m.healthLocked()
	var b strings.Builder
	for _, name := range names {
		h := health[name]
		if h.Status != StatusReady {
			fmt.Fprintf(&b, "%s (%s%s)\n", name, h.Status, healthErrorSuffix(h))
			continue
		}
		fmt.Fprintf(&b, "%s:\n", name)
		tools := make([]string, 0, len(byName[name].tools))
		for _, tool := range byName[name].tools {
			tools = append(tools, toolLine(tool))
		}
		sort.Strings(tools)
		for _, line := range tools {
			b.WriteString("  ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func healthErrorSuffix(h Health) string {
	if h.Error == "" {
		return ""
	}
	return ": " + h.Error
}

func toolLine(def toolDef) string {
	desc := strings.Join(strings.Fields(def.Description), " ")
	const maxDesc = 80
	if len([]rune(desc)) > maxDesc {
		desc = string([]rune(desc)[:maxDesc]) + "…"
	}
	if desc == "" {
		return "- " + def.Name
	}
	return fmt.Sprintf("- %s — %s", def.Name, desc)
}

// InspectTool returns one tool's full description and input schema.
func (m *Manager) InspectTool(serverName, toolName string) (string, error) {
	_, def, err := m.findTool(serverName, toolName)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "mcp__%s__%s\n", serverName, def.Name)
	if def.Description != "" {
		b.WriteString(def.Description)
		b.WriteString("\n")
	}
	schema, err := json.MarshalIndent(def.InputSchema, "", "  ")
	if err != nil {
		return "", err
	}
	b.Write(schema)
	return b.String(), nil
}

// CallServerTool invokes a tool on a ready server.
func (m *Manager) CallServerTool(ctx context.Context, serverName, toolName string, args map[string]any) (string, error) {
	client, def, err := m.findTool(serverName, toolName)
	if err != nil {
		return "", err
	}
	return client.CallTool(ctx, def.Name, args)
}

// findTool resolves a server.tool pair to its client and definition.
func (m *Manager) findTool(serverName, toolName string) (*client, toolDef, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.servers {
		if s.name != serverName {
			continue
		}
		for _, tool := range s.tools {
			if tool.Name == toolName {
				return s.client, tool, nil
			}
		}
		if m.health[serverName].Status == StatusStarting {
			return nil, toolDef{}, fmt.Errorf("server %q is still starting", serverName)
		}
		return nil, toolDef{}, fmt.Errorf("server %q has no tool %q", serverName, toolName)
	}
	if m.health[serverName].Status == StatusError {
		return nil, toolDef{}, fmt.Errorf("server %q is in error state: %s", serverName, m.health[serverName].Error)
	}
	return nil, toolDef{}, fmt.Errorf("unknown MCP server %q", serverName)
}
