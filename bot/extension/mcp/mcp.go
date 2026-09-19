// Package mcp implements an MCP (Model Context Protocol) client over stdio.
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
	"sort"
	"strings"
	"sync"
)

// Server health statuses reported by Manager.Health.
const (
	StatusStarting = "starting"
	StatusReady    = "ready"
	StatusError    = "error"
)

// Health reports the runtime state of one managed server.
type Health struct {
	Owner     string
	Status    string
	Error     string
	ToolCount int
}

// server is a managed connection and the tools discovered on it.
type server struct {
	name   string
	client *client
	tools  []toolDef
	cancel context.CancelFunc
}

// Manager owns the lifecycle of every MCP server connection: spawning
// processes, performing the initialize handshake, and holding the tools for
// the capability proxy to route to. Servers have a stable owner ID separate
// from their user-facing name.
type Manager struct {
	mu      sync.Mutex
	servers map[string]*server
	owners  map[string]string
	health  map[string]Health
	ctx     context.Context
	cancel  context.CancelFunc
	closed  bool
	wg      sync.WaitGroup
}

// New creates an empty Manager that owns MCP server lifecycles.
func New() *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		servers: make(map[string]*server),
		owners:  make(map[string]string),
		health:  make(map[string]Health),
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Add starts or replaces a server owned by id. Name remains the MCP tool
// prefix visible to the model.
func (m *Manager) Add(ctx context.Context, id, name string, cfg ServerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return fmt.Errorf("manager is closed")
	}

	if owner := m.owners[name]; owner != "" && owner != id {
		return fmt.Errorf("server name %q is already owned by %s", name, owner)
	}
	m.removeLocked(id)

	client := newClient(name, cfg)
	m.servers[id] = &server{name: name, client: client}
	m.owners[name] = id
	m.health[name] = Health{Status: StatusStarting}

	if err := client.Start(ctx); err != nil {
		m.removeLocked(id)
		return fmt.Errorf("start: %w", err)
	}

	defs, err := client.ListTools(ctx)
	if err != nil {
		_ = client.Close()
		m.removeLocked(id)
		return fmt.Errorf("list tools: %w", err)
	}

	serverTools := append([]toolDef(nil), defs...)
	m.servers[id].tools = serverTools
	m.health[name] = Health{Status: StatusReady, ToolCount: len(serverTools)}
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
	for _, id := range oldIDs {
		old[id] = struct{}{}
	}
	seenIDs := make(map[string]struct{}, len(registrations))
	seenNames := make(map[string]struct{}, len(registrations))
	for _, registration := range registrations {
		if registration.ID == "" || registration.Name == "" {
			m.mu.Unlock()
			return fmt.Errorf("server registration requires id and name")
		}
		if _, exists := seenIDs[registration.ID]; exists {
			m.mu.Unlock()
			return fmt.Errorf("duplicate server id %q", registration.ID)
		}
		if _, exists := seenNames[registration.Name]; exists {
			m.mu.Unlock()
			return fmt.Errorf("duplicate server name %q", registration.Name)
		}
		seenIDs[registration.ID] = struct{}{}
		seenNames[registration.Name] = struct{}{}
		if _, exists := m.servers[registration.ID]; exists {
			if _, replacing := old[registration.ID]; !replacing {
				m.mu.Unlock()
				return fmt.Errorf("server id %q is already registered", registration.ID)
			}
		}
		if owner := m.owners[registration.Name]; owner != "" && owner != registration.ID {
			if _, replacing := old[owner]; !replacing {
				m.mu.Unlock()
				return fmt.Errorf("server name %q is already owned by %s", registration.Name, owner)
			}
		}
	}

	staged := make(map[string]*server, len(registrations))
	cleanupStaged := func() {
		for _, item := range staged {
			_ = item.client.Close()
		}
	}
	for _, registration := range registrations {
		if err := ctx.Err(); err != nil {
			cleanupStaged()
			m.mu.Unlock()
			return err
		}
		client := newClient(registration.Name, registration.Config)
		item := &server{name: registration.Name, client: client}
		staged[registration.ID] = item
		if err := client.Start(ctx); err != nil {
			cleanupStaged()
			m.mu.Unlock()
			return fmt.Errorf("start %s: %w", registration.Name, err)
		}
		defs, err := client.ListTools(ctx)
		if err != nil {
			cleanupStaged()
			m.mu.Unlock()
			return fmt.Errorf("list tools %s: %w", registration.Name, err)
		}
		item.tools = append([]toolDef(nil), defs...)
	}

	retired := make([]*server, 0, len(old))
	for id := range old {
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
	s := &server{name: name, client: newClient(name, cfg), cancel: cancel}
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
	health := maps.Clone(m.health)
	for name, state := range health {
		state.Owner = m.owners[name]
		health[name] = state
	}
	return health
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

	var b strings.Builder
	for _, name := range names {
		h := m.health[name]
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
