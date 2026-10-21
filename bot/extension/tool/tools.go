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
	items           *registry.Registry[core.Tool]
	mu              sync.RWMutex
	workspace       *workspace.Manager
	planTools       map[string]struct{}
	targetResolvers map[string]TargetResolver
	previewers      map[string]PreviewFunc
	privileged      map[string]PrivilegedFunc
	permissionPlans map[string]PermissionPlanFunc
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
	ResolveTarget  TargetResolver
	Preview        PreviewFunc
	Privileged     PrivilegedFunc
	PermissionPlan PermissionPlanFunc
	PlanAllowed    bool
}

// Entry is one atomic registry snapshot used by the executor.
type Entry struct {
	Tool           core.Tool
	ResolveTarget  TargetResolver
	Preview        PreviewFunc
	Privileged     PrivilegedFunc
	PermissionPlan PermissionPlanFunc
	PlanAllowed    bool
}

// New creates a registry containing the supplied tools.
func New(items ...core.Tool) *Registry {
	r := &Registry{
		items:           registry.New[core.Tool](func(t core.Tool) string { return t.Name() }),
		workspace:       workspace.New("", nil),
		planTools:       make(map[string]struct{}),
		targetResolvers: make(map[string]TargetResolver),
		previewers:      make(map[string]PreviewFunc),
		privileged:      make(map[string]PrivilegedFunc),
		permissionPlans: make(map[string]PermissionPlanFunc),
	}
	r.RegisterAll(items)
	return r
}

// Register adds or replaces a tool and clears metadata left by an older
// registration with the same name.
func (r *Registry) Register(item core.Tool) {
	r.RegisterWithOptions(item, RegistrationOptions{})
}

// RegisterWithOptions adds or replaces a tool together with its executor
// metadata.
func (r *Registry) RegisterWithOptions(item core.Tool, options RegistrationOptions) {
	if item == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items.Register(item)
	if options.ResolveTarget == nil {
		delete(r.targetResolvers, item.Name())
	} else {
		r.targetResolvers[item.Name()] = options.ResolveTarget
	}
	if options.Preview == nil {
		delete(r.previewers, item.Name())
	} else {
		r.previewers[item.Name()] = options.Preview
	}
	if options.Privileged == nil {
		delete(r.privileged, item.Name())
	} else {
		r.privileged[item.Name()] = options.Privileged
	}
	if options.PermissionPlan == nil {
		delete(r.permissionPlans, item.Name())
	} else {
		r.permissionPlans[item.Name()] = options.PermissionPlan
	}
	if options.PlanAllowed {
		r.planTools[item.Name()] = struct{}{}
	} else {
		delete(r.planTools, item.Name())
	}
}

// RegisterAll adds tools without additional executor metadata.
func (r *Registry) RegisterAll(items []core.Tool) {
	for _, item := range items {
		r.Register(item)
	}
}

// Unregister removes a tool and all metadata attached to it.
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	r.items.Unregister(name)
	delete(r.targetResolvers, name)
	delete(r.previewers, name)
	delete(r.privileged, name)
	delete(r.permissionPlans, name)
	delete(r.planTools, name)
	r.mu.Unlock()
}

// Lookup returns the tool and all executor metadata from one registry
// snapshot, preventing a replacement from mixing old metadata with a new tool.
func (r *Registry) Lookup(name string) (Entry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, err := r.items.GetOrError(name, "tool not found: %s")
	if err != nil {
		return Entry{}, err
	}
	_, planAllowed := r.planTools[name]
	return Entry{
		Tool: t, ResolveTarget: r.targetResolvers[name], Preview: r.previewers[name],
		Privileged: r.privileged[name], PermissionPlan: r.permissionPlans[name], PlanAllowed: planAllowed,
	}, nil
}

// Preview renders the registered request-scoped preview for a tool.
func (r *Registry) Preview(ctx context.Context, name string, args map[string]any) (string, bool) {
	r.mu.RLock()
	preview := r.previewers[name]
	r.mu.RUnlock()
	if preview == nil {
		return "", false
	}
	return preview(ctx, args), true
}

// ResolveTarget returns the effective identity of a delegated call.
func (r *Registry) ResolveTarget(name string, args map[string]any) (CallTarget, bool) {
	r.mu.RLock()
	resolver := r.targetResolvers[name]
	r.mu.RUnlock()
	if resolver == nil {
		return CallTarget{}, false
	}
	return resolver(args)
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
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.items.Has(name)
}

// Names returns registered tool names in stable order.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.items.Names()
}

// List returns a stable tool snapshot sorted by name.
func (r *Registry) List() []core.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.items.List()
}

// Descriptors returns tool descriptors for all registered tools, sorted by name.
func (r *Registry) Descriptors() []core.Descriptor {
	list := r.List()
	descs := make([]core.Descriptor, len(list))
	for i, t := range list {
		descs[i] = core.Descriptor{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
		}
	}
	return descs
}
