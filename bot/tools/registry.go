package tools

import (
	"nekocode/common"
	"nekocode/bot/tools/core"
)

// Registry is a thread-safe tool registry backed by a generic registry.
type Registry struct {
	*common.Registry[core.Tool]
}

// NewRegistry creates a new tool registry.
func NewRegistry() *Registry {
	return &Registry{
		Registry: common.NewRegistry[core.Tool](func(t core.Tool) string { return t.Name() }),
	}
}

// Get returns a tool by name, or an error if not found.
func (r *Registry) Get(name string) (core.Tool, error) {
	return r.Registry.GetOrError(name, "tool not found: %s")
}

// Descriptors returns tool descriptors for all registered tools, sorted by name.
func (r *Registry) Descriptors() []core.Descriptor {
	list := r.Registry.List()
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
