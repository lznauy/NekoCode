package tools

import (
	"nekocode/common"
)

// Registry is a thread-safe tool registry backed by a generic registry.
type Registry struct {
	*common.Registry[Tool]
}

// NewRegistry creates a new tool registry.
func NewRegistry() *Registry {
	return &Registry{
		Registry: common.NewRegistry[Tool](func(t Tool) string { return t.Name() }),
	}
}

// Get returns a tool by name, or an error if not found.
func (r *Registry) Get(name string) (Tool, error) {
	return r.Registry.GetOrError(name, "tool not found: %s")
}

// Descriptors returns tool descriptors for all registered tools, sorted by name.
func (r *Registry) Descriptors() []Descriptor {
	list := r.Registry.List()
	descs := make([]Descriptor, len(list))
	for i, t := range list {
		descs[i] = Descriptor{t.Name(), t.Description(), t.Parameters()}
	}
	return descs
}
