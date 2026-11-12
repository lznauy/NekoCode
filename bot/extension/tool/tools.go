package tools

import (
	"context"
	"sync"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/workspace"
	"nekocode/util/registry"
)

// Registry is a thread-safe tool registry backed by a generic registry.
type Registry struct {
	items     *registry.Registry[Entry]
	mu        sync.RWMutex
	workspace *workspace.Manager
}

// CallTarget is the effective identity used by permission, audit, and UI
// layers when a model-facing tool delegates to another capability.
type CallTarget struct {
	Name string
	Args map[string]any
}

// TargetResolver translates model-facing arguments to an effective call
// identity. It is registration metadata rather than an optional tool
// interface, so the executor does not need to know concrete proxy protocols.
type TargetResolver func(args map[string]any) (CallTarget, bool)

// PreviewFunc renders a request-scoped preview before execution.
type PreviewFunc func(context.Context, map[string]any) string

// PrivilegedFunc retries a tool call with an explicitly approved capability
// grant. It is registration metadata so the executor never needs to inspect a
// concrete tool implementation.
type PrivilegedFunc func(context.Context, map[string]any, core.PermissionRequest) (core.ToolOutput, error)

// PermissionPlanFunc predicts the capability request implied by explicit tool
// arguments. It lives beside PrivilegedFunc so only the tool that owns an
// argument schema interprets it; the generic runner never guesses by key name.
type PermissionPlanFunc func(args map[string]any, workspace string) *core.PermissionRequest

// RegistrationOptions describes executor metadata attached to one tool.
type RegistrationOptions struct {
	ResolveTarget      TargetResolver
	Preview            PreviewFunc
	Privileged         PrivilegedFunc
	PermissionPlan     PermissionPlanFunc
	PlanAllowed        bool
	CoordinatorAllowed bool
	CoordinatorOnly    bool // Hide and reject outside the coordination tool policy.
	DelegationAllowed  bool
}

// Entry is one atomic registry snapshot used by the executor.
type Entry struct {
	Tool               core.Tool
	ResolveTarget      TargetResolver
	Preview            PreviewFunc
	Privileged         PrivilegedFunc
	PermissionPlan     PermissionPlanFunc
	PlanAllowed        bool
	CoordinatorAllowed bool
	CoordinatorOnly    bool
	DelegationAllowed  bool
}

// New creates a registry containing the supplied tools.
func New(items ...core.Tool) *Registry {
	r := &Registry{
		items:     registry.New[Entry](func(entry Entry) string { return entry.Tool.Name() }),
		workspace: workspace.New("", nil),
	}
	r.RegisterAll(items)
	return r
}

// Register replaces the tool and clears all previous executor metadata.
func (r *Registry) Register(item core.Tool) {
	r.RegisterWithOptions(item, RegistrationOptions{})
}

// RegisterWithOptions stores the tool and its metadata as one atomic entry.
func (r *Registry) RegisterWithOptions(item core.Tool, options RegistrationOptions) {
	if item == nil {
		return
	}
	r.items.Register(Entry{
		Tool: item, ResolveTarget: options.ResolveTarget, Preview: options.Preview,
		Privileged: options.Privileged, PermissionPlan: options.PermissionPlan,
		PlanAllowed: options.PlanAllowed, CoordinatorAllowed: options.CoordinatorAllowed, CoordinatorOnly: options.CoordinatorOnly, DelegationAllowed: options.DelegationAllowed,
	})
}

// RegisterAll adds tools without additional executor metadata.
func (r *Registry) RegisterAll(items []core.Tool) {
	for _, item := range items {
		r.Register(item)
	}
}

// Unregister removes a tool and all metadata attached to it.
func (r *Registry) Unregister(name string) { r.items.Unregister(name) }

// Lookup returns one atomic tool and metadata snapshot.
func (r *Registry) Lookup(name string) (Entry, error) {
	return r.items.GetOrError(name, "tool not found: %s")
}

// Preview renders the registered request-scoped preview for a tool.
func (r *Registry) Preview(ctx context.Context, name string, args map[string]any) (string, bool) {
	entry, err := r.Lookup(name)
	if err != nil || entry.Preview == nil {
		return "", false
	}
	return entry.Preview(ctx, args), true
}

// ResolveTarget returns the effective identity of a delegated call.
func (r *Registry) ResolveTarget(name string, args map[string]any) (CallTarget, bool) {
	entry, err := r.Lookup(name)
	if err != nil || entry.ResolveTarget == nil {
		return CallTarget{}, false
	}
	return entry.ResolveTarget(args)
}

// EnrichCall attaches the delegated governance identity while preserving the
// model-facing tool invocation used for execution and provider correlation.
func (r *Registry) EnrichCall(call core.ToolCallItem) core.ToolCallItem {
	if call.EffectiveName != "" {
		return call
	}
	target, ok := r.ResolveTarget(call.Name, call.Args)
	if !ok {
		return call
	}
	call.EffectiveName = target.Name
	call.EffectiveArgs = target.Args
	return call
}

// Workspace returns the authority shared by this registry's executor and
// tools. Each registry owns an independent instance.
func (r *Registry) Workspace() *workspace.Manager {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.workspace
}

func (r *Registry) SetWorkspace(manager *workspace.Manager) {
	if manager != nil {
		r.mu.Lock()
		r.workspace = manager
		r.mu.Unlock()
	}
}

// Get returns a tool by name, or an error if not found.
func (r *Registry) Get(name string) (core.Tool, error) {
	entry, err := r.Lookup(name)
	return entry.Tool, err
}

// Has reports whether a tool is registered.
func (r *Registry) Has(name string) bool { return r.items.Has(name) }

// Names returns registered tool names in stable order.
func (r *Registry) Names() []string { return r.items.Names() }

// List returns a stable tool snapshot sorted by name.
func (r *Registry) List() []core.Tool {
	entries := r.items.List()
	list := make([]core.Tool, len(entries))
	for i, entry := range entries {
		list[i] = entry.Tool
	}
	return list
}

// Descriptors returns tool descriptors for all registered tools, sorted by name.
func (r *Registry) Descriptors() []core.Descriptor {
	return r.DescriptorsMatching(nil)
}

// DescriptorsMatching filters each atomic entry before evaluating its schema.
// Tool implementations are called outside registry locks.
func (r *Registry) DescriptorsMatching(allow func(Entry) bool) []core.Descriptor {
	entries := r.items.List()
	descs := make([]core.Descriptor, 0, len(entries))
	for _, entry := range entries {
		if allow != nil && !allow(entry) {
			continue
		}
		t := entry.Tool
		descs = append(descs, core.Descriptor{Name: t.Name(), Description: t.Description(), Parameters: t.Parameters()})
	}
	return descs
}

// DelegatableNames returns only explicitly trusted worker capabilities.
func (r *Registry) DelegatableNames() []string {
	names := []string{}
	for _, entry := range r.items.List() {
		if entry.DelegationAllowed {
			names = append(names, entry.Tool.Name())
		}
	}
	return names
}
