package skill

import (
	"strings"
	"sync"

	"nekocode/common"
)

// Registry manages loaded skills, thread-safe.
type Registry struct {
	*common.Registry[*Skill]
	loaded sync.Map
}

func NewRegistry() *Registry {
	return &Registry{
		Registry: common.NewRegistry[*Skill](func(s *Skill) string { return s.Name }),
	}
}

func (r *Registry) RegisterBundled(skills []*Skill) {
	r.Registry.RegisterAll(skills)
}

func (r *Registry) Load(dirs []string) error {
	paths := discoverSkills(dirs)
	for _, p := range paths {
		sk, err := loadSkill(p)
		if err != nil {
			continue
		}
		if !r.Registry.Has(sk.Name) {
			r.Registry.Register(sk)
		}
	}
	return nil
}

func (r *Registry) MarkLoaded(name string) {
	r.loaded.Store(name, true)
}

func (r *Registry) ClearLoaded() {
	r.loaded.Clear()
}

func (r *Registry) IsLoaded(name string) bool {
	_, ok := r.loaded.Load(name)
	return ok
}

func (r *Registry) LoadedSet() map[string]bool {
	out := make(map[string]bool)
	r.loaded.Range(func(key, value any) bool {
		out[key.(string)] = true
		return true
	})
	return out
}

func (r *Registry) names() []string {
	return r.Registry.Names()
}

func (r *Registry) namesString() string {
	ns := r.names()
	if len(ns) == 0 {
		return "none"
	}
	return strings.Join(ns, ", ")
}
